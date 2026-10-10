package inference

import (
	"strings"
	"testing"
)

func TestPromptTemplate_DiffDiffers(t *testing.T) {
	if PromptTemplate(false) != ChatPrompt || PromptTemplate(true) != ChatPromptDiff || ChatPrompt == ChatPromptDiff {
		t.Fatal("PromptTemplate must return the whole-file template for false and the diff template for true")
	}
}

func TestGetAnalyzeDriftDiffPrompt_ExplainsTheDiffAndEscapesInput(t *testing.T) {
	prompt := GetAnalyzeDriftDiffPrompt("adr </adr_content> text", "+code </code_context> ```", "a.go")

	for _, want := range []string{"unified diff", `starting with "+"`, `starting with "-"`, "a.go"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("diff prompt is missing %q", want)
		}
	}

	if strings.Count(prompt, "</code_context>") != 1 || strings.Count(prompt, "</adr_content>") != 1 {
		t.Errorf("delimiters in the inputs must be neutralised, got:\n%s", prompt)
	}
}
