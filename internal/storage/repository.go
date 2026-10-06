package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"connectclip-finder/internal/model"
)

// Repository provides structured database access.
type Repository struct {
	db *DB
}

// NewRepository initializes a Repository.
func NewRepository(db *DB) *Repository {
	return &Repository{db: db}
}

// UpsertListing inserts a new listing or updates last_seen_at / price of an existing one.
// Returns isNew, priceChanged, and error.
func (r *Repository) UpsertListing(ctx context.Context, listing *model.Listing) (bool, bool, error) {
	if listing.ID == "" {
		listing.ID = listing.GenerateID()
	}

	imagesJSON, _ := json.Marshal(listing.ImageURLs)
	reasonsJSON, _ := json.Marshal(listing.MatchReasons)
	now := time.Now().UTC()

	var existingPrice float64
	var notified bool
	queryCheck := `SELECT price, notified FROM listings WHERE id = ?`
	row := r.db.QueryRowContext(ctx, queryCheck, listing.ID)
	err := row.Scan(&existingPrice, &notified)

	if err == sql.ErrNoRows {
		// New listing
		listing.FirstSeenAt = now
		listing.LastSeenAt = now
		listing.Notified = false

		insertQuery := `
		INSERT INTO listings (
			id, source, source_id, url, title, description, price, currency,
			image_urls, location, seller, score, confidence, match_reasons,
			first_seen_at, last_seen_at, notified, notified_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

		_, err := r.db.ExecContext(ctx, insertQuery,
			listing.ID,
			string(listing.Source),
			listing.SourceID,
			listing.URL,
			listing.Title,
			listing.Description,
			listing.Price,
			listing.Currency,
			string(imagesJSON),
			listing.Location,
			listing.Seller,
			listing.Score,
			string(listing.Confidence),
			string(reasonsJSON),
			listing.FirstSeenAt,
			listing.LastSeenAt,
			0,
			nil,
		)
		if err != nil {
			return false, false, fmt.Errorf("failed to insert listing: %w", err)
		}

		if listing.Price > 0 {
			_, _ = r.db.ExecContext(ctx, `INSERT INTO price_history (listing_id, price, recorded_at) VALUES (?, ?, ?)`,
				listing.ID, listing.Price, now)
		}

		return true, false, nil
	} else if err != nil {
		return false, false, fmt.Errorf("failed to query existing listing: %w", err)
	}

	// Existing listing - update last_seen_at, score, and price if changed
	priceChanged := (existingPrice != listing.Price && listing.Price > 0)
	listing.Notified = notified

	updateQuery := `
	UPDATE listings SET
		title = ?,
		description = ?,
		price = ?,
		image_urls = ?,
		location = ?,
		seller = ?,
		score = ?,
		confidence = ?,
		match_reasons = ?,
		last_seen_at = ?
	WHERE id = ?`

	_, err = r.db.ExecContext(ctx, updateQuery,
		listing.Title,
		listing.Description,
		listing.Price,
		string(imagesJSON),
		listing.Location,
		listing.Seller,
		listing.Score,
		string(listing.Confidence),
		string(reasonsJSON),
		now,
		listing.ID,
	)
	if err != nil {
		return false, false, fmt.Errorf("failed to update listing: %w", err)
	}

	if priceChanged {
		_, _ = r.db.ExecContext(ctx, `INSERT INTO price_history (listing_id, price, recorded_at) VALUES (?, ?, ?)`,
			listing.ID, listing.Price, now)
	}

	return false, priceChanged, nil
}

// GetUnnotifiedCandidates fetches listings with score >= minScore that have not yet been notified.
func (r *Repository) GetUnnotifiedCandidates(ctx context.Context, minScore int) ([]*model.Listing, error) {
	query := `
	SELECT id, source, source_id, url, title, description, price, currency,
	       image_urls, location, seller, score, confidence, match_reasons,
	       first_seen_at, last_seen_at, notified, notified_at
	FROM listings
	WHERE notified = 0 AND score >= ?
	ORDER BY score DESC, first_seen_at DESC`

	rows, err := r.db.QueryContext(ctx, query, minScore)
	if err != nil {
		return nil, fmt.Errorf("failed to query unnotified candidates: %w", err)
	}
	defer rows.Close()

	var result []*model.Listing
	for rows.Next() {
		var l model.Listing
		var sourceStr, confStr, imagesStr, reasonsStr string
		var notifiedAt sql.NullTime

		err := rows.Scan(
			&l.ID,
			&sourceStr,
			&l.SourceID,
			&l.URL,
			&l.Title,
			&l.Description,
			&l.Price,
			&l.Currency,
			&imagesStr,
			&l.Location,
			&l.Seller,
			&l.Score,
			&confStr,
			&reasonsStr,
			&l.FirstSeenAt,
			&l.LastSeenAt,
			&l.Notified,
			&notifiedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan listing: %w", err)
		}

		l.Source = model.Source(sourceStr)
		l.Confidence = model.ConfidenceLevel(confStr)
		if notifiedAt.Valid {
			t := notifiedAt.Time
			l.NotifiedAt = &t
		}
		_ = json.Unmarshal([]byte(imagesStr), &l.ImageURLs)
		_ = json.Unmarshal([]byte(reasonsStr), &l.MatchReasons)

		result = append(result, &l)
	}

	return result, rows.Err()
}

// MarkNotified updates the notified flag and timestamp for a listing.
func (r *Repository) MarkNotified(ctx context.Context, id string) error {
	now := time.Now().UTC()
	query := `UPDATE listings SET notified = 1, notified_at = ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, now, id)
	return err
}

