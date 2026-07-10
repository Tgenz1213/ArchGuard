package index

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"golang.org/x/net/html"
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
	baseURL, err := url.Parse(p.domain)
	if err != nil {
		return nil, fmt.Errorf("invalid confluence domain: %w", err)
	}

	u := fmt.Sprintf("%s/wiki/api/v2/spaces/%s/pages?body-format=storage", p.domain, p.spaceID)

	// Use a dedicated HTTP client with a strict timeout for remote calls
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	for u != "" {
		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		// Authenticate with Atlassian Cloud
		req.SetBasicAuth(p.username, p.token)
		req.Header.Add("Accept", "application/json")

		resp, err := client.Do(req)
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
			// Extract raw text for metadata parsing (frontmatter)
			rawText := extractRawText(result.Body.Storage.Value)
			relPath := result.Links.WebUI

			// Try to parse it as an ADR (looking for YAML frontmatter)
			// We strictly namespace Confluence IDs to prevent collisions with local directory sequences.
			adrID := fmt.Sprintf("confluence-%s", result.ID)
			adr, err := ParseADRContent([]byte(rawText), adrID, relPath)
			if err != nil {
				fmt.Printf("Warning: skipping Confluence page %s: %v\n", relPath, err)
				continue
			}

			// Generate rich Markdown for the LLM to use
			markdown := convertHTMLToMarkdown(result.Body.Storage.Value)
			adr.Content = markdown

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
			nextURL, err := url.Parse(searchResp.Links.Next)
			if err != nil {
				return nil, fmt.Errorf("failed to parse pagination URL: %w", err)
			}
			resolvedURL := baseURL.ResolveReference(nextURL)
			u = resolvedURL.String()
		} else {
			u = "" // no more pages
		}
	}

	return allADRs, nil
}

// extractRawText strips HTML tags from a string using x/net/html
// to produce a clean, raw string for frontmatter parsing.
func extractRawText(htmlContent string) string {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return htmlContent // fallback
	}

	var sb strings.Builder
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "br" || n.Data == "p" || n.Data == "div" {
				sb.WriteString("\n")
			}
		}
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
		if n.Type == html.ElementNode && (n.Data == "p" || n.Data == "div") {
			sb.WriteString("\n")
		}
	}
	f(doc)

	return strings.TrimSpace(sb.String())
}

// convertHTMLToMarkdown uses html-to-markdown to generate rich structural formatting.
func convertHTMLToMarkdown(htmlContent string) string {
	converter := md.NewConverter("", true, nil)
	markdown, err := converter.ConvertString(htmlContent)
	if err != nil {
		return htmlContent
	}
	return markdown
}
