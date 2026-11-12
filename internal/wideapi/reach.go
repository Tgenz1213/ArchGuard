package wideapi

type pkg struct {
	ImportPath string
	Imports    []string
}

func reaches(pkgs []pkg) map[string]int {
	imports := make(map[string][]string, len(pkgs))
	for _, p := range pkgs {
		imports[p.ImportPath] = p.Imports
	}

	reach := make(map[string]int, len(pkgs))

	for _, p := range pkgs {
		seen := map[string]bool{}
		walk(p.ImportPath, imports, seen)

		for imported := range seen {
			reach[imported]++
		}
	}

	return reach
}

func walk(from string, imports map[string][]string, seen map[string]bool) {
	for _, imported := range imports[from] {
		if _, inModule := imports[imported]; !inModule || seen[imported] {
			continue
		}

		seen[imported] = true
		walk(imported, imports, seen)
	}
}
