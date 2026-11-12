package wideapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func surfaceOf(t *testing.T, src string) map[string]string {
	t.Helper()

	surface, err := Surface(map[string][]byte{"p.go": []byte("package p\n" + src)})
	require.NoError(t, err)

	return surface
}

func TestSurfaceChanges(t *testing.T) {
	tests := []struct {
		name          string
		before, after string
		want          []string
	}{
		{"body edit", "func F() int { return 1 }", "func F() int { return 2 }", nil},
		{"unexported func added", "", "func f() {}", nil},
		{"exported func added", "", "func F() {}", []string{"+func F"}},
		{"exported func removed", "func F() {}", "", []string{"-func F"}},
		{"parameter renamed", "func F(a int) {}", "func F(b int) {}", nil},
		{"receiver renamed", "type T struct{}\nfunc (t T) M() {}", "type T struct{}\nfunc (x T) M() {}", nil},
		{"parameter retyped on a second line", "func F(a int,\n\tb string) {}", "func F(a int,\n\tb int) {}", []string{"~func F"}},
		{"grouped parameters split", "func F(a, b int) {}", "func F(a int) {}", []string{"~func F"}},
		{"result added", "func F() {}", "func F() error { return nil }", []string{"~func F"}},
		{"method on unexported type", "type t struct{}", "type t struct{}\nfunc (t) M() {}", nil},
		{"method added to exported type", "type T struct{}", "type T struct{}\nfunc (T) M() {}", []string{"+func T.M"}},
		{"pointer receiver kept apart", "type T struct{}\nfunc (*T) M() {}", "type T struct{}\nfunc (T) M() {}", []string{"~func T.M"}},
		{"unexported field added to a struct with none", "type S struct{ A int }", "type S struct {\n\ta int\n\tA int\n}", nil},
		{"unexported field realigns the rest", "type S struct {\n\tA int\n\tBB string\n}", "type S struct {\n\tlongUnexportedName int\n\tA int\n\tBB string\n}", nil},
		{"exported field added", "type S struct{ A int }", "type S struct{ A, B int }", []string{"~type S"}},
		{"field of an unexported struct", "", "type s struct{ A int }", nil},
		{"embedded unexported type swapped", "type e1 struct{}\ntype e2 struct{}\ntype S struct{ e1 }", "type e1 struct{}\ntype e2 struct{}\ntype S struct{ e2 }", []string{"~type S"}},
		{"embedded pointer retargeted", "type S struct{ *A }\ntype A struct{}", "type S struct{ *B }\ntype A struct{}\ntype B struct{}", []string{"+type B", "~type S"}},
		{"embedded interface changed", "type I interface{ error }", "type I interface{ fmt.Stringer }", []string{"~type I"}},
		{"exported interface method added", "type I interface{ M() }", "type I interface {\n\tM()\n\tN()\n}", []string{"~type I"}},
		{"interface sealed", "type I interface{ M() }", "type I interface {\n\tM()\n\tm()\n}", []string{"~type I"}},
		{"unexported interface method swapped", "type I interface{ m() }", "type I interface{ n() }", nil},
		{"iota value shifted", "const (\n\tA = iota\n\tb\n\tC\n)", "const (\n\tA = iota\n\tb\n\tx\n\tC\n)", []string{"~const C"}},
		{"explicit value unaffected by a sibling", "const (\n\ta = 1\n\tB = 2\n)", "const (\n\ta = 1\n\tx = 3\n\tB = 2\n)", nil},
		{"implicit constant repeats its value", "const (\n\tA = 1\n\tB\n)", "const (\n\tA = 2\n\tB\n)", []string{"~const A", "~const B"}},
		{"var type changed", "var V int", "var V string", []string{"~var V"}},
		{"unexported var added", "", "var v int", nil},
		{"type alias retargeted", "type T = int", "type T = string", []string{"~type T"}},
		{"generic constraint changed", "func F[T any](T) {}", "func F[T comparable](T) {}", []string{"~func F"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, diffSurfaces(surfaceOf(t, tt.before), surfaceOf(t, tt.after)))
		})
	}
}

func TestSurfaceKeepsEveryVariantOfASymbolDeclaredInSeveralFiles(t *testing.T) {
	file := func(sig string) map[string][]byte {
		return map[string][]byte{
			"a.go": []byte("package p\nfunc F(int) {}"),
			"b.go": []byte("package p\nfunc F(" + sig + ") {}"),
		}
	}

	before, err := Surface(file("string"))
	require.NoError(t, err)

	after, err := Surface(file("bool"))
	require.NoError(t, err)

	assert.Equal(t, []string{"~func F"}, diffSurfaces(before, after))
}

func TestSurfaceRejectsASourceThatDoesNotParse(t *testing.T) {
	_, err := Surface(map[string][]byte{"p.go": []byte("package p\nfunc (")})

	require.Error(t, err)
}
