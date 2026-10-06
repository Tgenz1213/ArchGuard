package index

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/output"
)

func TestConfluenceProvider_GetADRs_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		if !strings.Contains(r.URL.Path, "/wiki/api/v2/spaces/ARCH/pages") {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		response := ConfluenceSearchResponse{}

		validPage := struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Body  struct {
				Storage struct {
					Value string `json:"value"`
				} `json:"storage"`
			} `json:"body"`
			Links struct {
				WebUI string `json:"webui"`
			} `json:"_links"`
		}{}
		validPage.ID = "1"
		validPage.Title = "Use Go"
		validPage.Body.Storage.Value = `<p>---
title: Use Go
status: Accepted
---
We will use Go.</p>`
		validPage.Links.WebUI = "/spaces/ARCH/pages/1/Use+Go"

		rejectedPage := validPage
		rejectedPage.ID = "2"
		rejectedPage.Title = "Use Python"
		rejectedPage.Body.Storage.Value = `<p>---
title: Use Python
status: Rejected
---
We will use Python.</p>`
		rejectedPage.Links.WebUI = "/spaces/ARCH/pages/2/Use+Python"

		invalidPage := validPage
		invalidPage.ID = "3"
		invalidPage.Title = "Meeting Notes"
		invalidPage.Body.Storage.Value = `<p>Just some notes</p>`
		invalidPage.Links.WebUI = "/spaces/ARCH/pages/3/Meeting+Notes"

		response.Results = append(response.Results, validPage, rejectedPage, invalidPage)

		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))
	defer ts.Close()

	provider := NewConfluenceProvider(ts.URL, "ARCH", "user", "token", []string{"Accepted"})

	adrs, stats, err := provider.GetADRs(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(adrs) != 1 || adrs[0].ID != "confluence-1" || adrs[0].Title != "Use Go" {
		t.Errorf("unexpected ADR contents: %+v", adrs[0])
	}

	if stats.Discovered != 3 {
		t.Errorf("expected 3 discovered pages, got %d", stats.Discovered)
	}

	if stats.StatusRejected != 1 {
		t.Errorf("expected 1 status-rejected page, got %d", stats.StatusRejected)
	}

	if len(stats.ParseFailed) != 1 {
		t.Errorf("expected 1 parse-failed page, got %v", stats.ParseFailed)
	}
}

func TestConfluenceProvider_GetADRs_RespectsFrontmatterMappings(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := ConfluenceSearchResponse{}

		page := struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Body  struct {
				Storage struct {
					Value string `json:"value"`
				} `json:"storage"`
			} `json:"body"`
			Links struct {
				WebUI string `json:"webui"`
			} `json:"_links"`
		}{}
		page.ID = "1"
		page.Title = "Use Go"
		page.Body.Storage.Value = `<p>---
title: Use Go
status: Accepted
applies_to: "**/*.go"
---
We will use Go.</p>`
		page.Links.WebUI = "/spaces/ARCH/pages/1/Use+Go"

		response.Results = append(response.Results, page)

		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))
	defer ts.Close()

	provider := NewConfluenceProvider(ts.URL, "ARCH", "user", "token", []string{"Accepted"})
	provider.SetFrontmatterMappings(map[string]string{"scope": "applies_to"})

	adrs, _, err := provider.GetADRs(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(adrs) != 1 {
		t.Fatalf("expected 1 ADR, got %d", len(adrs))
	}

	if len(adrs[0].Scope) != 1 || adrs[0].Scope[0] != "**/*.go" {
		t.Errorf("expected scope [\"**/*.go\"] read via mapped applies_to key, got %+v", adrs[0].Scope)
	}
}

func confluenceADRsFromStorage(t *testing.T, storage string, configure func(*ConfluenceProvider)) ([]ADR, FetchStats) {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := map[string]any{
			"id":     "1",
			"title":  "Use Go",
			"body":   map[string]any{"storage": map[string]any{"value": storage}},
			"_links": map[string]any{"webui": "/spaces/ARCH/pages/1/Use+Go"},
		}
		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(map[string]any{"results": []any{page}}); err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))
	t.Cleanup(ts.Close)

	provider := NewConfluenceProvider(ts.URL, "ARCH", "user", "token", []string{"Accepted"})
	if configure != nil {
		configure(provider)
	}

	adrs, stats, err := provider.GetADRs(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return adrs, stats
}

const confluenceRulesPage = `<p>---
title: Use Go
status: Accepted
---</p><h2>Context</h2><p>We like Go.</p>` +
	`<h2>%s</h2><p>Rules follow.</p><ul><li>All code MUST be written in Go.<ul><li>a nested detail</li></ul></li><li>Use <code>errors.Is</code>, never <code>==</code></li></ul>` +
	`<h2>Consequences</h2><ul><li>Not a rule</li></ul>`

func TestConfluenceProvider_GetADRs_BodySectionRules(t *testing.T) {
	adrs, stats := confluenceADRsFromStorage(t, fmt.Sprintf(confluenceRulesPage, "Rules"), nil)
	if len(adrs) != 1 {
		t.Fatalf("expected 1 ADR, got %d", len(adrs))
	}

	want := statements("All code MUST be written in Go.", "Use `errors.Is`, never `==`")
	if !reflect.DeepEqual(adrs[0].Rules, want) {
		t.Errorf("Rules = %+v, want %+v", adrs[0].Rules, want)
	}

	if len(stats.MalformedRules) != 0 {
		t.Errorf("unexpected malformed-rules entries: %+v", stats.MalformedRules)
	}

	if !strings.Contains(adrs[0].Content, "## Rules") {
		t.Errorf("Content should stay the full converted markdown, got %q", adrs[0].Content)
	}
}

