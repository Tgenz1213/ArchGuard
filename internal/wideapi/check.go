package wideapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"slices"
	"strings"
)

const DefaultMinReach = 5

type Change struct {
	Dir   string
	Reach int
	Names []string
}

type Result struct {
	Changes  []Change
	Warnings []string
}

// A package that can't be read is a warning, not an error, so one bad package doesn't hide the others.
func Check(ctx context.Context, dir, base, head string, minReach int) (Result, error) {
	g := repo{dir: dir}

	mergeBase, err := g.run(ctx, "merge-base", base, head)
	if err != nil {
		return Result{}, err
	}

	from := strings.TrimSpace(string(mergeBase))

	changed, err := g.changedDirs(ctx, from, head)
	if err != nil || len(changed) == 0 {
		return Result{}, err
	}

	tree, cleanup, err := g.treeAt(ctx, head)
	if err != nil {
		return Result{}, err
	}

	result, err := g.compare(ctx, scan{tree: tree, from: from, head: head, changed: changed, minReach: minReach})

	return result, errors.Join(err, cleanup())
}

type scan struct {
	tree, from, head string
	changed          map[string]bool
	minReach         int
}

func (r repo) compare(ctx context.Context, sc scan) (Result, error) {
	pkgs, module, err := listPackages(ctx, sc.tree)
	if err != nil {
		return Result{}, err
	}

	reach := reaches(pkgs)

	var result Result

	for _, p := range pkgs {
		rel, ok := strings.CutPrefix(p.ImportPath, module+"/")
		if !ok || !sc.changed[rel] || reach[p.ImportPath] < sc.minReach {
			continue
		}

		names, err := r.changedAPI(ctx, sc.from, sc.head, rel)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s not checked: %v", rel, err))
			continue
		}

		if len(names) > 0 {
			result.Changes = append(result.Changes, Change{Dir: rel, Reach: reach[p.ImportPath], Names: names})
		}
	}

	slices.SortFunc(result.Changes, func(a, b Change) int { return strings.Compare(a.Dir, b.Dir) })

	return result, nil
}

func (r Result) String(minReach int) string {
	var out strings.Builder

	for _, warning := range r.Warnings {
		fmt.Fprintf(&out, "wideapi: %s\n", warning)
	}

	if len(r.Changes) == 0 {
		return out.String()
	}

	fmt.Fprintf(&out, "wide-API: exported API changed in packages that %d+ packages import:\n", minReach)

	for _, c := range r.Changes {
		fmt.Fprintf(&out, "  %s (reach %d): %s\n", c.Dir, c.Reach, summarize(c.Names))
	}

	out.WriteString("  Could the change live in a dependent (internal/analysis, internal/cli) instead? See .claude/rules/wide-packages.md.\n")

	return out.String()
}

func summarize(names []string) string {
	const shown = 6

	if len(names) <= shown {
		return strings.Join(names, ", ")
	}

	return fmt.Sprintf("%s, and %d more", strings.Join(names[:shown], ", "), len(names)-shown)
}

func diffSurfaces(before, after map[string]string) []string {
	var names []string

	for key, text := range after {
		if old, ok := before[key]; !ok {
			names = append(names, "+"+key)
		} else if old != text {
			names = append(names, "~"+key)
		}
	}

	for key := range before {
		if _, ok := after[key]; !ok {
			names = append(names, "-"+key)
		}
	}

	slices.Sort(names)

	return names
}

type repo struct{ dir string }

func (r repo) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", r.dir, "-c", "core.quotepath=off"}, args...)...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}

	return out, nil
}

func (r repo) changedDirs(ctx context.Context, from, head string) (map[string]bool, error) {
	out, err := r.run(ctx, "diff", "--name-only", "-z", "--no-renames", "--no-ext-diff", from, head, "--")
	if err != nil {
		return nil, err
	}

	dirs := map[string]bool{}

	for name := range strings.SplitSeq(string(out), "\x00") {
		if isSource(name) {
			dirs[path.Dir(name)] = true
		}
	}

	return dirs, nil
}

func isSource(name string) bool {
	return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
}

func (r repo) changedAPI(ctx context.Context, from, head, dir string) ([]string, error) {
	before, err := r.surface(ctx, from, dir)
	if err != nil {
		return nil, err
	}

	after, err := r.surface(ctx, head, dir)
	if err != nil {
		return nil, err
	}

	return diffSurfaces(before, after), nil
}

func (r repo) surface(ctx context.Context, ref, dir string) (map[string]string, error) {
	listing, err := r.run(ctx, "ls-tree", "-z", "--name-only", ref, "--", dir+"/")
	if err != nil {
		return nil, err
	}

	files := map[string][]byte{}

	for name := range strings.SplitSeq(string(listing), "\x00") {
		if !isSource(name) || path.Dir(name) != dir {
			continue
		}

		if files[name], err = r.run(ctx, "show", ref+":"+name); err != nil {
			return nil, err
		}
	}

	return Surface(files)
}

// Reach is counted on the commit being checked, so this checkout stands in only when it is already at head.
func (r repo) treeAt(ctx context.Context, head string) (string, func() error, error) {
	headSHA, err := r.run(ctx, "rev-parse", "--verify", head+"^{commit}")
	if err != nil {
		return "", nil, err
	}

	current, err := r.run(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", nil, err
	}

	if bytes.Equal(headSHA, current) {
		return r.dir, func() error { return nil }, nil
	}

	tmp, err := os.MkdirTemp("", "wideapi-")
	if err != nil {
		return "", nil, err
	}

	if _, err := r.run(ctx, "worktree", "add", "--detach", tmp, strings.TrimSpace(string(headSHA))); err != nil {
		return "", nil, errors.Join(err, os.RemoveAll(tmp))
	}

	return tmp, func() error {
		_, err := r.run(context.WithoutCancel(ctx), "worktree", "remove", "--force", tmp)

		return errors.Join(err, os.RemoveAll(tmp))
	}, nil
}

func listPackages(ctx context.Context, dir string) ([]pkg, string, error) {
	type listed struct {
		pkg
		Module *struct{ Path string }
	}

	cmd := exec.CommandContext(ctx, "go", "list", "-e", "-json=ImportPath,Imports,Module", "./...")
	cmd.Dir = dir

	out, err := cmd.Output()
	if err != nil {
		return nil, "", fmt.Errorf("go list: %w", err)
	}

	var (
		pkgs   []pkg
		module string
	)

	for dec := json.NewDecoder(bytes.NewReader(out)); dec.More(); {
		var p listed
		if err := dec.Decode(&p); err != nil {
			return nil, "", err
		}

		pkgs = append(pkgs, p.pkg)

		if p.Module != nil {
			module = p.Module.Path
		}
	}

	return pkgs, module, nil
}
