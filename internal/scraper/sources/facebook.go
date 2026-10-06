package sources

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"connectclip-finder/internal/model"
)

var fbItemRegex = regexp.MustCompile(`/marketplace/item/(\d+)`)

// FacebookAdapter queries Facebook Marketplace using authenticated session cookies.
type FacebookAdapter struct {
	enabled bool
	cookies string
	client  *http.Client
}

// NewFacebookAdapter creates a Facebook Marketplace adapter.
func NewFacebookAdapter(enabled bool, cookies string) *FacebookAdapter {
	adapter := &FacebookAdapter{
		enabled: enabled,
		cookies: cookies,
	}
	adapter.client = &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if strings.Contains(req.URL.Path, "login") {
				return fmt.Errorf("facebook session expired; redirected to login")
			}
			req.Header.Set("Cookie", cookies)
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
			req.Header.Set("Sec-Fetch-Site", "same-origin")
			req.Header.Set("Sec-Fetch-Mode", "navigate")
			req.Header.Set("Sec-Fetch-Dest", "document")
			return nil
		},
	}
	return adapter
}

func (f *FacebookAdapter) Name() model.Source {
	return model.SourceFacebook
}

func (f *FacebookAdapter) IsEnabled() bool {
	return f.enabled
}

// Search queries Facebook Marketplace.
func (f *FacebookAdapter) Search(ctx context.Context, query string) ([]*model.Listing, error) {
	if f.cookies == "" {
		return nil, fmt.Errorf("facebook Marketplace requires authentication; set FB_COOKIES in .env to enable")
	}

	searchURL := fmt.Sprintf("https://www.facebook.com/marketplace/108589842506633/search/?query=%s", url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9,lv;q=0.8")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Cookie", f.cookies)

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("facebook HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusTemporaryRedirect {
		return nil, fmt.Errorf("facebook redirected to login; session cookie expired or invalid")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("facebook returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read facebook response: %w", err)
	}

	htmlContent := string(body)
	matches := fbItemRegex.FindAllStringSubmatch(htmlContent, -1)
	seen := make(map[string]bool)
	var listings []*model.Listing

	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		itemID := m[1]
		if seen[itemID] {
			continue
		}
		seen[itemID] = true

		itemURL := fmt.Sprintf("https://www.facebook.com/marketplace/item/%s", itemID)
		listings = append(listings, &model.Listing{
			Source:      model.SourceFacebook,
			SourceID:    itemID,
			URL:         itemURL,
			Title:       fmt.Sprintf("Facebook Marketplace Item #%s", itemID),
			Description: "Discovered on Facebook Marketplace Riga search for " + query,
			Location:    "Rīga, Latvija",
			Currency:    "EUR",
		})
	}

	return listings, nil
}
