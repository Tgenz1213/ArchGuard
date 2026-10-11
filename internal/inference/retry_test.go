package inference

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v4"
)

func TestAnalyzeDrift_Retry(t *testing.T) {
	attempts := 0
	provider := &MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			attempts++
			if attempts < 3 {
				return "", fmt.Errorf("simulated 429 error")
			}

			return `{"violation": false, "reasoning": "success", "quoted_code": ""}`, nil
		},
	}

	start := time.Now()
	res, retries, err := AnalyzeDrift(t.Context(), provider, DriftInput{ADRContent: "adr", CodeContext: "code", Filename: "file.go"}, "system")
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}

	if res == nil {
		t.Fatal("Expected result, got nil")
	}

	if attempts != 3 || retries != 2 {
		t.Errorf("attempts = %d, retries = %d, want 3 and 2", attempts, retries)
	}

	if duration < 2*time.Second {
		t.Errorf("Expected backoff delay, got %v", duration)
	}
}

func TestAnalyzeDrift_MaxRetriesExceeded(t *testing.T) {
	attempts := 0
	provider := &MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			attempts++
			return "", fmt.Errorf("persistent error")
		},
	}

	_, retries, err := AnalyzeDrift(t.Context(), provider, DriftInput{ADRContent: "adr", CodeContext: "code", Filename: "file.go"}, "system")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	if attempts != 4 || retries != 3 {
		t.Errorf("attempts = %d, retries = %d, want 4 and 3", attempts, retries)
	}
}

func TestAnalyzeDrift_PermanentErrorIsNotRetried(t *testing.T) {
	provider := &MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			return "", backoff.Permanent(errors.New("bad request"))
		},
	}

	_, retries, err := AnalyzeDrift(t.Context(), provider, DriftInput{ADRContent: "adr", CodeContext: "code", Filename: "file.go"}, "system")
	if err == nil || !strings.Contains(err.Error(), "failed after 0 retries") {
		t.Fatalf("err = %v, want one that names 0 retries", err)
	}

	if retries != 0 {
		t.Errorf("retries = %d, want 0", retries)
	}
}

func TestAnalyzeDrift_ContextCancelled(t *testing.T) {
	provider := &MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			return "", fmt.Errorf("simulated error")
		},
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, err := AnalyzeDrift(ctx, provider, DriftInput{ADRContent: "adr", CodeContext: "code", Filename: "file.go"}, "system")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
