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
	"github.com/PuerkitoBio/goquery"
)

type ConfluenceProvider struct {
	domain              string
	spaceID             string
	username            string
	token               string
	acceptedStatuses    []string
	frontmatterMappings map[string]string
	writer              io.Writer
}

func NewConfluenceProvider(domain, spaceID, username, token string, acceptedStatuses []string) *ConfluenceProvider {
	return &ConfluenceProvider{
		domain:           domain,
		spaceID:          spaceID,
		username:         username,
		token:            token,
		acceptedStatuses: acceptedStatuses,
	}
}

// SetWriter routes GetADRs' parse-failure warnings to w instead of the
// default os.Stdout. Passing nil restores the default.
func (p *ConfluenceProvider) SetWriter(w io.Writer) {
	p.writer = w
}

func (p *ConfluenceProvider) SetFrontmatterMappings(mappings map[string]string) {
	p.frontmatterMappings = mappings
}

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

func (p *ConfluenceProvider) GetADRs(ctx context.Context) ([]ADR, FetchStats, error) {
	var allADRs []ADR
	var stats FetchStats

	baseURL, err := url.Parse(p.domain)
	if err != nil {
		return nil, FetchStats{}, fmt.Errorf("invalid confluence domain: %w", err)
	}

	u := fmt.Sprintf("%s/wiki/api/v2/spaces/%s/pages?body-format=storage", p.domain, p.spaceID)

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	for u != "" {
		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			return nil, FetchStats{}, fmt.Errorf("failed to create request: %w", err)
		}

		req.SetBasicAuth(p.username, p.token)
		req.Header.Add("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return nil, FetchStats{}, fmt.Errorf("confluence request failed: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			return nil, FetchStats{}, fmt.Errorf("confluence returned %d: %s", resp.StatusCode, string(body))
		}

		var searchResp ConfluenceSearchResponse
		if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
			_ = resp.Body.Close()
			return nil, FetchStats{}, fmt.Errorf("failed to decode confluence response: %w", err)
		}
		_ = resp.Body.Close()

		for _, result := range searchResp.Results {
			stats.Discovered++
			rawText := extractRawText(result.Body.Storage.Value)
			var relPath string
			if parsedWebUI, err := url.Parse(result.Links.WebUI); err == nil {
				relPath = baseURL.ResolveReference(parsedWebUI).String()
			} else {
				relPath = fmt.Sprintf("%s%s", p.domain, result.Links.WebUI)
			}

			// We strictly namespace Confluence IDs to prevent collisions with local directory sequences.
			adrID := fmt.Sprintf("confluence-%s", result.ID)
			adr, err := ParseADRContent([]byte(rawText), adrID, relPath, p.frontmatterMappings)
			if err != nil {
				diagPrintf(p.writer, "Warning: skipping Confluence page %s: %v\n", relPath, err)
				stats.ParseFailed = append(stats.ParseFailed, relPath)
				continue
			}

			markdown := convertHTMLToMarkdown(result.Body.Storage.Value)
			adr.Content = markdown

			if isAcceptedStatus(adr.Status, p.acceptedStatuses) {
				allADRs = append(allADRs, *adr)
			} else {
				stats.StatusRejected++
			}
		}

		if searchResp.Links.Next != "" {
			nextURL, err := url.Parse(searchResp.Links.Next)
			if err != nil {
				return nil, FetchStats{}, fmt.Errorf("failed to parse pagination URL: %w", err)
			}
			resolvedURL := baseURL.ResolveReference(nextURL)
			u = resolvedURL.String()
		} else {
			u = ""
		}
	}

	return allADRs, stats, nil
}

// Newline after br/p/div so adjacent lines are not concatenated.
func extractRawText(htmlContent string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return htmlContent
	}

	doc.Find("br, p, div").AfterHtml("\n")

	return strings.TrimSpace(doc.Text())
}

func convertHTMLToMarkdown(htmlContent string) string {
	converter := md.NewConverter("", true, nil)
	markdown, err := converter.ConvertString(htmlContent)
	if err != nil {
		return htmlContent
	}
	return markdown
}
