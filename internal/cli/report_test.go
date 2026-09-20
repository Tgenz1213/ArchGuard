package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis/stage"
)

func TestWriteCheckReport_EmptyListsEncodeAsArraysNotNull(t *testing.T) {
	var buf bytes.Buffer
	if err := writeCheckReport(&buf, nil, nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out := buf.String(); strings.Contains(out, "null") || !strings.Contains(out, `"stages": []`) || !strings.Contains(out, `"violations": []`) {
		t.Fatalf("report = %s, want empty arrays and no null", out)
	}
}

func TestWriteCheckReport_StagesKeepTheirJSONFields(t *testing.T) {
	var buf bytes.Buffer
	stages := []stage.Stats{{Name: "rank", Received: 6, Kept: 4, DurationMS: 812}, {Name: "rerank"}}
	if err := writeCheckReport(&buf, nil, stages, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var report struct {
		Count  int `json:"count"`
		Stages []struct {
			Name       string `json:"name"`
			Received   int    `json:"received"`
			Kept       int    `json:"kept"`
			DurationMS int64  `json:"duration_ms"`
		} `json:"stages"`
	}
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("report is not valid JSON: %v\n%s", err, buf.String())
	}
	if len(report.Stages) != 2 || report.Stages[0].Name != "rank" || report.Stages[0].Received != 6 || report.Stages[0].Kept != 4 || report.Stages[0].DurationMS != 812 || report.Stages[1].Name != "rerank" {
		t.Fatalf("stages = %+v, want rank 6/4/812ms then rerank with zeros", report.Stages)
	}
}
