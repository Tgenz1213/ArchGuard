package index

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConfluenceProvider_GetADRs_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify authorization headers and request params
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		if !strings.Contains(r.URL.Path, "/wiki/api/v2/spaces/ARCH/pages") {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		response := ConfluenceSearchResponse{}

		// Page 1: Valid ADR (Accepted)
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

		// Page 2: Rejected ADR
		rejectedPage := validPage
		rejectedPage.ID = "2"
		rejectedPage.Title = "Use Python"
		rejectedPage.Body.Storage.Value = `<p>---
title: Use Python
status: Rejected
---
We will use Python.</p>`
		rejectedPage.Links.WebUI = "/spaces/ARCH/pages/2/Use+Python"

		// Page 3: Not an ADR (No Frontmatter)
		invalidPage := validPage
		invalidPage.ID = "3"
		invalidPage.Title = "Meeting Notes"
		invalidPage.Body.Storage.Value = `<p>Just some notes</p>`
		invalidPage.Links.WebUI = "/spaces/ARCH/pages/3/Meeting+Notes"

		response.Results = append(response.Results, validPage, rejectedPage, invalidPage)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer ts.Close()

	// The provider expects domain. Since we updated the provider to respect prefixes,
	// we can just pass the full URL.
	provider := NewConfluenceProvider(ts.URL, "ARCH", "user", "token", []string{"Accepted"})

	adrs, err := provider.GetADRs(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(adrs) != 1 || adrs[0].ID != "confluence-1" || adrs[0].Title != "Use Go" {
		t.Errorf("unexpected ADR contents: %+v", adrs[0])
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
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer ts.Close()

	provider := NewConfluenceProvider(ts.URL, "ARCH", "user", "token", []string{"Accepted"})
	adrs, err := provider.GetADRs(context.Background())
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
	// Real Confluence storage format renders each line of a page as its own
	// block element, unlike the other fixtures in this file (which embed
	// literal newlines inside a single <p> and would pass even with a naive
	// HTML-to-text conversion that doesn't reconstruct block boundaries).
	html := `<p>---</p><p>title: Use Go</p><p>status: Accepted</p><p>scope: "**/*.go"</p><p>---</p><p>We will use Go for all services.</p>`

	raw := extractRawText(html)

	adr, err := ParseADRContent([]byte(raw), "confluence-test", "test/path")
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
		_, _ = w.Write([]byte("Internal Server Error"))
	}))
	defer ts.Close()

	provider := NewConfluenceProvider(ts.URL, "ARCH", "user", "token", []string{"Accepted"})
	_, err := provider.GetADRs(context.Background())
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "confluence returned 500") {
		t.Errorf("unexpected error message: %v", err)
	}
}
