package hooks

import "testing"

func TestBlockEnvHook(t *testing.T) {
	bash := findBash(t)

	tests := []struct {
		name     string
		command  string
		wantCode int
	}{
		{"cat", "cat .env", 2},
		{"python one-liner", `python3 -c "print(open('.env').read())"`, 2},
		{"grep include", "grep -r KEY --include=.env .", 2},
		{"nested suffix", "cat internal/x/.env.local", 2},
		{"absolute path", "cat /c/github/archguard/.env", 2},
		{"after a pipe", "ls | xargs cat .env", 2},
		{"go test", "go test ./...", 0},
		{"git log", "git log --oneline", 0},
		{"godotenv", "go doc github.com/joho/godotenv", 0},
		{"envrc", "ls .envrc", 0},
		{"environment", "echo .environment", 0},
		{"empty", "", 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := runHook(t, bash, blockEnv, tc.command); got != tc.wantCode {
				t.Errorf("exit code for %q = %d, want %d", tc.command, got, tc.wantCode)
			}
		})
	}
}
