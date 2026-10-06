package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
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
	if listing.ProductID == "" {
		listing.ProductID = "oticon-connectclip"
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
			id, product_id, source, source_id, url, title, description, price, currency,
			image_urls, location, seller, score, confidence, match_reasons,
			first_seen_at, last_seen_at, notified, notified_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

		_, err := r.db.ExecContext(ctx, insertQuery,
			listing.ID,
			listing.ProductID,
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
// Optionally filters by productID if provided.
func (r *Repository) GetUnnotifiedCandidates(ctx context.Context, minScore int, productID ...string) ([]*model.Listing, error) {
	whereSQL := "WHERE notified = 0 AND score >= ?"
	args := []any{minScore}
	if len(productID) > 0 && productID[0] != "" {
		whereSQL += " AND product_id = ?"
		args = append(args, productID[0])
	}

	query := fmt.Sprintf(`
	SELECT id, product_id, source, source_id, url, title, description, price, currency,
	       image_urls, location, seller, score, confidence, match_reasons,
	       first_seen_at, last_seen_at, notified, notified_at
	FROM listings
	%s
	ORDER BY score DESC, first_seen_at DESC`, whereSQL)

	rows, err := r.db.QueryContext(ctx, query, args...)
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
			&l.ProductID,
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

// GetAllListings returns all stored listings with optional score filter and optional productID filter.
func (r *Repository) GetAllListings(ctx context.Context, minScore int, productID ...string) ([]*model.Listing, error) {
	whereSQL := "WHERE score >= ?"
	args := []any{minScore}
	if len(productID) > 0 && productID[0] != "" {
		whereSQL += " AND product_id = ?"
		args = append(args, productID[0])
	}

	query := fmt.Sprintf(`
	SELECT id, product_id, source, source_id, url, title, description, price, currency,
	       image_urls, location, seller, score, confidence, match_reasons,
	       first_seen_at, last_seen_at, notified, notified_at
	FROM listings
	%s
	ORDER BY score DESC, last_seen_at DESC`, whereSQL)

	rows, err := r.db.QueryContext(ctx, query, args...)
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
			&l.ProductID,
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

// ProductStat holds summary metrics for a specific product.
type ProductStat struct {
	ProductID  string    `json:"product_id"`
	Total      int       `json:"total"`
	Candidates int       `json:"candidates"`
	Notified   int       `json:"notified"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

// GetStats returns summary counts for the dashboard. If productID is provided, stats are scoped to that product.
func (r *Repository) GetStats(ctx context.Context, minAlertScore int, productID ...string) (total int, candidates int, notified int, err error) {
	var whereClause, whereCand, whereNotif string
	var args, candArgs, notifArgs []any

	if len(productID) > 0 && productID[0] != "" {
		whereClause = "WHERE product_id = ?"
		args = []any{productID[0]}
		whereCand = "WHERE score >= ? AND product_id = ?"
		candArgs = []any{minAlertScore, productID[0]}
		whereNotif = "WHERE notified = 1 AND product_id = ?"
		notifArgs = []any{productID[0]}
	} else {
		whereClause = ""
		args = nil
		whereCand = "WHERE score >= ?"
		candArgs = []any{minAlertScore}
		whereNotif = "WHERE notified = 1"
		notifArgs = nil
	}

	err = r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM listings `+whereClause, args...).Scan(&total)
	if err != nil {
		return 0, 0, 0, err
	}
	err = r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM listings `+whereCand, candArgs...).Scan(&candidates)
	if err != nil {
		return 0, 0, 0, err
	}
	err = r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM listings `+whereNotif, notifArgs...).Scan(&notified)
	if err != nil {
		return 0, 0, 0, err
	}
	return total, candidates, notified, nil
}

// GetProductStats returns statistics grouped by product_id.
func (r *Repository) GetProductStats(ctx context.Context, minAlertScore int) (map[string]ProductStat, error) {
	query := `
	SELECT 
		product_id,
		COUNT(*) as total,
		COUNT(CASE WHEN score >= ? THEN 1 END) as candidates,
		COUNT(CASE WHEN notified = 1 THEN 1 END) as notified,
		MAX(last_seen_at) as last_seen
	FROM listings
	GROUP BY product_id`

	rows, err := r.db.QueryContext(ctx, query, minAlertScore)
	if err != nil {
		return nil, fmt.Errorf("failed to query product stats: %w", err)
	}
	defer rows.Close()

	stats := make(map[string]ProductStat)
	for rows.Next() {
		var ps ProductStat
		var lastSeenRaw any
		if err := rows.Scan(&ps.ProductID, &ps.Total, &ps.Candidates, &ps.Notified, &lastSeenRaw); err != nil {
			return nil, fmt.Errorf("failed to scan product stat: %w", err)
		}
		if lastSeenRaw != nil {
			switch v := lastSeenRaw.(type) {
			case time.Time:
				ps.LastSeenAt = v
			case string:
				for _, layout := range []string{
					time.RFC3339Nano,
					time.RFC3339,
					"2006-01-02 15:04:05.999999999-07:00",
					"2006-01-02 15:04:05-07:00",
					"2006-01-02 15:04:05",
				} {
					if t, err := time.Parse(layout, v); err == nil {
						ps.LastSeenAt = t
						break
					}
				}
			}
		}
		stats[ps.ProductID] = ps
	}
	return stats, rows.Err()
}

// ListingFilter specifies filter and pagination parameters.
type ListingFilter struct {
	ProductID string
	Query     string
	Source    string
	Status    string // "alerted" or "unalerted"
	PhotoOnly bool
	MinScore  int
	MaxScore  int
	MinPrice  float64
	MaxPrice  float64
	SortCol   string // "score", "title", "source", "price", "location", "time", "status"
	SortDir   string // "asc" or "desc"
	Page      int
	PageSize  int
}

// ListingQueryResult contains paginated listing results.
type ListingQueryResult struct {
	Listings   []*model.Listing `json:"items"`
	TotalCount int              `json:"total"`
	Page       int              `json:"page"`
	PageSize   int              `json:"page_size"`
	TotalPages int              `json:"total_pages"`
}

// GetListingsPaged queries listings according to filter, sorting, and pagination parameters.
func (r *Repository) GetListingsPaged(ctx context.Context, f ListingFilter) (*ListingQueryResult, error) {
	var whereClauses []string
	var args []any

	if f.ProductID != "" {
		whereClauses = append(whereClauses, "product_id = ?")
		args = append(args, f.ProductID)
	}
	if f.MinScore > 0 {
		whereClauses = append(whereClauses, "score >= ?")
		args = append(args, f.MinScore)
	}
	if f.MaxScore > 0 {
		whereClauses = append(whereClauses, "score <= ?")
		args = append(args, f.MaxScore)
	}
	if f.Source != "" {
		whereClauses = append(whereClauses, "source = ?")
		args = append(args, f.Source)
	}
	if f.Status == "alerted" {
		whereClauses = append(whereClauses, "notified = 1")
	} else if f.Status == "unalerted" {
		whereClauses = append(whereClauses, "notified = 0")
	}
	if f.PhotoOnly {
		whereClauses = append(whereClauses, "image_urls != '[]' AND image_urls != '' AND image_urls != 'null'")
	}
	if f.MinPrice > 0 {
		whereClauses = append(whereClauses, "price >= ?")
		args = append(args, f.MinPrice)
	}
	if f.MaxPrice > 0 {
		whereClauses = append(whereClauses, "price <= ?")
		args = append(args, f.MaxPrice)
	}
	if f.Query != "" {
		pattern := "%" + f.Query + "%"
		whereClauses = append(whereClauses, "(title LIKE ? OR description LIKE ? OR location LIKE ? OR match_reasons LIKE ?)")
		args = append(args, pattern, pattern, pattern, pattern)
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	// 1. Total count query
	countSQL := "SELECT COUNT(*) FROM listings " + whereSQL
	var totalCount int
	err := r.db.QueryRowContext(ctx, countSQL, args...).Scan(&totalCount)
	if err != nil {
		return nil, fmt.Errorf("failed to count listings: %w", err)
	}

	// 2. Sorting
	sortColSQL := "score"
	switch strings.ToLower(f.SortCol) {
	case "title":
		sortColSQL = "title"
	case "source":
		sortColSQL = "source"
	case "price":
		sortColSQL = "price"
	case "location":
		sortColSQL = "location"
	case "time":
		sortColSQL = "last_seen_at"
	case "status":
		sortColSQL = "notified"
	case "score":
		sortColSQL = "score"
	}

	sortDirSQL := "DESC"
	if strings.ToLower(f.SortDir) == "asc" {
		sortDirSQL = "ASC"
	}

	orderSQL := fmt.Sprintf("ORDER BY %s %s, last_seen_at DESC", sortColSQL, sortDirSQL)

	// 3. Pagination limits
	pageSize := f.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	page := f.Page
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * pageSize

	totalPages := 0
	if totalCount > 0 {
		totalPages = (totalCount + pageSize - 1) / pageSize
	}

	querySQL := fmt.Sprintf(`
	SELECT id, product_id, source, source_id, url, title, description, price, currency,
	       image_urls, location, seller, score, confidence, match_reasons,
	       first_seen_at, last_seen_at, notified, notified_at
	FROM listings
	%s
	%s
	LIMIT ? OFFSET ?`, whereSQL, orderSQL)

	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, pageSize, offset)

	rows, err := r.db.QueryContext(ctx, querySQL, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to query paged listings: %w", err)
	}
	defer rows.Close()

	var listings []*model.Listing
	for rows.Next() {
		var l model.Listing
		var sourceStr, confStr, imagesStr, reasonsStr string
		var notifiedAt sql.NullTime

		err := rows.Scan(
			&l.ID,
			&l.ProductID,
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

		listings = append(listings, &l)
	}

	return &ListingQueryResult{
		Listings:   listings,
		TotalCount: totalCount,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}, rows.Err()
}

// GetAllSources returns a list of distinct sources in alphabetical order, optionally scoped by productID.
func (r *Repository) GetAllSources(ctx context.Context, productID ...string) ([]string, error) {
	whereSQL := "WHERE source != ''"
	var args []any
	if len(productID) > 0 && productID[0] != "" {
		whereSQL += " AND product_id = ?"
		args = append(args, productID[0])
	}

	rows, err := r.db.QueryContext(ctx, fmt.Sprintf("SELECT DISTINCT source FROM listings %s ORDER BY source ASC", whereSQL), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sources []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err == nil {
			sources = append(sources, s)
		}
	}
	return sources, nil
}


