package cli

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/baseline"
	"github.com/tgenz1213/archguard/internal/output"
)

func TestResolvePaths(t *testing.T) {
	repoRoot := t.TempDir()
	cwd := filepath.Join(repoRoot, "sub")
	elsewhere := filepath.Join(t.TempDir(), "report.txt")

	tests := []struct {
		name     string
		path     string
		wantPath string
	}{
		{"unset stays unset", "", ""},
		{"relative resolves against the starting directory", "report.txt", filepath.Join(cwd, "report.txt")},
		{"parent-relative resolves against the starting directory", filepath.Join("..", "report.txt"), filepath.Join(repoRoot, "report.txt")},
		{"absolute is kept", elsewhere, elsewhere},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := checkCmd{Output: tt.path, configOption: configOption{Config: tt.path}, Paths: []string{"main.go"}}
			index := indexCmd{configOption: configOption{Config: tt.path}}

			cmd.resolvePaths(cwd, repoRoot)
			index.resolvePaths(cwd)

			if cmd.Output != tt.wantPath {
				t.Errorf("Output = %q, want %q", cmd.Output, tt.wantPath)
			}

			if cmd.Config != tt.wantPath {
				t.Errorf("check Config = %q, want %q", cmd.Config, tt.wantPath)
			}

			if index.Config != tt.wantPath {
				t.Errorf("index Config = %q, want %q", index.Config, tt.wantPath)
			}

			if want := "sub/main.go"; cmd.Paths[0] != want {
				t.Errorf("Paths[0] = %q, want %q (relative to the repo root)", cmd.Paths[0], want)
			}
		})
	}
}

