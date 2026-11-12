package wideapi

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"maps"
	"slices"
	"strings"
)

func Surface(files map[string][]byte) (map[string]string, error) {
	variants := map[string][]string{}
	fset := token.NewFileSet()

	for _, name := range slices.Sorted(maps.Keys(files)) {
		file, err := parser.ParseFile(fset, name, files[name], parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}

		for _, decl := range file.Decls {
			if err := addDecl(variants, fset, decl); err != nil {
				return nil, err
			}
		}
	}

	surface := make(map[string]string, len(variants))

	for key, texts := range variants {
		slices.Sort(texts)
		surface[key] = strings.Join(texts, " | ")
	}

	return surface, nil
}

func addDecl(variants map[string][]string, fset *token.FileSet, decl ast.Decl) error {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		return addFunc(variants, fset, d)
	case *ast.GenDecl:
		return addGen(variants, fset, d)
	}

	return nil
}

func addFunc(variants map[string][]string, fset *token.FileSet, d *ast.FuncDecl) error {
	key := "func " + d.Name.Name

	if !d.Name.IsExported() {
		return nil
	}

	if d.Recv != nil && len(d.Recv.List) == 1 {
		recv := receiverName(d.Recv.List[0].Type)
		if !ast.IsExported(recv) {
			return nil
		}

		key = "func " + recv + "." + d.Name.Name
	}

	d.Body, d.Doc = nil, nil

	return record(variants, fset, key, d, "")
}

func addGen(variants map[string][]string, fset *token.FileSet, d *ast.GenDecl) error {
	switch d.Tok {
	case token.TYPE:
		return addTypes(variants, fset, d)
	case token.CONST, token.VAR:
		return addValues(variants, fset, d)
	}

	return nil
}

func addTypes(variants map[string][]string, fset *token.FileSet, d *ast.GenDecl) error {
	for _, s := range d.Specs {
		spec := s.(*ast.TypeSpec)
		if !spec.Name.IsExported() {
			continue
		}

		suffix := ""
		if narrow(spec.Type) {
			suffix = " [sealed]"
		}

		node := &ast.GenDecl{Tok: token.TYPE, Specs: []ast.Spec{spec}}
		if err := record(variants, fset, "type "+spec.Name.Name, node, suffix); err != nil {
			return err
		}
	}

	return nil
}

func addValues(variants map[string][]string, fset *token.FileSet, d *ast.GenDecl) error {
	var last *ast.ValueSpec

	for i, s := range d.Specs {
		spec := s.(*ast.ValueSpec)

		if len(spec.Values) > 0 {
			last = spec
		} else if d.Tok == token.CONST && last != nil {
			spec = &ast.ValueSpec{Names: spec.Names, Type: last.Type, Values: last.Values}
		}

		names := exportedNames(spec.Names)
		if len(names) == 0 {
			continue
		}

		suffix := ""
		if d.Tok == token.CONST && usesIota(spec.Values) {
			suffix = fmt.Sprintf(" iota=%d", i)
		}

		node := &ast.GenDecl{Tok: d.Tok, Specs: []ast.Spec{spec}}
		if err := record(variants, fset, d.Tok.String()+" "+strings.Join(names, ","), node, suffix); err != nil {
			return err
		}
	}

	return nil
}

// An interface with an unexported method can't be implemented outside the package, so gaining or losing one is an API change.
func narrow(expr ast.Expr) (sealed bool) {
	switch t := expr.(type) {
	case *ast.StructType:
		t.Fields = &ast.FieldList{List: exportedFields(t.Fields.List)}
	case *ast.InterfaceType:
		for _, m := range t.Methods.List {
			if len(m.Names) > 0 && !m.Names[0].IsExported() {
				sealed = true
			}
		}

		t.Methods = &ast.FieldList{List: exportedFields(t.Methods.List)}
	}

	return sealed
}

func exportedFields(list []*ast.Field) []*ast.Field {
	var kept []*ast.Field

	for _, f := range list {
		if len(f.Names) == 0 {
			kept = append(kept, f)
			continue
		}

		if names := slices.DeleteFunc(slices.Clone(f.Names), func(n *ast.Ident) bool { return !n.IsExported() }); len(names) > 0 {
			kept = append(kept, &ast.Field{Names: names, Type: f.Type, Tag: f.Tag})
		}
	}

	return kept
}

func exportedNames(idents []*ast.Ident) []string {
	var names []string

	for _, id := range idents {
		if id.IsExported() {
			names = append(names, id.Name)
		}
	}

	return names
}

func usesIota(values []ast.Expr) bool {
	found := false

	for _, v := range values {
		ast.Inspect(v, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name == "iota" {
				found = true
			}

			return !found
		})
	}

	return found
}

func record(variants map[string][]string, fset *token.FileSet, key string, node ast.Node, suffix string) error {
	ast.Inspect(node, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.FuncDecl:
			t.Recv = unnamed(t.Recv)
		case *ast.FuncType:
			t.Params, t.Results = unnamed(t.Params), unnamed(t.Results)
		}

		return true
	})

	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, node); err != nil {
		return err
	}

	variants[key] = append(variants[key], strings.Join(strings.Fields(buf.String()), " ")+suffix)

	return nil
}

func unnamed(list *ast.FieldList) *ast.FieldList {
	if list == nil {
		return nil
	}

	out := &ast.FieldList{}

	for _, f := range list.List {
		for range max(1, len(f.Names)) {
			out.List = append(out.List, &ast.Field{Type: f.Type})
		}
	}

	return out
}

func receiverName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return receiverName(t.X)
	case *ast.IndexExpr:
		return receiverName(t.X)
	case *ast.IndexListExpr:
		return receiverName(t.X)
	case *ast.Ident:
		return t.Name
	}

	return ""
}
