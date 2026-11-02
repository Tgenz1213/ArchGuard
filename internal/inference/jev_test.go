package inference

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func jevServer(t *testing.T, handler http.HandlerFunc) *JevProvider {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	provider := NewJevProviderWithBaseURL("test-key", "", server.URL, server.Client())
	provider.retryWait = 0

	return provider
}

func TestJevProvider_AskSendsOneNoulQuestionPerRule(t *testing.T) {
	var got struct {
		Model     string `json:"model"`
		State     string `json:"state"`
		Questions map[string]struct {
			Type         string `json:"type"`
			Instructions string `json:"instructions"`
		} `json:"questions"`
	}
	var path, auth string

	provider := jevServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decoding request: %v", err)
		}

		if _, err := w.Write([]byte(`{"model":"jev-1.13.0","answers":{"a":{"type":"noul","noul":0.91},"b":{"type":"noul","noul":0.04}},"usage":{"input_tokens":10,"output_tokens":2}}`)); err != nil {
			t.Errorf("writing response: %v", err)
		}
	})

	answers, err := provider.Ask(t.Context(), "+fmt.Println(x)", map[string]string{"a": "Does it print?", "b": "Does it panic?"})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	if answers["a"] != 0.91 || answers["b"] != 0.04 || len(answers) != 2 {
		t.Errorf("answers = %v, want a=0.91 b=0.04", answers)
	}

	if path != "/v1/systemone" || auth != "Bearer test-key" {
		t.Errorf("request to %s with Authorization %q", path, auth)
	}

	if got.Model != DefaultJevModel || got.State != "+fmt.Println(x)" {
		t.Errorf("model %q, state %q: want the default model and the state unchanged", got.Model, got.State)
	}

	if q := got.Questions["a"]; q.Type != "noul" || q.Instructions != "Does it print?" || len(got.Questions) != 2 {
		t.Errorf("questions = %+v", got.Questions)
	}
}

func TestJevProvider_AskRetriesRateLimitsAndOverload(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, statusOverloaded} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32

			provider := jevServer(t, func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) < 3 {
					w.WriteHeader(status)
					return
				}

				if _, err := w.Write([]byte(`{"answers":{"a":{"type":"noul","noul":0.5}}}`)); err != nil {
					t.Errorf("writing response: %v", err)
				}
			})

			answers, err := provider.Ask(t.Context(), "code", map[string]string{"a": "q"})
			if err != nil || answers["a"] != 0.5 {
				t.Fatalf("Ask = %v, %v; want the third attempt's answer", answers, err)
			}

			if calls.Load() != 3 {
				t.Errorf("calls = %d, want 3", calls.Load())
			}
		})
	}
}

func TestJevProvider_AskGivesUpAfterMaxRetries(t *testing.T) {
	var calls atomic.Int32

	provider := jevServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	})

	_, err := provider.Ask(t.Context(), "code", map[string]string{"a": "q"})

	var jevErr *JevError
	if !errors.As(err, &jevErr) || jevErr.Status != http.StatusTooManyRequests {
		t.Fatalf("err = %v, want a 429 JevError", err)
	}

	if calls.Load() != jevMaxRetries+1 {
		t.Errorf("calls = %d, want %d", calls.Load(), jevMaxRetries+1)
	}
}

func TestJevProvider_AskDoesNotRetryOtherErrors(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		body        string
		tooLarge    bool
		keyRejected bool
	}{
		{"input too large", http.StatusBadRequest, `{"detail":{"error_type":"max_tokens_exceeded"}}`, true, false},
		{"invalid key", http.StatusUnauthorized, `{"detail":{"error_type":"authentication_error","message":"bad key"}}`, false, true},
		{"forbidden key", http.StatusForbidden, `{"detail":"forbidden"}`, false, true},
		{"validation list", http.StatusUnprocessableEntity, `{"detail":[{"loc":["body","state"],"msg":"Field required"}]}`, false, false},
		{"plain string detail", http.StatusBadRequest, `{"detail":"unknown model"}`, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32

			provider := jevServer(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(tt.status)

				if _, err := w.Write([]byte(tt.body)); err != nil {
					t.Errorf("writing response: %v", err)
				}
			})

			_, err := provider.Ask(t.Context(), "code", map[string]string{"a": "q"})

			var jevErr *JevError
			if !errors.As(err, &jevErr) || jevErr.Status != tt.status {
				t.Fatalf("err = %v, want a %d JevError", err, tt.status)
			}

			if got := errors.Is(err, ErrInputTooLarge); got != tt.tooLarge {
				t.Errorf("errors.Is(err, ErrInputTooLarge) = %v, want %v", got, tt.tooLarge)
			}

			if got := errors.Is(err, ErrKeyRejected); got != tt.keyRejected {
				t.Errorf("errors.Is(err, ErrKeyRejected) = %v, want %v", got, tt.keyRejected)
			}

			if calls.Load() != 1 {
				t.Errorf("calls = %d, want 1", calls.Load())
			}
		})
	}
}

func TestJevProvider_AskRejectsAnAnswerWithoutAProbability(t *testing.T) {
	provider := jevServer(t, func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte(`{"answers":{"a":{"type":"noul"}}}`)); err != nil {
			t.Errorf("writing response: %v", err)
		}
	})

	if _, err := provider.Ask(t.Context(), "code", map[string]string{"a": "q"}); err == nil {
		t.Fatal("expected an error for an answer with no noul probability")
	}
}

func TestJevResponseError_ReadsRetryAfter(t *testing.T) {
	recorder := httptest.NewRecorder()
	recorder.Header().Set("Retry-After", "7")
	recorder.WriteHeader(http.StatusTooManyRequests)

	if got := jevResponseError(recorder.Result()); got.retryAfter != 7*time.Second {
		t.Errorf("retryAfter = %v, want 7s", got.retryAfter)
	}
}
