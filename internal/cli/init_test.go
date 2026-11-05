package cli

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/config"
)

const retiredPipelineConfig = "analysis:\n  adr_path: docs\n  pipeline:\n    rank:\n      top_k: 2\n"

func answering(answer string) prompter {
	return prompter{scanner: bufio.NewScanner(strings.NewReader(answer))}
}

func TestAddDefaultPipeline_KeepsTheFileModeAndASymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes and symlinks need a Unix file system")
	}

	dir := t.TempDir()

	target := filepath.Join(dir, "shared.yaml")
	if err := os.WriteFile(target, []byte(retiredPipelineConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(dir, "archguard.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := answering("y\n").addDefaultPipeline(link, []byte(retiredPipelineConfig)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("archguard.yaml is no longer a symlink (err %v)", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}

	got, err := os.ReadFile(target)
	if err != nil || !strings.Contains(string(got), "- name: shortlist") {
		t.Errorf("target = %q (err %v), want the default pipeline written through the link", got, err)
	}
}

func TestAddDefaultPipeline_ReportsAListPipelineCheckRejects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archguard.yaml")

	content := []byte("analysis:\n  pipeline: []\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := answering("").addDefaultPipeline(path, content); err == nil {
		t.Fatal("expected an error for a list pipeline check rejects, got nil")
	}

	if got, err := os.ReadFile(path); err != nil || string(got) != string(content) {
		t.Errorf("config = %q (err %v), want it untouched", got, err)
	}
}

func TestVerifySplice(t *testing.T) {
	before := []byte(retiredPipelineConfig)
	tests := []struct {
		name    string
		after   string
		wantErr bool
	}{
		{"only the pipeline changed", "analysis:\n  adr_path: docs\n" + splicedPipeline, false},
		{"another key changed", "analysis:\n  adr_path: elsewhere\n" + splicedPipeline, true},
		{"the new pipeline is invalid", "analysis:\n  adr_path: docs\n  pipeline: []\n", true},
		{"the result doesn't parse", "analysis:\n  adr_path: [docs\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifySplice(before, []byte(tt.after))
			if tt.wantErr && !errors.Is(err, errNotSpliceable) {
				t.Errorf("err = %v, want errNotSpliceable", err)
			}

			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func withStdin(t *testing.T, answers string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(path, []byte(answers), 0o600); err != nil {
		t.Fatal(err)
	}

	stdin, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}

	orig := os.Stdin
	os.Stdin = stdin

	t.Cleanup(func() {
		os.Stdin = orig
		_ = stdin.Close() //nolint:errcheck // a read-only test file
	})
}

func TestExecute_InitInAFreshRepo(t *testing.T) {
	tests := []struct {
		name         string
		answers      string
		wantADRDir   bool
		wantTemplate bool
	}{
		{"creates the ADR directory and template", "\ny\ny\n", true, true},
		{"declines the template", "\ny\nn\n", true, false},
		{"declines the ADR directory", "\nn\n", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupExecuteTestRepo(t)
			withStdin(t, tt.answers)

			if code, _ := runExecute(t, ProviderFactories{}, "init"); code != ExitSuccess {
				t.Fatalf("init exit = %d, want %d", code, ExitSuccess)
			}

			cfg, err := config.LoadConfig(configFilename)
			if err != nil {
				t.Fatalf("init wrote a config that doesn't load: %v", err)
			}

			if _, err := config.ParsePipeline(cfg); err != nil {
				t.Errorf("init wrote a pipeline check rejects: %v", err)
			}

			if _, err := os.Stat(defaultADRPath); (err == nil) != tt.wantADRDir {
				t.Errorf("ADR directory exists = %v, want %v", err == nil, tt.wantADRDir)
			}

			if _, err := os.Stat(filepath.Join(defaultADRPath, "ADR_TEMPLATE.md")); (err == nil) != tt.wantTemplate {
				t.Errorf("template exists = %v, want %v", err == nil, tt.wantTemplate)
			}

			if info, err := os.Stat(filepath.Join(".archguard", "cache")); err != nil || !info.IsDir() {
				t.Errorf("init made no .archguard/cache directory: %v", err)
			}

			if gitignore, err := os.ReadFile(".gitignore"); err != nil || string(gitignore) != ".archguard/\n" {
				t.Errorf(".gitignore = %q, %v; want only the .archguard/ entry", gitignore, err)
			}
		})
	}
}

func TestExecute_InitUsesAnExistingADRDirectory(t *testing.T) {
	setupExecuteTestRepo(t)

	if err := os.MkdirAll("adrs", 0o755); err != nil {
		t.Fatal(err)
	}

	withStdin(t, "adrs\n")

	if code, _ := runExecute(t, ProviderFactories{}, "init"); code != ExitSuccess {
		t.Fatalf("init exit = %d, want %d", code, ExitSuccess)
	}

	cfg, err := config.LoadConfig(configFilename)
	if err != nil {
		t.Fatalf("init wrote a config that doesn't load: %v", err)
	}

	if cfg.Analysis.ADRPath != "adrs" {
		t.Errorf("analysis.adr_path = %q, want adrs", cfg.Analysis.ADRPath)
	}
}

func TestEnsureGitignore(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		want     string
	}{
		{"adds the entry after a last line with no newline", "bin/", "bin/\n.archguard/\n"},
		{"adds the entry after a last line with a newline", "bin/\n", "bin/\n.archguard/\n"},
		{"leaves an existing entry alone", "bin/\n  .archguard/  \n", "bin/\n  .archguard/  \n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())

			if err := os.WriteFile(".gitignore", []byte(tt.existing), 0o644); err != nil {
				t.Fatal(err)
			}

			captureStdout(t, func() {
				if err := ensureGitignore(); err != nil {
					t.Errorf("ensureGitignore failed: %v", err)
				}
			})

			if got, err := os.ReadFile(".gitignore"); err != nil || string(got) != tt.want {
				t.Errorf(".gitignore = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}
