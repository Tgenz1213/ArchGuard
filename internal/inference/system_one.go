package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	systemOneMaxRetries        = 3
	systemOneRetryWait         = time.Second
	maxSystemOneErrorBodyBytes = 4096
	statusOverloaded           = 529
	kevTruncateHint            = "KEV_TRUNCATE_STATES"
)

var errTruncated = fmt.Errorf("the server answered on only the start of the state: %w", ErrInputTooLarge)

// SystemOneError is a non-2xx response from a System One server; ErrorType is the body's detail.error_type when it has one.
type SystemOneError struct {
	Status     int
	ErrorType  string
	Body       string
	retryAfter time.Duration
	stateLimit bool
}

func (e *SystemOneError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("system one api error: %d %s", e.Status, http.StatusText(e.Status))
	}

	return fmt.Sprintf("system one api error: %d %s: %s", e.Status, http.StatusText(e.Status), e.Body)
}

func (e *SystemOneError) Is(target error) bool {
	switch target {
	case ErrKeyRejected:
		return e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
	case ErrInputTooLarge:
		return e.ErrorType == "max_tokens_exceeded" || e.stateLimit
	default:
		return false
	}
}

func (e *SystemOneError) retryable() bool {
	return e.Status == http.StatusTooManyRequests || e.Status == statusOverloaded
}

type SystemOneProvider struct {
	apiKey    string
	model     string
	endpoint  string
	client    *http.Client
	retryWait time.Duration
}

func NewSystemOneProvider(baseURL, apiKey, model string, timeout time.Duration) (*SystemOneProvider, error) {
	return newSystemOneProvider(baseURL, apiKey, model, &http.Client{Timeout: timeout})
}

func newSystemOneProvider(baseURL, apiKey, model string, httpClient *http.Client) (*SystemOneProvider, error) {
	endpoint, err := url.JoinPath(baseURL, "v1", "systemone")
	if err != nil {
		return nil, fmt.Errorf("system one base url %q: %w", baseURL, err)
	}

	return &SystemOneProvider{apiKey: apiKey, model: model, endpoint: endpoint, client: httpClient, retryWait: systemOneRetryWait}, nil
}

// Ask asks every yes/no question about state in one call and returns each answer's probability of yes, by question name.
func (p *SystemOneProvider) Ask(ctx context.Context, state string, questions map[string]string) (AskReply, error) {
	payload, err := p.requestBody(state, questions)
	if err != nil {
		return AskReply{}, err
	}

	for attempt := 0; ; attempt++ {
		reply, err := p.post(ctx, payload)

		var apiErr *SystemOneError
		if err == nil || !errors.As(err, &apiErr) || !apiErr.retryable() || attempt == systemOneMaxRetries {
			return reply, err
		}

		wait := apiErr.retryAfter
		if wait == 0 {
			wait = p.retryWait << attempt
		}

		select {
		case <-ctx.Done():
			return AskReply{}, ctx.Err()
		case <-time.After(wait):
		}
	}
}

func (p *SystemOneProvider) requestBody(state string, questions map[string]string) ([]byte, error) {
	type question struct {
		Type         string `json:"type"`
		Instructions string `json:"instructions"`
	}

	asked := make(map[string]question, len(questions))
	for name, instructions := range questions {
		asked[name] = question{Type: "noul", Instructions: instructions}
	}

	payload, err := json.Marshal(map[string]any{"model": p.model, "state": state, "questions": asked})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal system one request: %w", err)
	}

	return payload, nil
}

func (p *SystemOneProvider) post(ctx context.Context, payload []byte) (AskReply, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(payload))
	if err != nil {
		return AskReply{}, fmt.Errorf("failed to build system one request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return AskReply{}, fmt.Errorf("system one request failed: %w", err)
	}

	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // closing a read response body can't change the result

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return AskReply{}, systemOneResponseError(resp)
	}

	return decodeAnswers(resp.Body)
}

func decodeAnswers(r io.Reader) (AskReply, error) {
	var body struct {
		Answers map[string]struct {
			Noul *float64 `json:"noul"`
		} `json:"answers"`
		Truncated bool `json:"truncated"`
		Usage     *struct {
			InputTokens  *int `json:"input_tokens"`
			OutputTokens *int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(r).Decode(&body); err != nil {
		return AskReply{}, fmt.Errorf("failed to decode system one response: %w", err)
	}

	if body.Truncated {
		return AskReply{}, errTruncated
	}

	reply := AskReply{Answers: make(map[string]float64, len(body.Answers))}
	for name, answer := range body.Answers {
		if answer.Noul == nil {
			return AskReply{}, fmt.Errorf("system one answer %q has no noul probability", name)
		}

		reply.Answers[name] = *answer.Noul
	}

	if body.Usage != nil && body.Usage.InputTokens != nil && body.Usage.OutputTokens != nil {
		reply.Usage = Usage{InputTokens: *body.Usage.InputTokens, OutputTokens: *body.Usage.OutputTokens, Reported: true}
	}

	return reply, nil
}

func systemOneResponseError(resp *http.Response) *SystemOneError {
	apiErr := &SystemOneError{Status: resp.StatusCode}
	if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && seconds > 0 {
		apiErr.retryAfter = time.Duration(seconds) * time.Second
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxSystemOneErrorBodyBytes))
	if err != nil {
		return apiErr
	}

	apiErr.Body = strings.TrimSpace(string(raw))

	// Jev's detail is an object, a list or a string depending on the error; only the object carries error_type.
	var object struct {
		Detail struct {
			ErrorType string `json:"error_type"`
		} `json:"detail"`
	}
	if json.Unmarshal(raw, &object) == nil {
		apiErr.ErrorType = object.Detail.ErrorType
	}

	// Kev refuses an over-long state with a 422 whose string detail alone names KEV_TRUNCATE_STATES.
	var text struct {
		Detail string `json:"detail"`
	}
	if resp.StatusCode == http.StatusUnprocessableEntity && json.Unmarshal(raw, &text) == nil {
		apiErr.stateLimit = strings.Contains(text.Detail, kevTruncateHint)
	}

	return apiErr
}