func TestConfluenceProvider_GetADRs_CustomRulesHeading(t *testing.T) {
	adrs, _ := confluenceADRsFromStorage(t, fmt.Sprintf(confluenceRulesPage, "Detection"), func(p *ConfluenceProvider) {
		p.SetRulesHeading("Detection")
	})
	if len(adrs) != 1 || len(adrs[0].Rules) != 2 {
		t.Fatalf("expected 2 rules under the custom heading, got %+v", adrs)
	}
}

func TestConfluenceProvider_GetADRs_RulesSectionWithoutBulletsHasNoRules(t *testing.T) {
	page := `<p>---
title: Use Go
status: Accepted
---</p><h2>Rules</h2><p>Only prose here.</p>`

	adrs, stats := confluenceADRsFromStorage(t, page, nil)
	if len(adrs) != 1 || adrs[0].Rules != nil || len(stats.MalformedRules) != 0 {
		t.Fatalf("expected the ADR to load with no rules and no report, got %+v %+v", adrs, stats)
	}
}

func TestConfluenceProvider_GetADRs_WithoutRulesSectionHasNoRules(t *testing.T) {
	adrs, stats := confluenceADRsFromStorage(t, fmt.Sprintf(confluenceRulesPage, "Notes"), nil)
	if len(adrs) != 1 || adrs[0].Rules != nil || len(stats.MalformedRules) != 0 {
		t.Fatalf("expected no rules and no report, got %+v %+v", adrs, stats)
	}
}

func TestConfluenceProvider_GetADRs_MalformedRulesKeepTheADRAndAreReported(t *testing.T) {
	page := `<p>---
title: Use Go
status: Accepted
rules: nope
---</p><p>Body</p>`
	var warnings bytes.Buffer

	adrs, stats := confluenceADRsFromStorage(t, page, func(p *ConfluenceProvider) { p.SetPrinter(output.New(&warnings, false)) })
	if len(adrs) != 1 || adrs[0].Rules != nil {
		t.Fatalf("expected the ADR to load without rules, got %+v", adrs)
	}

	if len(stats.MalformedRules) != 1 || !strings.Contains(stats.MalformedRules[0].Reason, "rules must be a list") {
		t.Fatalf("MalformedRules = %+v, want one entry with the reason", stats.MalformedRules)
	}

	if !strings.HasSuffix(stats.MalformedRules[0].RelPath, "/spaces/ARCH/pages/1/Use+Go") {
		t.Errorf("RelPath = %q, want the page link", stats.MalformedRules[0].RelPath)
	}

	if !strings.Contains(warnings.String(), "ignoring rules") {
		t.Errorf("expected a warning on the configured writer, got %q", warnings.String())
	}
}

func TestConfluenceProvider_GetADRs_Pagination(t *testing.T) {
	requests := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		response := ConfluenceSearchResponse{}

		validPage := struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Body  struct {
				Storage struct {
					Value string `json:"value"`
				} `json:"storage"`
			} `json:"body"`
			Links struct {
				WebUI string `json:"webui"`
			} `json:"_links"`
		}{}

		if requests == 1 {
			validPage.ID = "1"
			validPage.Title = "Page 1"
			validPage.Body.Storage.Value = `<p>---
title: Page 1
status: Accepted
---
Content 1</p>`
			response.Results = append(response.Results, validPage)
			response.Links.Next = "/wiki/api/v2/spaces/ARCH/pages?cursor=nextpage"
		} else {
			validPage.ID = "2"
			validPage.Title = "Page 2"
			validPage.Body.Storage.Value = `<p>---
title: Page 2
status: Accepted
---
Content 2</p>`
			response.Results = append(response.Results, validPage)
		}

		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))
	defer ts.Close()

	provider := NewConfluenceProvider(ts.URL, "ARCH", "user", "token", []string{"Accepted"})

	adrs, _, err := provider.GetADRs(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(adrs) != 2 {
		t.Fatalf("expected 2 ADRs across pagination, got %d", len(adrs))
	}

	if requests != 2 {
		t.Fatalf("expected 2 HTTP requests, got %d", requests)
	}
}

func TestExtractRawText_RealisticMultiParagraphFrontmatter(t *testing.T) {
	// Real storage format: one <p> per line, unlike this file's other fixtures.
	html := `<p>---</p><p>title: Use Go</p><p>status: Accepted</p><p>scope: "**/*.go"</p><p>---</p><p>We will use Go for all services.</p>`

	raw := extractRawText(html)

	adr, err := ParseADRContent([]byte(raw), "confluence-test", "test/path", ParseOptions{})
	if err != nil {
		t.Fatalf("ParseADRContent failed on extracted text (got: %q): %v", raw, err)
	}

	if adr.Title != "Use Go" {
		t.Errorf("expected title 'Use Go', got %q", adr.Title)
	}

	if adr.Status != "Accepted" {
		t.Errorf("expected status 'Accepted', got %q", adr.Status)
	}

	if !strings.Contains(adr.Content, "We will use Go for all services.") {
		t.Errorf("expected content to contain body text, got %q", adr.Content)
	}
}

func TestConfluenceProvider_GetADRs_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)

		if _, err := w.Write([]byte("Internal Server Error")); err != nil {
			t.Errorf("writing response: %v", err)
		}
	}))
	defer ts.Close()

	provider := NewConfluenceProvider(ts.URL, "ARCH", "user", "token", []string{"Accepted"})

	_, _, err := provider.GetADRs(t.Context())
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "confluence returned 500") {
		t.Errorf("unexpected error message: %v", err)
	}
}
