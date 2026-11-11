//go:build ignore

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const minReach = 5

var exported = regexp.MustCompile(`^[+-](\t[A-Z]|func (\([^)]*\) )?[A-Z]|type [A-Z]|var [A-Z]|const [A-Z])`)

type pkg struct {
	ImportPath string
	Dir        string
	Imports    []string
}

func main() {
	base := "origin/main"
	if len(os.Args) > 1 {
		base = os.Args[1]
	}

	pkgs := listPackages()
	reach := reaches(pkgs)

	out, err := exec.Command("git", "diff", "--unified=0", base+"...HEAD", "--", "*.go").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "wideapi: git diff failed:", err)
		return
	}

	dirs := make(map[string]string, len(pkgs))
	for _, p := range pkgs {
		dirs[filepath.ToSlash(p.Dir)] = p.ImportPath
	}

	changed := map[string][]string{}
	file := ""
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	scanner.Buffer(make([]byte, 1<<20), 1<<20)

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "+++ b/"):
			file = strings.TrimPrefix(line, "+++ b/")
		case strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "+++ "):
		case file != "" && !strings.HasSuffix(file, "_test.go") && exported.MatchString(line):
			changed[file] = append(changed[file], line)
		}
	}

	var report []string
	for f, lines := range changed {
		ip, ok := dirs[pathDir(f)]
		if !ok || reach[ip] < minReach {
			continue
		}

		report = append(report, fmt.Sprintf("  %s (reach %d): %d exported line(s) changed", f, reach[ip], len(lines)))
	}

	if len(report) == 0 {
		return
	}

	sort.Strings(report)
	fmt.Fprintln(os.Stderr, "wide-API: exported API changed in packages that "+fmt.Sprint(minReach)+"+ packages import:")
	fmt.Fprintln(os.Stderr, strings.Join(report, "\n"))
	fmt.Fprintln(os.Stderr, "  Could the change live in a dependent (internal/analysis, internal/cli) instead? See .claude/rules/wide-packages.md.")
}

func pathDir(f string) string {
	cwd, _ := os.Getwd()
	return filepath.ToSlash(filepath.Join(cwd, filepath.Dir(f)))
}

func listPackages() []pkg {
	out, err := exec.Command("go", "list", "-json=ImportPath,Dir,Imports", "./...").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "wideapi: go list failed:", err)
		os.Exit(0)
	}

	var pkgs []pkg
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var p pkg
		if err := dec.Decode(&p); err != nil {
			break
		}

		pkgs = append(pkgs, p)
	}

	return pkgs
}

func reaches(pkgs []pkg) map[string]int {
	known := map[string]bool{}
	for _, p := range pkgs {
		known[p.ImportPath] = true
	}

	deps := map[string][]string{}
	for _, p := range pkgs {
		for _, i := range p.Imports {
			if known[i] {
				deps[p.ImportPath] = append(deps[p.ImportPath], i)
			}
		}
	}

	reach := map[string]int{}
	for _, p := range pkgs {
		seen := map[string]bool{}
		var walk func(string)
		walk = func(s string) {
			for _, d := range deps[s] {
				if !seen[d] {
					seen[d] = true
					walk(d)
				}
			}
		}
		walk(p.ImportPath)

		for d := range seen {
			reach[d]++
		}
	}

	return reach
}