func TestInvocationConfigPath(t *testing.T) {
	tests := []struct {
		name string
		inv  invocation
		want string
	}{
		{"check defaults to archguard.yaml", invocation{command: "check"}, configFilename},
		{"index defaults to archguard.yaml", invocation{command: "index"}, configFilename},
		{"check uses --config", invocation{command: "check", check: checkCmd{configOption: configOption{Config: "ci.yaml"}}}, "ci.yaml"},
		{"index uses --config", invocation{command: "index", index: indexCmd{configOption: configOption{Config: "ci.yaml"}}}, "ci.yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.inv.configPath(); got != tt.want {
				t.Errorf("configPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseCommandLine(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantCommand string
		wantCode    ExitCode
		wantErr     bool
		wantOut     string
		wantErrOut  string
		check       func(t *testing.T, c checkCmd)
	}{
		{name: "check with paths", args: []string{"check", "a.go", "b.go"}, wantCommand: "check", check: func(t *testing.T, c checkCmd) {
			if len(c.Paths) != 2 || c.Paths[0] != "a.go" || c.Paths[1] != "b.go" {
				t.Errorf("Paths = %v", c.Paths)
			}
		}},
		{name: "flags after paths are parsed", args: []string{"check", "a.go", "--debug"}, wantCommand: "check", check: func(t *testing.T, c checkCmd) {
			if !c.Debug || len(c.Paths) != 1 {
				t.Errorf("Debug = %v, Paths = %v", c.Debug, c.Paths)
			}
		}},
		{name: "value flag is not a path", args: []string{"check", "--update-baseline", "--baseline-reason", "accepted-debt"}, wantCommand: "check", check: func(t *testing.T, c checkCmd) {
			if c.BaselineReason != "accepted-debt" || len(c.Paths) != 0 {
				t.Errorf("BaselineReason = %q, Paths = %v", c.BaselineReason, c.Paths)
			}
		}},
		{name: "format json", args: []string{"check", "--format", "json"}, wantCommand: "check", check: func(t *testing.T, c checkCmd) {
			if !c.jsonOutput() {
				t.Error("jsonOutput() = false, want true")
			}
		}},
		{name: "format json ignored with update-baseline", args: []string{"check", "--format=json", "--update-baseline"}, wantCommand: "check", check: func(t *testing.T, c checkCmd) {
			if c.jsonOutput() {
				t.Error("jsonOutput() = true, want false")
			}
		}},
		{name: "since", args: []string{"check", "--since", "abc123"}, wantCommand: "check", check: func(t *testing.T, c checkCmd) {
			if c.Since != "abc123" || len(c.Paths) != 0 {
				t.Errorf("Since = %q, Paths = %v", c.Since, c.Paths)
			}
		}},
		{name: "ci without a scope", args: []string{"check", "--ci"}, wantCode: ExitUsage, wantErr: true},
		{name: "ci with since", args: []string{"check", "--ci", "--since", "abc123"}, wantCommand: "check"},
		{name: "ci with all", args: []string{"check", "--ci", "--all"}, wantCommand: "check"},
		{name: "ci with staged", args: []string{"check", "--ci", "--staged"}, wantCommand: "check"},
		{name: "ci with a path", args: []string{"check", "--ci", "main.go"}, wantCommand: "check"},
		{name: "ci with update-baseline", args: []string{"check", "--ci", "--update-baseline"}, wantCommand: "check"},
		{name: "dry run", args: []string{"check", "--dry-run", "--all"}, wantCommand: "check"},
		{name: "dry run with update-baseline", args: []string{"check", "--dry-run", "--update-baseline"}, wantCode: ExitUsage, wantErr: true},
		{name: "since with an empty ref", args: []string{"check", "--since", ""}, wantCode: ExitUsage, wantErr: true},
		{name: "since= with an empty ref", args: []string{"check", "--since="}, wantCode: ExitUsage, wantErr: true},
		{name: "since with staged", args: []string{"check", "--since", "abc123", "--staged"}, wantCode: ExitUsage, wantErr: true},
		{name: "since with all", args: []string{"check", "--since", "abc123", "--all"}, wantCode: ExitUsage, wantErr: true},
		{name: "since with update-baseline", args: []string{"check", "--since", "abc123", "--update-baseline"}, wantCode: ExitUsage, wantErr: true},
		{name: "since with whole-file", args: []string{"check", "--since", "abc123", "--whole-file"}, wantCommand: "check", check: func(t *testing.T, c checkCmd) {
			if !c.WholeFile {
				t.Error("WholeFile = false, want true")
			}
		}},
		{name: "whole-file without since", args: []string{"check", "--whole-file"}, wantCode: ExitUsage, wantErr: true},
		{name: "whole-file with all", args: []string{"check", "--all", "--whole-file"}, wantCode: ExitUsage, wantErr: true},
		{name: "since with a path", args: []string{"check", "--since", "abc123", "main.go"}, wantCode: ExitUsage, wantErr: true},
		{name: "debug-rejected with debug", args: []string{"check", "--debug", "--debug-rejected"}, wantCommand: "check", check: func(t *testing.T, c checkCmd) {
			if !c.DebugRejected {
				t.Error("DebugRejected = false, want true")
			}
		}},
		{name: "debug-rejected without debug", args: []string{"check", "--debug-rejected"}, wantCode: ExitUsage, wantErr: true},
		{name: "index", args: []string{"index"}, wantCommand: "index"},
		{name: "init", args: []string{"init"}, wantCommand: "init"},
		{name: "--help", args: []string{"--help"}, wantOut: "Usage: archguard <command>"},
		{name: "-h", args: []string{"-h"}, wantOut: "Usage: archguard <command>"},
		{name: "help", args: []string{"help"}, wantOut: "Usage: archguard <command>"},
		{name: "check --help after flags", args: []string{"check", "--format", "json", "--help"}, wantOut: "Scan staged files only"},
		{name: "index --help", args: []string{"index", "--help"}, wantOut: "Usage: archguard index"},
		{name: "check --help lists --config", args: []string{"check", "--help"}, wantOut: "--config=PATH"},
		{name: "index --help lists --config", args: []string{"index", "--help"}, wantOut: "--config=PATH"},
		{name: "--version", args: []string{"--version"}, wantOut: "ArchGuard version"},
		{name: "no command", args: nil, wantCode: ExitUsage, wantErr: true, wantErrOut: "Usage: archguard <command>"},
		{name: "unknown command", args: []string{"typo"}, wantCode: ExitUsage, wantErr: true, wantErrOut: "Usage: archguard <command>"},
		{name: "invalid format", args: []string{"check", "--format", "xml"}, wantCode: ExitUsage, wantErr: true},
		{name: "single-dash long flag", args: []string{"check", "-debug"}, wantCode: ExitUsage, wantErr: true},
		{name: "invalid color", args: []string{"check", "--color", "rainbow"}, wantCode: ExitUsage, wantErr: true},
		{name: "invalid index color", args: []string{"index", "--color=rainbow"}, wantCode: ExitUsage, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut bytes.Buffer

			inv, code, err := parseCommandLine(tt.args, &out, &errOut, "test")

			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}

			if !strings.Contains(out.String(), tt.wantOut) {
				t.Errorf("stdout missing %q, got:\n%s", tt.wantOut, out.String())
			}

			if !strings.Contains(errOut.String(), tt.wantErrOut) {
				t.Errorf("stderr missing %q, got:\n%s", tt.wantErrOut, errOut.String())
			}

			if tt.wantErr && out.Len() > 0 {
				t.Errorf("a usage error must leave stdout empty, got:\n%s", out.String())
			}

			if tt.wantCommand == "" {
				if inv != nil {
					t.Fatalf("expected the command line to be handled during parsing, got command %q", inv.command)
				}

				if code != tt.wantCode {
					t.Errorf("exit code = %d, want %d", code, tt.wantCode)
				}

				return
			}

			if inv == nil || inv.command != tt.wantCommand {
				t.Fatalf("invocation = %+v, want command %q", inv, tt.wantCommand)
			}

			if tt.check != nil {
				tt.check(t, inv.check)
			}
		})
	}
}

