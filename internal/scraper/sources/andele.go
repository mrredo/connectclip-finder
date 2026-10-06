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
	andeleBgURLRegex = regexp.MustCompile(`url\(['"]?([^'"]+)['"]?\)`)
	andelePriceRegex = regexp.MustCompile(`([\d\s]+(?:[.,]\d+)?)\s*€`)
)

// AndeleAdapter scrapes andelemandele.lv.
type AndeleAdapter struct {
	enabled bool
	cookies string
	client  *http.Client
}

// NewAndeleAdapter creates an Andele Mandele adapter.
func NewAndeleAdapter(enabled bool, cookies string) *AndeleAdapter {
	return &AndeleAdapter{
		enabled: enabled,
		cookies: cookies,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (a *AndeleAdapter) Name() model.Source {
	return model.SourceAndele
}

func (a *AndeleAdapter) IsEnabled() bool {
	return a.enabled
}

// Search executes a query on Andele Mandele.
func (a *AndeleAdapter) Search(ctx context.Context, query string) ([]*model.Listing, error) {
	searchURL := fmt.Sprintf("https://www.andelemandele.lv/search/?search=%s", url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept-Language", "lv,en;q=0.9")
	if a.cookies != "" {
		req.Header.Set("Cookie", a.cookies)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("andele.lv HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("andele.lv returned status code %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read andele.lv response body: %w", err)
	}

	return a.parseHTML(string(body))
}

func (a *AndeleAdapter) parseHTML(content string) ([]*model.Listing, error) {
	doc, err := xHtml.Parse(strings.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("failed to parse andele.lv HTML: %w", err)
	}

	var listings []*model.Listing
	var traverse func(*xHtml.Node)
	traverse = func(n *xHtml.Node) {
		if n.Type == xHtml.ElementNode && n.Data == "article" {
			for _, attr := range n.Attr {
				if attr.Key == "data-role" && attr.Val == "product-card" {
					listing := a.parseProductCard(n)
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

func (a *AndeleAdapter) parseProductCard(node *xHtml.Node) *model.Listing {
	var sourceID string
	for _, attr := range node.Attr {
		if attr.Key == "data-id" {
			sourceID = attr.Val
			break
		}
	}

	var itemURL, title, imageURL, priceText, seller string

	var extract func(*xHtml.Node)
	extract = func(n *xHtml.Node) {
		if n.Type == xHtml.ElementNode {
			if n.Data == "a" {
				for _, attr := range n.Attr {
					if attr.Key == "href" && strings.Contains(attr.Val, "/perle/") && itemURL == "" {
						itemURL = attr.Val
						// If title is empty, derive slug from URL as fallback
						parts := strings.Split(strings.Trim(itemURL, "/"), "/")
						if len(parts) >= 3 {
							title = strings.ReplaceAll(parts[2], "-", " ")
						}
					}
				}
			}
			if n.Data == "div" {
				for _, attr := range n.Attr {
					if attr.Key == "style" && strings.Contains(attr.Val, "background-image") {
						matches := andeleBgURLRegex.FindStringSubmatch(attr.Val)
						if len(matches) > 2 {
							imageURL = matches[2]
						}
					}
					if attr.Key == "class" && strings.Contains(attr.Val, "product-card__title") {
						t := getText(n)
						if t != "" {
							title = t
						}
					}
					if attr.Key == "class" && strings.Contains(attr.Val, "product-card__user") {
						seller = getText(n)
					}
				}
			}
			if n.Data == "span" {
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

	// Clean tracking params from itemURL
	if idx := strings.Index(itemURL, "?"); idx != -1 {
		itemURL = itemURL[:idx]
	}

	if !strings.HasPrefix(itemURL, "http") {
		itemURL = "https://www.andelemandele.lv" + itemURL
	}

	if title == "" {
		title = "Andele Mandele Perle #" + sourceID
	}

	price := 0.0
	if priceText != "" {
		matches := andelePriceRegex.FindStringSubmatch(priceText)
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
		Source:      model.SourceAndele,
		SourceID:    sourceID,
		URL:         itemURL,
		Title:       title,
		Description: "Andele Mandele Sludinājums",
		Price:       price,
		Currency:    "EUR",
		ImageURLs:   images,
		Location:    "Latvija",
		Seller:      seller,
	}
}
