package output

import (
	"os"
	"strconv"

	"github.com/sethvargo/go-githubactions"
)

// Annotation is a GitHub Actions error annotation; a Line of 0 annotates the whole file.
type Annotation struct {
	File     string
	Line     int
	ADRID    string
	ADRTitle string
	Message  string
}

func InGitHubActions() bool {
	return os.Getenv("GITHUB_ACTIONS") == "true"
}

// Built from githubactions.Command rather than Action.Errorf, which panics when the write fails.
func (p *Printer) Annotation(a Annotation) {
	cmd := githubactions.Command{
		Name:       "error",
		Message:    a.Message,
		Properties: githubactions.CommandProperties{"file": a.File, "title": a.ADRID + ": " + a.ADRTitle},
	}
	if a.Line > 0 {
		cmd.Properties["line"] = strconv.Itoa(a.Line)
	}

	p.write(cmd.String()+"\n", false, false)
}
