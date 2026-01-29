package llm

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// MockProvider is defined in mock.go

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
	res, err := AnalyzeDrift(context.Background(), provider, "adr", "code", "file.go", "system")
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}

	if res == nil {
		t.Fatal("Expected result, got nil")
	}

	if attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts)
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

	_, err := AnalyzeDrift(context.Background(), provider, "adr", "code", "file.go", "system")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	if attempts != 4 {
		t.Errorf("Expected 4 attempts, got %d", attempts)
	}
}
