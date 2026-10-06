package sources

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"connectclip-finder/internal/model"
	"golang.org/x/net/html"
)

var (
	ssPriceRegex = regexp.MustCompile(`([\d\s]+(?:[.,]\d+)?)\s*€`)
	ssIDRegex    = regexp.MustCompile(`^tr_(\d+)`)
)

// SSComAdapter implements scraping for SS.com (ss.lv).
type SSComAdapter struct {
	enabled bool
	client  *http.Client
}

// NewSSComAdapter creates a new SS.com scraper adapter.
func NewSSComAdapter(enabled bool) *SSComAdapter {
	return &SSComAdapter{
		enabled: enabled,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (s *SSComAdapter) Name() model.Source {
	return model.SourceSSCom
}

func (s *SSComAdapter) IsEnabled() bool {
	return s.enabled
}

// Search executes a search query on SS.com.
func (s *SSComAdapter) Search(ctx context.Context, query string) ([]*model.Listing, error) {
	searchURL := fmt.Sprintf("https://www.ss.com/lv/search-result/?q=%s", url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept-Language", "lv,en;q=0.9")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ss.com HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ss.com returned status code %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read ss.com response body: %w", err)
	}

	return s.parseHTML(string(body))
}

func (s *SSComAdapter) parseHTML(content string) ([]*model.Listing, error) {
	doc, err := html.Parse(strings.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("failed to parse ss.com HTML: %w", err)
	}

	var listings []*model.Listing
	var traverse func(*html.Node)
	traverse = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "tr" {
			for _, attr := range n.Attr {
				if attr.Key == "id" && strings.HasPrefix(attr.Val, "tr_") {
					listing := s.parseRow(n, attr.Val)
					if listing != nil {
						listings = append(listings, listing)
					}
					return
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			traverse(c)
		}
	}
	traverse(doc)
	return listings, nil
}

func (s *SSComAdapter) parseRow(row *html.Node, trID string) *model.Listing {
	sourceID := strings.TrimPrefix(trID, "tr_")

	var itemURL, title, imageURL, category, priceText string

	var extractDetails func(*html.Node)
	extractDetails = func(n *html.Node) {
		if n.Type == html.ElementNode {
			// Find link and title in <a class="am" ...>
			if n.Data == "a" {
				for _, attr := range n.Attr {
					if attr.Key == "class" && (attr.Val == "am" || strings.Contains(attr.Val, "am ")) {
						for _, a := range n.Attr {
							if a.Key == "href" && itemURL == "" {
								itemURL = a.Val
							}
						}
						title = getText(n)
					}
				}
			}
			// Find image
			if n.Data == "img" {
				for _, attr := range n.Attr {
					if attr.Key == "src" && strings.Contains(attr.Val, "gallery") {
						imageURL = attr.Val
					}
				}
			}
			// Find category
			if n.Data == "div" {
				for _, attr := range n.Attr {
					if attr.Key == "class" && attr.Val == "ads_cat_names" {
						category = getText(n)
					}
				}
			}
			// Find price
			if n.Data == "td" {
				for _, attr := range n.Attr {
					if attr.Key == "class" && strings.Contains(attr.Val, "msga2-o") {
						priceText = getText(n)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extractDetails(c)
		}
	}
	extractDetails(row)

	title = strings.TrimSpace(title)
	if title == "" || itemURL == "" {
		return nil
	}

	if !strings.HasPrefix(itemURL, "http") {
		itemURL = "https://www.ss.com" + itemURL
	}

	price := parsePrice(priceText)

	var images []string
	if imageURL != "" {
		images = append(images, imageURL)
	}

	return &model.Listing{
		Source:      model.SourceSSCom,
		SourceID:    sourceID,
		URL:         itemURL,
		Title:       title,
		Description: category,
		Price:       price,
		Currency:    "EUR",
		ImageURLs:   images,
		Location:    "Latvija",
	}
}

func getText(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		if cur.Type == html.TextNode {
			sb.WriteString(cur.Data)
		}
		for c := cur.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(sb.String())
}

func parsePrice(text string) float64 {
	matches := ssPriceRegex.FindStringSubmatch(text)
	if len(matches) > 1 {
		raw := strings.ReplaceAll(matches[1], " ", "")
		raw = strings.ReplaceAll(raw, ",", ".")
		if val, err := strconv.ParseFloat(raw, 64); err == nil {
			return val
		}
	}
	return 0.0
}
