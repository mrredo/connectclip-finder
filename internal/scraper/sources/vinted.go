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
)

var (
	vintedOverlayRegex = regexp.MustCompile(`<a\s+href="(/items/(\d+)-[^"]*)"[^>]*title="([^"]+)"`)
	vintedImgRegex     = regexp.MustCompile(`<img[^>]+src="([^"]+)"[^>]*data-testid="product-item-id-(\d+)--image--img"`)
	vintedPriceRegex   = regexp.MustCompile(`([\d\s]+(?:[.,]\d+)?)\s*€`)
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

// Search executes a search on Vinted catalog.
func (v *VintedAdapter) Search(ctx context.Context, query string) ([]*model.Listing, error) {
	searchURL := fmt.Sprintf("https://www.vinted.lv/catalog?search_text=%s", url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", v.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
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

	htmlContent := string(body)

	// Map images by item ID
	imagesByID := make(map[string]string)
	for _, m := range vintedImgRegex.FindAllStringSubmatch(htmlContent, -1) {
		if len(m) >= 3 {
			imagesByID[m[2]] = m[1]
		}
	}

	matches := vintedOverlayRegex.FindAllStringSubmatch(htmlContent, -1)
	seen := make(map[string]bool)
	var listings []*model.Listing

	for _, m := range matches {
		if len(m) < 4 {
			continue
		}
		itemPath := m[1]
		itemID := m[2]
		rawTitle := m[3]

		if seen[itemID] {
			continue
		}
		seen[itemID] = true

		// Split title (e.g. "Baterie, Zīmols: Oticon, Stāvoklis: Jauna prece ar etiķetēm, 5.77 €, 6.76 €")
		titleParts := strings.Split(rawTitle, ",")
		title := strings.TrimSpace(titleParts[0])

		var price float64
		if pMatch := vintedPriceRegex.FindStringSubmatch(rawTitle); len(pMatch) > 1 {
			pStr := strings.ReplaceAll(pMatch[1], " ", "")
			pStr = strings.ReplaceAll(pStr, "\u00a0", "")
			pStr = strings.ReplaceAll(pStr, ",", ".")
			price, _ = strconv.ParseFloat(pStr, 64)
		}

		var images []string
		if img, ok := imagesByID[itemID]; ok && img != "" {
			images = append(images, img)
		}

		listings = append(listings, &model.Listing{
			Source:      model.SourceVinted,
			SourceID:    itemID,
			URL:         "https://www.vinted.lv" + itemPath,
			Title:       title,
			Description: rawTitle,
			Price:       price,
			Currency:    "EUR",
			ImageURLs:   images,
			Location:    "Latvija / Baltics",
		})
	}

	return listings, nil
}
