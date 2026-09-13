package index

import "testing"

func TestParseADRContent_SimilarityThresholdOverride(t *testing.T) {
	data := []byte("---\ntitle: \"Strict ADR\"\nstatus: \"Accepted\"\nsimilarity_threshold: 0.6\n---\nBody")

	adr, err := ParseADRContent(data, "0001", "0001-strict.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if adr.SimilarityThreshold == nil || *adr.SimilarityThreshold != 0.6 {
		t.Fatalf("expected similarity_threshold override of 0.6, got %+v", adr.SimilarityThreshold)
	}
}

func TestParseADRContent_SimilarityThresholdUnsetIsNil(t *testing.T) {
	data := []byte("---\ntitle: \"Default ADR\"\nstatus: \"Accepted\"\n---\nBody")

	adr, err := ParseADRContent(data, "0002", "0002-default.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if adr.SimilarityThreshold != nil {
		t.Fatalf("expected nil similarity_threshold when frontmatter omits it, got %v", *adr.SimilarityThreshold)
	}
}
