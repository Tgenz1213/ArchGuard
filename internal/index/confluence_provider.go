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

	"github.com/tgenz1213/archguard/internal/output"
)

type ConfluenceProvider struct {
	domain           string
	spaceID          string
	username         string
	token            string
	acceptedStatuses []string
	parseOpts        ParseOptions
	out              *output.Printer
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

func (p *ConfluenceProvider) SetPrinter(out *output.Printer) {
	p.out = out
}

func (p *ConfluenceProvider) SetFrontmatterMappings(mappings map[string]string) {
	p.parseOpts.FrontmatterMappings = mappings
}

func (p *ConfluenceProvider) SetRulesHeading(heading string) {
	p.parseOpts.RulesHeading = heading
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
			body, _ := io.ReadAll(resp.Body) //nolint:errcheck // the status error wins; the body only adds detail
			_ = resp.Body.Close()            //nolint:errcheck // cleanup; the status error wins
			return nil, FetchStats{}, fmt.Errorf("confluence returned %d: %s", resp.StatusCode, string(body))
		}

		var searchResp ConfluenceSearchResponse
		if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
			_ = resp.Body.Close() //nolint:errcheck // cleanup; the decode error wins
			return nil, FetchStats{}, fmt.Errorf("failed to decode confluence response: %w", err)
		}

		_ = resp.Body.Close() //nolint:errcheck // the body is fully decoded; closing it can't change the result

		for _, result := range searchResp.Results {
			stats.Discovered++
			rawText := extractRawText(result.Body.Storage.Value)

			var relPath string
			if parsedWebUI, err := url.Parse(result.Links.WebUI); err == nil {
				relPath = baseURL.ResolveReference(parsedWebUI).String()
			} else {
				relPath = fmt.Sprintf("%s%s", p.domain, result.Links.WebUI)
			}

			// Namespaced so Confluence IDs can't collide with local ADR IDs.
			adrID := fmt.Sprintf("confluence-%s", result.ID)
			markdown := convertHTMLToMarkdown(result.Body.Storage.Value)

			adr, rulesErr, err := parseADR([]byte(rawText), adrID, relPath, p.parseOpts, &markdown)
			if err != nil {
				p.out.Warn("skipping Confluence page %s: %v", relPath, err)
				stats.ParseFailed = append(stats.ParseFailed, relPath)
				continue
			}

			if isAcceptedStatus(adr.Status, p.acceptedStatuses) {
				allADRs = append(allADRs, *adr)

				if rulesErr != nil {
					p.out.Warn("ignoring rules in Confluence page %s: %v", relPath, rulesErr)
					stats.MalformedRules = append(stats.MalformedRules, MalformedRules{RelPath: relPath, Reason: rulesErr.Error()})
				}
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
