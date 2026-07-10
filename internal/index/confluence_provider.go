package index

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	md "github.com/JohannesKaufmann/html-to-markdown"
)

// ConfluenceProvider fetches ADRs from an Atlassian Confluence Space.
type ConfluenceProvider struct {
	domain           string
	spaceID          string
	username         string
	token            string
	acceptedStatuses []string
}

// NewConfluenceProvider creates a new ConfluenceProvider.
func NewConfluenceProvider(domain, spaceID, username, token string, acceptedStatuses []string) *ConfluenceProvider {
	return &ConfluenceProvider{
		domain:           domain,
		spaceID:          spaceID,
		username:         username,
		token:            token,
		acceptedStatuses: acceptedStatuses,
	}
}

// ConfluenceSearchResponse represents the REST API response from Confluence.
type ConfluenceSearchResponse struct {
	Results []struct {
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
	} `json:"results"`
	Links struct {
		Next string `json:"next"`
	} `json:"_links"`
}

// GetADRs fetches and parses all matching ADRs from Confluence using the CQL query.
func (p *ConfluenceProvider) GetADRs(ctx context.Context) ([]ADR, error) {
	var allADRs []ADR

	// Use Confluence v2 API to get pages in a space
	baseURL := p.domain
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "https://" + baseURL
	}
	u := fmt.Sprintf("%s/wiki/api/v2/spaces/%s/pages?body-format=storage", baseURL, p.spaceID)

	for u != "" {
		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		req.SetBasicAuth(p.username, p.token)
		req.Header.Set("Accept", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("confluence request failed: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			return nil, fmt.Errorf("confluence returned %d: %s", resp.StatusCode, string(body))
		}

		var searchResp ConfluenceSearchResponse
		if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("failed to decode confluence response: %w", err)
		}
		_ = resp.Body.Close()

		for _, result := range searchResp.Results {
			// Extract ID and convert HTML to text
			relPath := result.Links.WebUI
			rawText := strings.TrimSpace(extractTextFromHTML(result.Body.Storage.Value))

			// Try to parse it as an ADR (looking for YAML frontmatter)
			adr, err := ParseADRContent([]byte(rawText), result.ID, relPath)
			if err != nil {
				fmt.Printf("Warning: skipping Confluence page %s: %v\n", relPath, err)
				continue
			}

			// Filter by status
			accept := false
			for _, status := range p.acceptedStatuses {
				if status == "*" || strings.EqualFold(strings.TrimSpace(adr.Status), strings.TrimSpace(status)) {
					accept = true
					break
				}
			}
			if accept {
				allADRs = append(allADRs, *adr)
			}
		}

		if searchResp.Links.Next != "" {
			// _links.next usually contains the relative path like "/wiki/rest/api/content/search?..."
			// Need to ensure it's absolute
			nextPath := searchResp.Links.Next
			if strings.HasPrefix(nextPath, "/wiki") {
				u = fmt.Sprintf("%s%s", baseURL, nextPath)
			} else {
				u = searchResp.Links.Next // Fallback if absolute
			}
		} else {
			u = "" // Break pagination loop
		}
	}

	return allADRs, nil
}

// extractTextFromHTML strips HTML tags from a string using x/net/html.
func extractTextFromHTML(htmlContent string) string {
	converter := md.NewConverter("", true, nil)
	markdown, err := converter.ConvertString(htmlContent)
	if err != nil {
		// Fallback to returning original content if conversion fails
		return htmlContent
	}

	// html-to-markdown escapes --- as \-\-\- if it's not inside a code block.
	// This breaks our frontmatter parser. We must unescape it.
	markdown = strings.ReplaceAll(markdown, "\\-\\-\\-", "---")

	return markdown
}
