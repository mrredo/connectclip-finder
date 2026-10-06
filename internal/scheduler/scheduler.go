package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"connectclip-finder/internal/matcher"
	"connectclip-finder/internal/model"
	"connectclip-finder/internal/notifier"
	"connectclip-finder/internal/scraper"
	"connectclip-finder/internal/storage"
)

// Scheduler manages continuous monitoring cycles and execution timing.
type Scheduler struct {
	interval      time.Duration
	minAlertScore int
	engine        *scraper.Engine
	matcher       *matcher.Matcher
	aiClassifier  *matcher.AIClassifier
	repo          *storage.Repository
	notifier      *notifier.TelegramNotifier

	mu        sync.Mutex
	isRunning bool
}

// NewScheduler creates a new monitoring scheduler.
func NewScheduler(
	interval time.Duration,
	minAlertScore int,
	engine *scraper.Engine,
	matcher *matcher.Matcher,
	aiClassifier *matcher.AIClassifier,
	repo *storage.Repository,
	notifier *notifier.TelegramNotifier,
) *Scheduler {
	return &Scheduler{
		interval:      interval,
		minAlertScore: minAlertScore,
		engine:        engine,
		matcher:       matcher,
		aiClassifier:  aiClassifier,
		repo:          repo,
		notifier:      notifier,
	}
}

// Start runs the periodic monitoring loop until ctx is canceled.
func (s *Scheduler) Start(ctx context.Context) {
	slog.Info("Scheduler starting", "interval", s.interval, "alert_threshold", s.minAlertScore)

	// Execute initial scan immediately on startup
	s.RunOnce(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("Scheduler received shutdown signal")
			return
		case <-ticker.C:
			s.RunOnce(ctx)
		}
	}
}

// RunOnce executes a single scan cycle with overlap protection.
func (s *Scheduler) RunOnce(ctx context.Context) *model.ScanReport {
	s.mu.Lock()
	if s.isRunning {
		slog.Warn("Skipping scheduled scan: previous scan is still running")
		s.mu.Unlock()
		return nil
	}
	s.isRunning = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.isRunning = false
		s.mu.Unlock()
	}()

	startTime := time.Now().UTC()
	slog.Info("=== Beginning marketplace scan ===")

	// 1. Scrape enabled marketplaces
	scrapeResult := s.engine.Execute(ctx)

	var newListingsCount int
	var candidatesCount int
	var notificationsCount int
	var failedSourcesCount int

	for _, status := range scrapeResult.SourceStatuses {
		if status.Error != "" {
			failedSourcesCount++
		}
	}

	// 2. Process each listing through matching & persistence
	for _, listing := range scrapeResult.Listings {
		// Evaluate deterministic scoring
		s.matcher.Evaluate(listing)

		// Optional AI classification for borderline candidates (score between 40 and 70)
		if s.aiClassifier != nil && s.aiClassifier.IsEnabled() && listing.Score >= 40 && listing.Score < 70 {
			if err := s.aiClassifier.Classify(ctx, listing); err != nil {
				slog.Warn("AI classification warning", "listing_id", listing.ID, "error", err)
			}
		}

		if listing.Score >= s.minAlertScore {
			candidatesCount++
		}

		// Persist to SQLite
		isNew, priceChanged, err := s.repo.UpsertListing(ctx, listing)
		if err != nil {
			slog.Error("Failed to persist listing", "id", listing.ID, "error", err)
			continue
		}

		if isNew {
			newListingsCount++
		}

		if priceChanged {
			slog.Info("Price update detected on listing",
				"id", listing.ID,
				"title", listing.Title,
				"new_price", listing.Price,
			)
		}
	}

	// 3. Dispatch notifications for unnotified candidates
	unnotified, err := s.repo.GetUnnotifiedCandidates(ctx, s.minAlertScore)
	if err != nil {
		slog.Error("Failed to query unnotified candidates", "error", err)
	} else {
		for _, candidate := range unnotified {
			slog.Info("Promising Oticon ConnectClip match found! Sending notification...",
				"source", candidate.Source,
				"title", candidate.Title,
				"score", candidate.Score,
				"price", candidate.Price,
			)

			if s.notifier != nil && s.notifier.IsConfigured() {
				if err := s.notifier.SendListingAlert(ctx, candidate); err != nil {
					slog.Error("Failed to send Telegram alert", "id", candidate.ID, "error", err)
					continue
				}
			}

			if err := s.repo.MarkNotified(ctx, candidate.ID); err != nil {
				slog.Error("Failed to mark listing as notified", "id", candidate.ID, "error", err)
			} else {
				notificationsCount++
			}
		}
	}

	endTime := time.Now().UTC()
	duration := endTime.Sub(startTime)

	report := &model.ScanReport{
		StartedAt:          startTime,
		CompletedAt:        endTime,
		Duration:           duration,
		TotalSources:       len(scrapeResult.SourceStatuses),
		FailedSources:      failedSourcesCount,
		ListingsDiscovered: len(scrapeResult.Listings),
		NewListings:        newListingsCount,
		CandidatesFound:    candidatesCount,
		NotificationsSent:  notificationsCount,
		SourceStatuses:     scrapeResult.SourceStatuses,
	}

	_ = s.repo.RecordScanRun(ctx, report)

	slog.Info("=== Scan completed ===",
		"duration", duration.Round(time.Millisecond),
		"sources", report.TotalSources,
		"failed_sources", report.FailedSources,
		"listings_discovered", report.ListingsDiscovered,
		"new_listings", report.NewListings,
		"candidates", report.CandidatesFound,
		"notifications_sent", report.NotificationsSent,
	)

	return report
}
