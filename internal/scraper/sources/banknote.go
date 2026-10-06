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
	xHtml "golang.org/x/net/html"
)

var (
	banknotePriceRegex = regexp.MustCompile(`([\d\s]+(?:[.,]\d+)?)\s*€`)
)

// BanknoteAdapter scrapes veikals.banknote.lv.
type BanknoteAdapter struct {
	enabled bool
	cookies string
	client  *http.Client
}

// NewBanknoteAdapter creates a Banknote adapter.
func NewBanknoteAdapter(enabled bool, cookies string) *BanknoteAdapter {
	return &BanknoteAdapter{
		enabled: enabled,
		cookies: cookies,
		client: &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 4 {
					return http.ErrUseLastResponse
				}
				return nil
			},
		},
	}
}

func (b *BanknoteAdapter) Name() model.Source {
	return model.SourceBanknote
}

func (b *BanknoteAdapter) IsEnabled() bool {
	return b.enabled
}

// Search executes a query on Banknote store.
func (b *BanknoteAdapter) Search(ctx context.Context, query string) ([]*model.Listing, error) {
	searchURL := fmt.Sprintf("https://veikals.banknote.lv/lv/filter?q=%s", url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept-Language", "lv,en;q=0.9")
	if b.cookies != "" {
		req.Header.Set("Cookie", b.cookies)
	} else {
		req.Header.Set("Cookie", "selected_country=eyJpdiI6Inh4IiwidmFsdWUiOiJ4eCJ9; typicms_locale=lv")
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("banknote HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusServiceUnavailable {
		return nil, fmt.Errorf("banknote Cloudflare protection active (status %d); supply BANKNOTE_COOKIES in .env if needed", resp.StatusCode)
	}

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		// Handled redirect
		loc := resp.Header.Get("Location")
		if loc != "" && !strings.Contains(loc, "lv/lv/lv") {
			// Follow single redirect
			req2, _ := http.NewRequestWithContext(ctx, "GET", loc, nil)
			req2.Header = req.Header
			resp2, err := b.client.Do(req2)
			if err == nil {
				defer resp2.Body.Close()
				resp = resp2
			}
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read banknote response body: %w", err)
	}

	return b.parseHTML(string(body))
}

func (b *BanknoteAdapter) parseHTML(content string) ([]*model.Listing, error) {
	doc, err := xHtml.Parse(strings.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("failed to parse banknote HTML: %w", err)
	}

	var listings []*model.Listing
	var traverse func(*xHtml.Node)
	traverse = func(n *xHtml.Node) {
		if n.Type == xHtml.ElementNode && (n.Data == "div" || n.Data == "article") {
			for _, attr := range n.Attr {
				if attr.Key == "class" && (strings.Contains(attr.Val, "product-card") || strings.Contains(attr.Val, "catalog-item")) {
					listing := b.parseItem(n)
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

func (b *BanknoteAdapter) parseItem(node *xHtml.Node) *model.Listing {
	var itemURL, title, imageURL, priceText, sourceID string

	var extract func(*xHtml.Node)
	extract = func(n *xHtml.Node) {
		if n.Type == xHtml.ElementNode {
			if n.Data == "a" {
				for _, attr := range n.Attr {
					if attr.Key == "href" && itemURL == "" {
						itemURL = attr.Val
					}
				}
			}
			if n.Data == "img" {
				for _, attr := range n.Attr {
					if attr.Key == "src" && imageURL == "" {
						imageURL = attr.Val
					}
					if attr.Key == "alt" && title == "" {
						title = attr.Val
					}
				}
			}
			if n.Data == "h3" || n.Data == "h2" || n.Data == "p" {
				for _, attr := range n.Attr {
					if attr.Key == "class" && strings.Contains(attr.Val, "title") {
						title = getText(n)
					}
				}
			}
			if n.Data == "span" || n.Data == "div" {
				for _, attr := range n.Attr {
					if attr.Key == "class" && strings.Contains(attr.Val, "price") {
						priceText = getText(n)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}
	extract(node)

	if itemURL == "" {
		return nil
	}

	if !strings.HasPrefix(itemURL, "http") {
		itemURL = "https://veikals.banknote.lv" + itemURL
	}

	// Extract product ID from URL if possible
	parts := strings.Split(strings.Trim(itemURL, "/"), "/")
	if len(parts) > 0 {
		sourceID = parts[len(parts)-1]
	}

	price := 0.0
	if priceText != "" {
		matches := banknotePriceRegex.FindStringSubmatch(priceText)
		if len(matches) > 1 {
			raw := strings.ReplaceAll(matches[1], " ", "")
			raw = strings.ReplaceAll(raw, ",", ".")
			price, _ = strconv.ParseFloat(raw, 64)
		}
	}

	var images []string
	if imageURL != "" {
		images = append(images, imageURL)
	}

	return &model.Listing{
		Source:      model.SourceBanknote,
		SourceID:    sourceID,
		URL:         itemURL,
		Title:       title,
		Description: "Banknote Veikals Prece",
		Price:       price,
		Currency:    "EUR",
		ImageURLs:   images,
		Location:    "Latvija",
		Seller:      "Banknote",
	}
}
