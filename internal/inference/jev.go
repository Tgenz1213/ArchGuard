package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultJevModel      = "jev-latest"
	jevBaseURL           = "https://api.typesafe.ai"
	jevMaxRetries        = 3
	jevRetryWait         = time.Second
	jevRequestTimeout    = 60 * time.Second
	maxJevErrorBodyBytes = 4096
	statusOverloaded     = 529
)

// JevError is a non-2xx response from Jev; ErrorType is the body's detail.error_type when it has one.
type JevError struct {
	Status     int
	ErrorType  string
	Body       string
	retryAfter time.Duration
}

func (e *JevError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("jev api error: %d %s", e.Status, http.StatusText(e.Status))
	}

	return fmt.Sprintf("jev api error: %d %s: %s", e.Status, http.StatusText(e.Status), e.Body)
}

func (e *JevError) TooLarge() bool { return e.ErrorType == "max_tokens_exceeded" }

// Rejected reports that Jev refused the API key, which no retry or pass-through fixes.
func (e *JevError) Rejected() bool {
	return e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
}

func (e *JevError) retryable() bool {
	return e.Status == http.StatusTooManyRequests || e.Status == statusOverloaded
}

type JevProvider struct {
	apiKey    string
	model     string
	baseURL   string
	client    *http.Client
	retryWait time.Duration
}

func NewJevProvider(apiKey, model string) *JevProvider {
	return NewJevProviderWithBaseURL(apiKey, model, jevBaseURL, &http.Client{Timeout: jevRequestTimeout})
}

func NewJevProviderWithBaseURL(apiKey, model, baseURL string, httpClient *http.Client) *JevProvider {
	if model == "" {
		model = DefaultJevModel
	}

	return &JevProvider{apiKey: apiKey, model: model, baseURL: baseURL, client: httpClient, retryWait: jevRetryWait}
}

// Ask asks every yes/no question about state in one call and returns each answer's probability of yes, by question name.
func (p *JevProvider) Ask(ctx context.Context, state string, questions map[string]string) (map[string]float64, error) {
	payload, err := p.requestBody(state, questions)
	if err != nil {
		return nil, err
	}

	for attempt := 0; ; attempt++ {
		answers, err := p.post(ctx, payload)

		var jevErr *JevError
		if err == nil || !errors.As(err, &jevErr) || !jevErr.retryable() || attempt == jevMaxRetries {
			return answers, err
		}

		wait := jevErr.retryAfter
		if wait == 0 {
			wait = p.retryWait << attempt
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
}

func (p *JevProvider) requestBody(state string, questions map[string]string) ([]byte, error) {
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
		return nil, fmt.Errorf("failed to marshal jev request: %w", err)
	}

	return payload, nil
}

func (p *JevProvider) post(ctx context.Context, payload []byte) (map[string]float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/systemone", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to build jev request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jev request failed: %w", err)
	}

	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // closing a read response body can't change the result

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, jevResponseError(resp)
	}

	var body struct {
		Answers map[string]struct {
			Noul *float64 `json:"noul"`
		} `json:"answers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("failed to decode jev response: %w", err)
	}

	answers := make(map[string]float64, len(body.Answers))
	for name, answer := range body.Answers {
		if answer.Noul == nil {
			return nil, fmt.Errorf("jev answer %q has no noul probability", name)
		}

		answers[name] = *answer.Noul
	}

	return answers, nil
}

func jevResponseError(resp *http.Response) *JevError {
	jevErr := &JevError{Status: resp.StatusCode}
	if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && seconds > 0 {
		jevErr.retryAfter = time.Duration(seconds) * time.Second
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxJevErrorBodyBytes))
	if err != nil {
		return jevErr
	}

	jevErr.Body = strings.TrimSpace(string(raw))

	// detail is an object, a list or a string depending on the error; only the object carries error_type.
	var body struct {
		Detail struct {
			ErrorType string `json:"error_type"`
		} `json:"detail"`
	}
	if json.Unmarshal(raw, &body) == nil {
		jevErr.ErrorType = body.Detail.ErrorType
	}

	return jevErr
}
