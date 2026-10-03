package output_test

import (
	"bytes"
	"testing"

	"github.com/tgenz1213/archguard/internal/output"
)

func TestAnnotation(t *testing.T) {
	tests := []struct {
		name string
		a    output.Annotation
		want string
	}{
		{
			name: "line-level",
			a:    output.Annotation{File: "src/app.go", Line: 12, ADRID: "0001", ADRTitle: "Use Go", Message: "Python is not allowed."},
			want: "::error file=src/app.go,line=12,title=0001%3A Use Go::Python is not allowed.\n",
		},
		{
			name: "file-level without a line",
			a:    output.Annotation{File: "src/app.go", ADRID: "0001", ADRTitle: "Use Go", Message: "m"},
			want: "::error file=src/app.go,title=0001%3A Use Go::m\n",
		},
		{
			name: "message escapes percent and line breaks but keeps colons and commas",
			a:    output.Annotation{File: "a.go", ADRID: "1", ADRTitle: "t", Message: "100% wrong,\r\nsee: ::error::x\n"},
			want: "::error file=a.go,title=1%3A t::100%25 wrong,%0D%0Asee: ::error::x%0A\n",
		},
		{
			name: "file and title escape percent, line breaks, colons and commas",
			a:    output.Annotation{File: "dir,x/50%:a\nb.go", ADRID: "0001", ADRTitle: "Use Go, not\rPython: 100%", Message: "m"},
			want: "::error file=dir%2Cx/50%25%3Aa%0Ab.go,title=0001%3A Use Go%2C not%0DPython%3A 100%25::m\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			output.New(&buf, false).Annotation(tt.a)

			if got := buf.String(); got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}
