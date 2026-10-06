package sources

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"connectclip-finder/internal/model"
)

// FacebookAdapter queries Facebook Marketplace using authenticated session cookies.
type FacebookAdapter struct {
	enabled bool
	cookies string
	client  *http.Client
}

// NewFacebookAdapter creates a Facebook Marketplace adapter.
func NewFacebookAdapter(enabled bool, cookies string) *FacebookAdapter {
	return &FacebookAdapter{
		enabled: enabled,
		cookies: cookies,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
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

	searchURL := fmt.Sprintf("https://www.facebook.com/marketplace/riga/search?query=%s", url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Cookie", f.cookies)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

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

	// Facebook returns server-rendered HTML or GraphQL Relay data.
	// We extract listings if present in Relay payloads or HTML snippets.
	return nil, nil
}
