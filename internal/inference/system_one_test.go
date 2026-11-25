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

func systemOneServer(t *testing.T, handler http.HandlerFunc) *SystemOneProvider {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	provider, err := newSystemOneProvider(server.URL, "test-key", "jev-latest", server.Client())
	if err != nil {
		t.Fatalf("newSystemOneProvider: %v", err)
	}

	provider.retryWait = 0

	return provider
}

func TestSystemOneProvider_AskSendsOneNoulQuestionPerRule(t *testing.T) {
	var got struct {
		Model     string `json:"model"`
		State     string `json:"state"`
		Questions map[string]struct {
			Type         string `json:"type"`
			Instructions string `json:"instructions"`
		} `json:"questions"`
	}
	var path, auth string

	provider := systemOneServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decoding request: %v", err)
		}

		if _, err := w.Write([]byte(`{"model":"jev-1.13.0","answers":{"a":{"type":"noul","noul":0.91},"b":{"type":"noul","noul":0.04}},"usage":{"input_tokens":10,"output_tokens":2}}`)); err != nil {
			t.Errorf("writing response: %v", err)
		}
	})

	reply, err := provider.Ask(t.Context(), "+fmt.Println(x)", map[string]string{"a": "Does it print?", "b": "Does it panic?"})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	if reply.Answers["a"] != 0.91 || reply.Answers["b"] != 0.04 || len(reply.Answers) != 2 {
		t.Errorf("reply.Answers = %v, want a=0.91 b=0.04", reply.Answers)
	}

	if path != "/v1/systemone" || auth != "Bearer test-key" {
		t.Errorf("request to %s with Authorization %q", path, auth)
	}

	if got.Model != "jev-latest" || got.State != "+fmt.Println(x)" {
		t.Errorf("model %q, state %q: want the default model and the state unchanged", got.Model, got.State)
	}

	if q := got.Questions["a"]; q.Type != "noul" || q.Instructions != "Does it print?" || len(got.Questions) != 2 {
		t.Errorf("questions = %+v", got.Questions)
	}
}

func TestSystemOneProvider_AskRetriesRateLimitsAndOverload(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, statusOverloaded} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32

			provider := systemOneServer(t, func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) < 3 {
					w.WriteHeader(status)
					return
				}

				if _, err := w.Write([]byte(`{"answers":{"a":{"type":"noul","noul":0.5}}}`)); err != nil {
					t.Errorf("writing response: %v", err)
				}
			})

			reply, err := provider.Ask(t.Context(), "code", map[string]string{"a": "q"})
			if err != nil || reply.Answers["a"] != 0.5 {
				t.Fatalf("Ask = %v, %v; want the third attempt's answer", reply.Answers, err)
			}

			if calls.Load() != 3 {
				t.Errorf("calls = %d, want 3", calls.Load())
			}
		})
	}
}

func TestSystemOneProvider_AskGivesUpAfterMaxRetries(t *testing.T) {
	var calls atomic.Int32

	provider := systemOneServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	})

	_, err := provider.Ask(t.Context(), "code", map[string]string{"a": "q"})

	var apiErr *SystemOneError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusTooManyRequests {
		t.Fatalf("err = %v, want a 429 SystemOneError", err)
	}

	if calls.Load() != systemOneMaxRetries+1 {
		t.Errorf("calls = %d, want %d", calls.Load(), systemOneMaxRetries+1)
	}
}

func TestSystemOneProvider_AskDoesNotRetryOtherErrors(t *testing.T) {
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
		{"kev state over the limit", http.StatusUnprocessableEntity, `{"detail":"state is 70,000 tokens, over the 65,536 this server reads; or start the server with KEV_TRUNCATE_STATES=1 to read only its first 65,536 tokens (responses then say truncated: true)"}`, true, false},
		{"kev invalid request", http.StatusUnprocessableEntity, `{"detail":"question must be an existing choice question"}`, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32

			provider := systemOneServer(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(tt.status)

				if _, err := w.Write([]byte(tt.body)); err != nil {
					t.Errorf("writing response: %v", err)
				}
			})

			_, err := provider.Ask(t.Context(), "code", map[string]string{"a": "q"})

			var apiErr *SystemOneError
			if !errors.As(err, &apiErr) || apiErr.Status != tt.status {
				t.Fatalf("err = %v, want a %d SystemOneError", err, tt.status)
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

func TestSystemOneProvider_AskRejectsAnAnswerWithoutAProbability(t *testing.T) {
	provider := systemOneServer(t, func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte(`{"answers":{"a":{"type":"noul"}}}`)); err != nil {
			t.Errorf("writing response: %v", err)
		}
	})

	if _, err := provider.Ask(t.Context(), "code", map[string]string{"a": "q"}); err == nil {
		t.Fatal("expected an error for an answer with no noul probability")
	}
}

func TestSystemOneResponseError_ReadsRetryAfter(t *testing.T) {
	recorder := httptest.NewRecorder()
	recorder.Header().Set("Retry-After", "7")
	recorder.WriteHeader(http.StatusTooManyRequests)

	if got := systemOneResponseError(recorder.Result()); got.retryAfter != 7*time.Second {
		t.Errorf("retryAfter = %v, want 7s", got.retryAfter)
	}
}

func TestSystemOneProvider_AskReportsUsage(t *testing.T) {
	provider := systemOneServer(t, func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte(`{"answers":{"a":{"type":"noul","noul":0.5}},"usage":{"input_tokens":120,"output_tokens":3}}`)); err != nil {
			t.Errorf("writing response: %v", err)
		}
	})

	reply, err := provider.Ask(t.Context(), "code", map[string]string{"a": "q"})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	if want := (Usage{InputTokens: 120, OutputTokens: 3, Reported: true}); reply.Usage != want {
		t.Errorf("usage = %+v, want %+v", reply.Usage, want)
	}
}