func TestParseCommandLine_Color(t *testing.T) {
	tests := []struct {
		args []string
		want output.ColorMode
	}{
		{[]string{"check"}, output.ColorAuto},
		{[]string{"check", "--color=always"}, output.ColorAlways},
		{[]string{"check", "a.go", "--color", "never"}, output.ColorNever},
		{[]string{"index"}, output.ColorAuto},
		{[]string{"index", "--color=always"}, output.ColorAlways},
		{[]string{"index", "--color=never"}, output.ColorNever},
		{[]string{"init"}, output.ColorAuto},
	}

	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			inv, _, err := parseCommandLine(tt.args, io.Discard, io.Discard, "test")
			if err != nil || inv == nil {
				t.Fatalf("parseCommandLine(%v) = %v, %v", tt.args, inv, err)
			}

			if got := inv.color(); got != tt.want {
				t.Errorf("color() = %q, want %q", got, tt.want)
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestParseCommandLine_OutputWriteFailureExitsOne(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"check", "--help"}, {"--version"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			inv, code, err := parseCommandLine(args, failingWriter{}, io.Discard, "test")
			if inv != nil || code != ExitError || err == nil {
				t.Fatalf("got (%v, %d, %v), want (nil, %d, error)", inv, code, err, ExitError)
			}
		})
	}
}

func TestParseCommandLine_UsageErrorIgnoresBrokenStreams(t *testing.T) {
	inv, code, err := parseCommandLine([]string{"typo"}, failingWriter{}, failingWriter{}, "test")
	if inv != nil || code != ExitUsage || err == nil || !strings.Contains(err.Error(), "typo") {
		t.Fatalf("got (%v, %d, %v), want the usage error with exit %d", inv, code, err, ExitUsage)
	}
}

func TestNormalizePaths(t *testing.T) {
	repoRoot := filepath.Clean(t.TempDir())

	tests := []struct {
		name string
		path string
		cwd  string
		want string
	}{
		{"cleans a relative path at the repo root", "./sub/../file.go", repoRoot, "file.go"},
		{"resolves from a subdirectory", "cli.go", filepath.Join(repoRoot, "internal", "cli"), "internal/cli/cli.go"},
		{"handles an absolute path", filepath.Join(repoRoot, "sub", "file.go"), repoRoot, "sub/file.go"},
		{"leaves an empty path empty rather than a whole-repo scan", "", repoRoot, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths := []string{tt.path}
			normalizePaths(paths, tt.cwd, repoRoot)

			if paths[0] != tt.want {
				t.Errorf("got %q, want %q", paths[0], tt.want)
			}
		})
	}
}

func TestNormalizePaths_MatchesBaselineEntryRecordedWithForwardSlashes(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("backslash-as-separator is a Windows-only path.filepath behavior")
	}

	repoRoot := filepath.Clean(t.TempDir())

	paths := []string{`internal\analysis\engine.go`}
	normalizePaths(paths, repoRoot, repoRoot)

	b := baseline.New()
	b.Add(baseline.Entry{ADRID: "0001", File: "internal/analysis/engine.go", QuotedCode: "quoted violating code"})

	if !b.IsSuppressed("0001", paths[0], "some context\nquoted violating code\nmore context") {
		t.Errorf("normalized path %q should match a baseline entry recorded with forward slashes", paths[0])
	}
}
