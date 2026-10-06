package scraper

import (
	"context"

	"connectclip-finder/internal/model"
)

// SourceAdapter defines the contract for any marketplace scraper.
type SourceAdapter interface {
	// Name returns the identifier of the marketplace source.
	Name() model.Source

	// IsEnabled checks whether this adapter is active in configuration.
	IsEnabled() bool

	// Search queries the marketplace for the specified search term and returns normalized listings.
	Search(ctx context.Context, query string) ([]*model.Listing, error)
}
