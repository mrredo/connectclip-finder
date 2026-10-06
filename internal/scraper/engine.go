package scraper

import (
	"context"
	"log/slog"
	"time"

	"connectclip-finder/internal/model"
)

// Engine manages and coordinates marketplace source adapters.
type Engine struct {
	adapters    []SourceAdapter
	searchTerms []string
}

// NewEngine creates a new scraper engine.
func NewEngine(searchTerms []string, adapters ...SourceAdapter) *Engine {
	return &Engine{
		adapters:    adapters,
		searchTerms: searchTerms,
	}
}

// Result holds the aggregate output of a scrape run.
type Result struct {
	Listings       []*model.Listing
	SourceStatuses []model.SourceStatus
}

// Execute runs all enabled source adapters across all search queries.
// Failures in individual sources are recorded and isolated without stopping other sources.
func (e *Engine) Execute(ctx context.Context) *Result {
	result := &Result{
		Listings:       make([]*model.Listing, 0),
		SourceStatuses: make([]model.SourceStatus, 0),
	}

	seenIDs := make(map[string]bool)

	for _, adapter := range e.adapters {
		if !adapter.IsEnabled() {
			continue
		}

		sourceName := adapter.Name()
		slog.Info("Starting scrape for source", "source", sourceName)
		start := time.Now()

		var sourceListings []*model.Listing
		var lastErr error

		for i, term := range e.searchTerms {
			select {
			case <-ctx.Done():
				slog.Warn("Scrape interrupted by context cancellation", "source", sourceName)
				return result
			default:
			}

			// Polite inter-query delay
			if i > 0 {
				time.Sleep(1200 * time.Millisecond)
			}

			items, err := adapter.Search(ctx, term)
			if err != nil {
				slog.Warn("Query error on source adapter",
					"source", sourceName,
					"query", term,
					"error", err,
				)
				lastErr = err
				continue
			}

			for _, item := range items {
				id := item.GenerateID()
				item.ID = id
				if !seenIDs[id] {
					seenIDs[id] = true
					sourceListings = append(sourceListings, item)
					result.Listings = append(result.Listings, item)
				}
			}
		}

		duration := time.Since(start)
		status := model.SourceStatus{
			Source:        sourceName,
			Duration:      duration,
			ListingsFound: len(sourceListings),
		}
		if lastErr != nil && len(sourceListings) == 0 {
			status.Error = lastErr.Error()
		}
		result.SourceStatuses = append(result.SourceStatuses, status)

		slog.Info("Completed source scrape",
			"source", sourceName,
			"listings_found", len(sourceListings),
			"duration", duration.Round(time.Millisecond),
		)
	}

	return result
}
