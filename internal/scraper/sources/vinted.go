package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"connectclip-finder/internal/model"
)

// VintedAdapter queries Vinted's catalog search endpoint.
type VintedAdapter struct {
	enabled       bool
	sessionCookie string
	userAgent     string
	client        *http.Client
}

// NewVintedAdapter creates a Vinted adapter.
func NewVintedAdapter(enabled bool, sessionCookie, userAgent string) *VintedAdapter {
	if userAgent == "" {
		userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	}
	return &VintedAdapter{
		enabled:       enabled,
		sessionCookie: sessionCookie,
		userAgent:     userAgent,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (v *VintedAdapter) Name() model.Source {
	return model.SourceVinted
}

func (v *VintedAdapter) IsEnabled() bool {
	return v.enabled
}

type vintedResponse struct {
	Items []struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
		Price struct {
			Amount       string `json:"amount"`
			CurrencyCode string `json:"currency_code"`
		} `json:"price"`
		URL   string `json:"url"`
		Photo struct {
			URL string `json:"url"`
		} `json:"photo"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"items"`
}

// Search executes a search on Vinted catalog.
func (v *VintedAdapter) Search(ctx context.Context, query string) ([]*model.Listing, error) {
	searchURL := fmt.Sprintf("https://www.vinted.lv/api/v2/catalog/items?search_text=%s&per_page=20", url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", v.userAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "lv,en;q=0.9")

	if v.sessionCookie != "" {
		req.Header.Set("Cookie", v.sessionCookie)
	}

	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vinted HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("vinted Cloudflare challenge active (HTTP %d). Provide VINTED_SESSION_COOKIE in .env to authenticate", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vinted returned HTTP status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read vinted response body: %w", err)
	}

	var data vintedResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("failed to parse vinted JSON response: %w", err)
	}

	var listings []*model.Listing
	for _, item := range data.Items {
		price, _ := strconv.ParseFloat(item.Price.Amount, 64)
		currency := item.Price.CurrencyCode
		if currency == "" {
			currency = "EUR"
		}

		var images []string
		if item.Photo.URL != "" {
			images = append(images, item.Photo.URL)
		}

		itemURL := item.URL
		if itemURL != "" && itemURL[0] == '/' {
			itemURL = "https://www.vinted.lv" + itemURL
		}

		listings = append(listings, &model.Listing{
			Source:      model.SourceVinted,
			SourceID:    strconv.FormatInt(item.ID, 10),
			URL:         itemURL,
			Title:       item.Title,
			Description: "Vinted Catalog Listing",
			Price:       price,
			Currency:    currency,
			ImageURLs:   images,
			Location:    "Latvija / Baltics",
			Seller:      item.User.Login,
		})
	}

	return listings, nil
}
