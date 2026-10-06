package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"connectclip-finder/internal/model"
	xHtml "golang.org/x/net/html"
)

// VitaLombardsAdapter scrapes vitalombards.lv online catalog.
type VitaLombardsAdapter struct {
	enabled bool
	client  *http.Client
}

// NewVitaLombardsAdapter initializes a Vita Lombards adapter.
func NewVitaLombardsAdapter(enabled bool) *VitaLombardsAdapter {
	return &VitaLombardsAdapter{
		enabled: enabled,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (v *VitaLombardsAdapter) Name() model.Source {
	return model.SourceVitaLombards
}

func (v *VitaLombardsAdapter) IsEnabled() bool {
	return v.enabled
}

// Search executes a search query on Vita Lombards.
func (v *VitaLombardsAdapter) Search(ctx context.Context, query string) ([]*model.Listing, error) {
	searchURL := fmt.Sprintf("https://vitalombards.lv/lv/?search=%s", url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept-Language", "lv,en;q=0.9")

	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vitalombards HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vitalombards returned status code %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read vitalombards response body: %w", err)
	}

	return v.parseHTML(string(body))
}

type vitaGA4Item struct {
	ItemID       string  `json:"item_id"`
	ItemName     string  `json:"item_name"`
	Price        float64 `json:"price"`
	ItemBrand    string  `json:"item_brand"`
	ItemCategory string  `json:"item_category"`
}

func (v *VitaLombardsAdapter) parseHTML(content string) ([]*model.Listing, error) {
	doc, err := xHtml.Parse(strings.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("failed to parse vitalombards HTML: %w", err)
	}

	var listings []*model.Listing
	var traverse func(*xHtml.Node)
	traverse = func(n *xHtml.Node) {
		if n.Type == xHtml.ElementNode && n.Data == "div" {
			for _, attr := range n.Attr {
				if attr.Key == "class" && strings.Contains(attr.Val, "product") {
					listing := v.parseProductDiv(n)
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

func (v *VitaLombardsAdapter) parseProductDiv(node *xHtml.Node) *model.Listing {
	var ga4Data string
	for _, attr := range node.Attr {
		if attr.Key == "data-ga4-item" {
			ga4Data = html.UnescapeString(attr.Val)
			break
		}
	}

	var item vitaGA4Item
	if ga4Data != "" {
		_ = json.Unmarshal([]byte(ga4Data), &item)
	}

	var itemURL, imageURL, title, location string
	var details []string

	var extract func(*xHtml.Node)
	extract = func(n *xHtml.Node) {
		if n.Type == xHtml.ElementNode {
			if n.Data == "a" {
				for _, attr := range n.Attr {
					if attr.Key == "href" && strings.Contains(attr.Val, "/product/") && itemURL == "" {
						itemURL = attr.Val
					}
				}
			}
			if n.Data == "img" {
				for _, attr := range n.Attr {
					if attr.Key == "src" && imageURL == "" {
						imageURL = attr.Val
					}
				}
			}
			if n.Data == "h3" {
				for _, attr := range n.Attr {
					if attr.Key == "class" && strings.Contains(attr.Val, "product-title") {
						title = getText(n)
					}
				}
			}
			if n.Data == "li" {
				for _, attr := range n.Attr {
					if attr.Key == "class" && strings.Contains(attr.Val, "product-place-row") {
						location = getText(n)
					}
					if attr.Key == "class" && strings.Contains(attr.Val, "product-detail-row") {
						details = append(details, getText(n))
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}
	extract(node)

	if title == "" {
		title = item.ItemName
	}
	if title == "" || itemURL == "" {
		return nil
	}

	if !strings.HasPrefix(itemURL, "http") {
		itemURL = "https://vitalombards.lv" + itemURL
	}

	var images []string
	if imageURL != "" {
		images = append(images, imageURL)
	}

	sourceID := item.ItemID
	if sourceID == "" {
		sourceID = itemURL
	}

	description := strings.Join(details, " | ")
	if item.ItemCategory != "" {
		description = item.ItemCategory + " - " + description
	}

	return &model.Listing{
		Source:      model.SourceVitaLombards,
		SourceID:    sourceID,
		URL:         itemURL,
		Title:       title,
		Description: description,
		Price:       item.Price,
		Currency:    "EUR",
		ImageURLs:   images,
		Location:    location,
		Seller:      "Vita Lombards",
	}
}