func TestSystemOneProvider_AskMarksMissingUsageUnreported(t *testing.T) {
	provider := systemOneServer(t, func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte(`{"answers":{"a":{"type":"noul","noul":0.5}}}`)); err != nil {
			t.Errorf("writing response: %v", err)
		}
	})

	reply, err := provider.Ask(t.Context(), "code", map[string]string{"a": "q"})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	if reply.Usage.Reported {
		t.Errorf("usage = %+v, want unreported", reply.Usage)
	}
}

func TestSystemOneProvider_AskTreatsATruncatedAnswerAsTooLarge(t *testing.T) {
	provider := systemOneServer(t, func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte(`{"answers":{"a":{"type":"noul","noul":0.01}},"truncated":true}`)); err != nil {
			t.Errorf("writing response: %v", err)
		}
	})

	reply, err := provider.Ask(t.Context(), "code", map[string]string{"a": "q"})
	if !errors.Is(err, ErrInputTooLarge) {
		t.Fatalf("err = %v, want ErrInputTooLarge", err)
	}

	if reply.Answers != nil {
		t.Errorf("answers = %v, want none from a truncated reply", reply.Answers)
	}
}

func TestSystemOneProvider_AskSendsNoAuthorizationWithoutAKey(t *testing.T) {
	var auth []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Values("Authorization")

		if _, err := w.Write([]byte(`{"answers":{"a":{"type":"noul","noul":0.5}}}`)); err != nil {
			t.Errorf("writing response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	provider, err := NewSystemOneProvider(server.URL, "", "kev-latest", time.Minute)
	if err != nil {
		t.Fatalf("NewSystemOneProvider: %v", err)
	}

	if _, err := provider.Ask(t.Context(), "code", map[string]string{"a": "q"}); err != nil {
		t.Fatalf("Ask: %v", err)
	}

	if len(auth) != 0 {
		t.Errorf("Authorization = %q, want none", auth)
	}
}

func TestSystemOneProvider_AskJoinsTheEndpointToAPathPrefix(t *testing.T) {
	var path string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path

		if _, err := w.Write([]byte(`{"answers":{"a":{"type":"noul","noul":0.5}}}`)); err != nil {
			t.Errorf("writing response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	provider, err := NewSystemOneProvider(server.URL+"/kev", "", "kev-latest", time.Minute)
	if err != nil {
		t.Fatalf("NewSystemOneProvider: %v", err)
	}

	if _, err := provider.Ask(t.Context(), "code", map[string]string{"a": "q"}); err != nil {
		t.Fatalf("Ask: %v", err)
	}

	if path != "/kev/v1/systemone" {
		t.Errorf("path = %q, want /kev/v1/systemone", path)
	}
}

func TestSystemOneProvider_AskStopsAtTheTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })

	provider, err := NewSystemOneProvider(server.URL, "", "kev-latest", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("NewSystemOneProvider: %v", err)
	}

	_, err = provider.Ask(t.Context(), "code", map[string]string{"a": "q"})
	if err == nil || errors.Is(err, ErrInputTooLarge) || errors.Is(err, ErrKeyRejected) {
		t.Fatalf("err = %v, want a plain timeout failure", err)
	}
}

func TestNewSystemOneProvider_RejectsAnUnparsableBaseURL(t *testing.T) {
	if _, err := NewSystemOneProvider("http://[::1", "", "kev-latest", time.Minute); err == nil {
		t.Fatal("expected an error for an unparsable base url")
	}
}

func TestSystemOneProvider_AskReportsItsRetries(t *testing.T) {
	var calls atomic.Int32

	provider := systemOneServer(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}

		if _, err := w.Write([]byte(`{"answers":{"a":{"type":"noul","noul":0.5}}}`)); err != nil {
			t.Errorf("writing response: %v", err)
		}
	})

	reply, err := provider.Ask(t.Context(), "code", map[string]string{"a": "q"})
	if err != nil || reply.Retries != 2 {
		t.Fatalf("Ask = %+v, %v; want an answer after 2 retries", reply, err)
	}
}

func TestSystemOneProvider_AskGivesUpWhenRetryAfterOutlastsTheTimeout(t *testing.T) {
	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	provider, err := NewSystemOneProvider(server.URL, "", "kev-latest", time.Minute)
	if err != nil {
		t.Fatalf("NewSystemOneProvider: %v", err)
	}

	_, err = provider.Ask(t.Context(), "code", map[string]string{"a": "q"})

	var apiErr *SystemOneError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusTooManyRequests {
		t.Fatalf("err = %v, want the 429 without waiting an hour", err)
	}

	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", calls.Load())
	}
}

func TestSystemOneProvider_AskDoesNotFollowARedirect(t *testing.T) {
	var redirectedAuth atomic.Value

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectedAuth.Store(r.Header.Get("Authorization"))
	}))
	t.Cleanup(target.Close)

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(origin.Close)

	provider, err := NewSystemOneProvider(origin.URL, "secret", "jev-latest", time.Minute)
	if err != nil {
		t.Fatalf("NewSystemOneProvider: %v", err)
	}

	_, err = provider.Ask(t.Context(), "code", map[string]string{"a": "q"})

	var apiErr *SystemOneError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusTemporaryRedirect {
		t.Fatalf("err = %v, want the 307 itself as a failure", err)
	}

	if got := redirectedAuth.Load(); got != nil {
		t.Errorf("the redirect target was asked, with Authorization %q", got)
	}
}