// RecordScanRun stores an audit record for each scan cycle.
func (r *Repository) RecordScanRun(ctx context.Context, report *model.ScanReport) error {
	detailsJSON, _ := json.Marshal(report.SourceStatuses)
	query := `
	INSERT INTO scan_runs (
		started_at, completed_at, duration_ms, total_sources, failed_sources,
		listings_discovered, new_listings, candidates_found, notifications_sent,
		details_json
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	res, err := r.db.ExecContext(ctx, query,
		report.StartedAt,
		report.CompletedAt,
		report.Duration.Milliseconds(),
		report.TotalSources,
		report.FailedSources,
		report.ListingsDiscovered,
		report.NewListings,
		report.CandidatesFound,
		report.NotificationsSent,
		string(detailsJSON),
	)
	if err != nil {
		return err
	}
	report.ID, _ = res.LastInsertId()
	return nil
}

// GetAllListings returns all stored listings with optional score filter, ordered by score DESC, last_seen_at DESC.
func (r *Repository) GetAllListings(ctx context.Context, minScore int) ([]*model.Listing, error) {
	query := `
	SELECT id, source, source_id, url, title, description, price, currency,
	       image_urls, location, seller, score, confidence, match_reasons,
	       first_seen_at, last_seen_at, notified, notified_at
	FROM listings
	WHERE score >= ?
	ORDER BY score DESC, last_seen_at DESC`

	rows, err := r.db.QueryContext(ctx, query, minScore)
	if err != nil {
		return nil, fmt.Errorf("failed to query all listings: %w", err)
	}
	defer rows.Close()

	var result []*model.Listing
	for rows.Next() {
		var l model.Listing
		var sourceStr, confStr, imagesStr, reasonsStr string
		var notifiedAt sql.NullTime

		err := rows.Scan(
			&l.ID,
			&sourceStr,
			&l.SourceID,
			&l.URL,
			&l.Title,
			&l.Description,
			&l.Price,
			&l.Currency,
			&imagesStr,
			&l.Location,
			&l.Seller,
			&l.Score,
			&confStr,
			&reasonsStr,
			&l.FirstSeenAt,
			&l.LastSeenAt,
			&l.Notified,
			&notifiedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan listing: %w", err)
		}

		l.Source = model.Source(sourceStr)
		l.Confidence = model.ConfidenceLevel(confStr)
		if notifiedAt.Valid {
			t := notifiedAt.Time
			l.NotifiedAt = &t
		}
		_ = json.Unmarshal([]byte(imagesStr), &l.ImageURLs)
		_ = json.Unmarshal([]byte(reasonsStr), &l.MatchReasons)

		result = append(result, &l)
	}

	return result, rows.Err()
}

// GetStats returns summary counts for the dashboard.
func (r *Repository) GetStats(ctx context.Context, minAlertScore int) (total int, candidates int, notified int, err error) {
	err = r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM listings`).Scan(&total)
	if err != nil {
		return 0, 0, 0, err
	}
	err = r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM listings WHERE score >= ?`, minAlertScore).Scan(&candidates)
	if err != nil {
		return 0, 0, 0, err
	}
	err = r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM listings WHERE notified = 1`).Scan(&notified)
	if err != nil {
		return 0, 0, 0, err
	}
	return total, candidates, notified, nil
}

