package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"connectclip-finder/internal/config"
	"connectclip-finder/internal/matcher"
	"connectclip-finder/internal/model"
	"connectclip-finder/internal/notifier"
	"connectclip-finder/internal/scraper"
	"connectclip-finder/internal/storage"
)

// Scheduler manages continuous monitoring cycles and execution timing.
type Scheduler struct {
	interval        time.Duration
	minAlertScore   int
	engine          *scraper.Engine
	matcher         *matcher.Matcher
	aiClassifier    *matcher.AIClassifier
	repo            *storage.Repository
	notifier        *notifier.TelegramNotifier
	products        []config.ProductConfig
	productMatchers map[string]*matcher.Matcher

	mu        sync.Mutex
	isRunning bool
}

// NewScheduler creates a new monitoring scheduler.
func NewScheduler(
	interval time.Duration,
	minAlertScore int,
	engine *scraper.Engine,
	matchEngine *matcher.Matcher,
	aiClassifier *matcher.AIClassifier,
	repo *storage.Repository,
	notifier *notifier.TelegramNotifier,
) *Scheduler {
	return &Scheduler{
		interval:        interval,
		minAlertScore:   minAlertScore,
		engine:          engine,
		matcher:         matchEngine,
		aiClassifier:    aiClassifier,
		repo:            repo,
		notifier:        notifier,
		productMatchers: make(map[string]*matcher.Matcher),
	}
}

// SetProducts configures multiple products and initializes per-product matchers.
func (s *Scheduler) SetProducts(products []config.ProductConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.products = products
	s.productMatchers = make(map[string]*matcher.Matcher)
	for _, p := range products {
		m := matcher.NewCustomMatcher(
			p.Name,
			p.MatchExactKeywords,
			p.MatchContextKeywords,
			p.MatchModelNumbers,
			p.MatchExcludeKeywords,
			p.MinPrice,
			p.MaxPrice,
		)
		s.productMatchers[p.ID] = m
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

// RunOnce executes a single scan cycle with overlap protection across all configured products.
func (s *Scheduler) RunOnce(ctx context.Context) *model.ScanReport {
	s.mu.Lock()
	if s.isRunning {
		slog.Warn("Skipping scheduled scan: previous scan is still running")
		s.mu.Unlock()
		return nil
	}
	s.isRunning = true
	products := append([]config.ProductConfig{}, s.products...)
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.isRunning = false
		s.mu.Unlock()
	}()

	startTime := time.Now().UTC()
	slog.Info("=== Beginning marketplace scan ===")

	if len(products) == 0 {
		report := s.runScanForProduct(ctx, "oticon-connectclip", "Oticon ConnectClip", nil, s.matcher, s.minAlertScore, startTime)
		_ = s.repo.RecordScanRun(ctx, report)
		return report
	}

	var allStatuses []model.SourceStatus
	var totalDiscovered, totalNew, totalCandidates, totalNotifications, totalFailed int

	for _, p := range products {
		if !p.Enabled {
			continue
		}
		select {
		case <-ctx.Done():
			slog.Warn("Scan cancelled during multi-product execution")
			break
		default:
		}

		m := s.productMatchers[p.ID]
		if m == nil {
			m = s.matcher
		}
		thresh := p.AlertThreshold
		if thresh <= 0 {
			thresh = s.minAlertScore
		}

		slog.Info("Scanning product", "product_id", p.ID, "name", p.Name, "terms", len(p.SearchTerms))
		rep := s.runScanForProduct(ctx, p.ID, p.Name, p.SearchTerms, m, thresh, time.Now().UTC())
		if rep != nil {
			totalDiscovered += rep.ListingsDiscovered
			totalNew += rep.NewListings
			totalCandidates += rep.CandidatesFound
			totalNotifications += rep.NotificationsSent
			totalFailed += rep.FailedSources
			allStatuses = append(allStatuses, rep.SourceStatuses...)
		}
	}

	endTime := time.Now().UTC()
	duration := endTime.Sub(startTime)

	overallReport := &model.ScanReport{
		StartedAt:          startTime,
		CompletedAt:        endTime,
		Duration:           duration,
		TotalSources:       len(allStatuses),
		FailedSources:      totalFailed,
		ListingsDiscovered: totalDiscovered,
		NewListings:        totalNew,
		CandidatesFound:    totalCandidates,
		NotificationsSent:  totalNotifications,
		SourceStatuses:     allStatuses,
	}

	_ = s.repo.RecordScanRun(ctx, overallReport)

	slog.Info("=== Multi-product scan completed ===",
		"duration", duration.Round(time.Millisecond),
		"products", len(products),
		"listings_discovered", totalDiscovered,
		"new_listings", totalNew,
		"candidates", totalCandidates,
		"notifications_sent", totalNotifications,
	)

	return overallReport
}

// RunOnceForProduct runs a scan targeted at a specific product.
func (s *Scheduler) RunOnceForProduct(ctx context.Context, productID string) *model.ScanReport {
	s.mu.Lock()
	if s.isRunning {
		slog.Warn("Skipping product scan: another scan is currently running", "product_id", productID)
		s.mu.Unlock()
		return nil
	}
	s.isRunning = true
	var targetProduct *config.ProductConfig
	for i := range s.products {
		if s.products[i].ID == productID {
			targetProduct = &s.products[i]
			break
		}
	}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.isRunning = false
		s.mu.Unlock()
	}()

	startTime := time.Now().UTC()
	if targetProduct == nil {
		rep := s.runScanForProduct(ctx, productID, productID, nil, s.matcher, s.minAlertScore, startTime)
		_ = s.repo.RecordScanRun(ctx, rep)
		return rep
	}

	m := s.productMatchers[targetProduct.ID]
	if m == nil {
		m = s.matcher
	}
	thresh := targetProduct.AlertThreshold
	if thresh <= 0 {
		thresh = s.minAlertScore
	}

	rep := s.runScanForProduct(ctx, targetProduct.ID, targetProduct.Name, targetProduct.SearchTerms, m, thresh, startTime)
	_ = s.repo.RecordScanRun(ctx, rep)
	return rep
}

func (s *Scheduler) runScanForProduct(
	ctx context.Context,
	productID string,
	productName string,
	searchTerms []string,
	m *matcher.Matcher,
	alertThreshold int,
	startTime time.Time,
) *model.ScanReport {
	scrapeResult := s.engine.ExecuteForProduct(ctx, productID, searchTerms)

	var newListingsCount int
	var candidatesCount int
	var notificationsCount int
	var failedSourcesCount int

	for _, status := range scrapeResult.SourceStatuses {
		if status.Error != "" {
			failedSourcesCount++
		}
	}

	for _, listing := range scrapeResult.Listings {
		listing.ProductID = productID
		if m != nil {
			m.Evaluate(listing)
		}

		if s.aiClassifier != nil && s.aiClassifier.IsEnabled() && listing.Score >= 40 && listing.Score < 70 {
			if err := s.aiClassifier.Classify(ctx, listing); err != nil {
				slog.Warn("AI classification warning", "listing_id", listing.ID, "error", err)
			}
		}

		if listing.Score >= alertThreshold {
			candidatesCount++
		}

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

	unnotified, err := s.repo.GetUnnotifiedCandidates(ctx, alertThreshold, productID)
	if err != nil {
		slog.Error("Failed to query unnotified candidates", "product_id", productID, "error", err)
	} else {
		for _, candidate := range unnotified {
			slog.Info("Promising product match found! Sending notification...",
				"product", productName,
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

	return &model.ScanReport{
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
}
