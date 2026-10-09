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
	baseURL, err := url.Parse(p.domain)
	if err != nil {
		return nil, FetchStats{}, fmt.Errorf("invalid confluence domain: %w", err)
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	pageURL := fmt.Sprintf("%s/wiki/api/v2/spaces/%s/pages?body-format=storage", p.domain, p.spaceID)

	var allADRs []ADR

	var stats FetchStats

	for pageURL != "" {
		page, err := p.fetchPage(ctx, client, pageURL)
		if err != nil {
			return nil, FetchStats{}, err
		}

		allADRs = append(allADRs, p.pageADRs(baseURL, page, &stats)...)

		pageURL, err = nextPageURL(baseURL, page.Links.Next)
		if err != nil {
			return nil, FetchStats{}, err
		}
	}

	return allADRs, stats, nil
}

func (p *ConfluenceProvider) fetchPage(ctx context.Context, client *http.Client, pageURL string) (ConfluenceSearchResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", pageURL, nil)
	if err != nil {
		return ConfluenceSearchResponse{}, fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(p.username, p.token)
	req.Header.Add("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return ConfluenceSearchResponse{}, fmt.Errorf("confluence request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body) //nolint:errcheck // the status error wins; the body only adds detail
		_ = resp.Body.Close()            //nolint:errcheck // cleanup; the status error wins

		return ConfluenceSearchResponse{}, fmt.Errorf("confluence returned %d: %s", resp.StatusCode, string(body))
	}

	var page ConfluenceSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		_ = resp.Body.Close() //nolint:errcheck // cleanup; the decode error wins

		return ConfluenceSearchResponse{}, fmt.Errorf("failed to decode confluence response: %w", err)
	}

	_ = resp.Body.Close() //nolint:errcheck // the body is fully decoded; closing it can't change the result

	return page, nil
}

func (p *ConfluenceProvider) pageADRs(baseURL *url.URL, page ConfluenceSearchResponse, stats *FetchStats) []ADR {
	var adrs []ADR

	for _, result := range page.Results {
		stats.Discovered++
		rawText := extractRawText(result.Body.Storage.Value)
		relPath := p.pageRelPath(baseURL, result.Links.WebUI)

		// Namespaced so Confluence IDs can't collide with local ADR IDs.
		adrID := fmt.Sprintf("confluence-%s", result.ID)
		markdown := convertHTMLToMarkdown(result.Body.Storage.Value)

		adr, rulesErr, err := parseADR([]byte(rawText), adrID, relPath, p.parseOpts, &markdown)
		if err != nil {
			p.out.Warn("skipping Confluence page %s: %v", relPath, err)
			stats.ParseFailed = append(stats.ParseFailed, relPath)

			continue
		}

		if !isAcceptedStatus(adr.Status, p.acceptedStatuses) {
			stats.StatusRejected++

			continue
		}

		adrs = append(adrs, *adr)

		if rulesErr != nil {
			p.out.Warn("ignoring rules in Confluence page %s: %v", relPath, rulesErr)
			stats.MalformedRules = append(stats.MalformedRules, MalformedRules{RelPath: relPath, Reason: rulesErr.Error()})
		}
	}

	return adrs
}

func (p *ConfluenceProvider) pageRelPath(baseURL *url.URL, webUI string) string {
	parsed, err := url.Parse(webUI)
	if err != nil {
		return fmt.Sprintf("%s%s", p.domain, webUI)
	}

	return baseURL.ResolveReference(parsed).String()
}

func nextPageURL(baseURL *url.URL, next string) (string, error) {
	if next == "" {
		return "", nil
	}

	parsed, err := url.Parse(next)
	if err != nil {
		return "", fmt.Errorf("failed to parse pagination URL: %w", err)
	}

	return baseURL.ResolveReference(parsed).String(), nil
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
