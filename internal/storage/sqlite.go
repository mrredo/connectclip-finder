package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"connectclip-finder/internal/config"

	_ "modernc.org/sqlite"
)

// DB wraps the SQL database handle.
type DB struct {
	*sql.DB
}

// Open initializes SQLite database at the given path and runs migrations.
func Open(dbPath string) (*DB, error) {
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create db directory: %w", err)
		}
	}

	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// Limit connection pool for SQLite file safety
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	sdb := &DB{DB: db}
	if err := sdb.migrate(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return sdb, nil
}

func (db *DB) migrate(ctx context.Context) error {
	schema := `
	CREATE TABLE IF NOT EXISTS listings (
		id TEXT PRIMARY KEY,
		source TEXT NOT NULL,
		source_id TEXT,
		url TEXT NOT NULL,
		title TEXT NOT NULL,
		description TEXT,
		price REAL,
		currency TEXT,
		image_urls TEXT,
		location TEXT,
		seller TEXT,
		score INTEGER,
		confidence TEXT,
		match_reasons TEXT,
		product_id TEXT NOT NULL DEFAULT 'oticon-connectclip',
		first_seen_at DATETIME NOT NULL,
		last_seen_at DATETIME NOT NULL,
		notified BOOLEAN DEFAULT 0,
		notified_at DATETIME
	);

	CREATE INDEX IF NOT EXISTS idx_listings_source ON listings(source);
	CREATE INDEX IF NOT EXISTS idx_listings_score ON listings(score);
	CREATE INDEX IF NOT EXISTS idx_listings_notified ON listings(notified);
	CREATE INDEX IF NOT EXISTS idx_listings_last_seen ON listings(last_seen_at);

	CREATE TABLE IF NOT EXISTS price_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		listing_id TEXT NOT NULL,
		price REAL NOT NULL,
		recorded_at DATETIME NOT NULL,
		FOREIGN KEY (listing_id) REFERENCES listings(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_price_history_listing ON price_history(listing_id);

	CREATE TABLE IF NOT EXISTS scan_runs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		started_at DATETIME NOT NULL,
		completed_at DATETIME NOT NULL,
		duration_ms INTEGER NOT NULL,
		total_sources INTEGER NOT NULL,
		failed_sources INTEGER NOT NULL,
		listings_discovered INTEGER NOT NULL,
		new_listings INTEGER NOT NULL,
		candidates_found INTEGER NOT NULL,
		notifications_sent INTEGER NOT NULL,
		details_json TEXT
	);

	CREATE TABLE IF NOT EXISTS products (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		icon TEXT NOT NULL DEFAULT '📦',
		category TEXT NOT NULL DEFAULT 'General',
		description TEXT,
		enabled BOOLEAN NOT NULL DEFAULT 1,
		search_terms TEXT NOT NULL,
		min_price REAL NOT NULL DEFAULT 0,
		max_price REAL NOT NULL DEFAULT 0,
		alert_threshold INTEGER NOT NULL DEFAULT 70,
		exact_keywords TEXT,
		context_keywords TEXT,
		model_numbers TEXT,
		exclude_keywords TEXT,
		rule_preset TEXT NOT NULL DEFAULT 'great_deal',
		custom_rule TEXT,
		max_alert_price REAL NOT NULL DEFAULT 0,
		min_alert_price REAL NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_products_enabled ON products(enabled);
	`
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return err
	}

	// For existing databases without product_id column in listings, add it safely
	_, _ = db.ExecContext(ctx, "ALTER TABLE listings ADD COLUMN product_id TEXT NOT NULL DEFAULT 'oticon-connectclip'")
	_, _ = db.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS idx_listings_product ON listings(product_id)")

	// Safe alter for products table if columns added later
	_, _ = db.ExecContext(ctx, "ALTER TABLE products ADD COLUMN rule_preset TEXT NOT NULL DEFAULT 'great_deal'")
	_, _ = db.ExecContext(ctx, "ALTER TABLE products ADD COLUMN custom_rule TEXT")
	_, _ = db.ExecContext(ctx, "ALTER TABLE products ADD COLUMN max_alert_price REAL NOT NULL DEFAULT 0")
	_, _ = db.ExecContext(ctx, "ALTER TABLE products ADD COLUMN min_alert_price REAL NOT NULL DEFAULT 0")

	// Seed products table if empty
	var count int
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM products").Scan(&count)
	if count == 0 {
		_ = db.seedProducts(ctx)
	}

	return nil
}

func (db *DB) seedProducts(ctx context.Context) error {
	var prods []config.ProductConfig

	// 1. Try reading products.json if it exists
	if data, err := os.ReadFile("products.json"); err == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &prods)
	}

	// 2. If still empty, use defaults
	if len(prods) == 0 {
		cfg := &config.Config{
			TargetName:     "Oticon ConnectClip",
			TargetMinPrice: 30,
			TargetMaxPrice: 200,
			MinAlertScore:  70,
		}
		prods = config.DefaultProducts(cfg)
	}

	// Ensure oticon-connectclip is in the slice
	hasConnectClip := false
	for _, p := range prods {
		if p.ID == "oticon-connectclip" {
			hasConnectClip = true
			break
		}
	}
	if !hasConnectClip {
		prods = append([]config.ProductConfig{{
			ID:             "oticon-connectclip",
			Name:           "Oticon ConnectClip",
			Icon:           "🎧",
			Category:       "Audio / Hearing",
			Description:    "Wireless hearing aid microphone and Bluetooth audio streamer",
			Enabled:        true,
			SearchTerms:    config.DefaultSearchTerms(),
			MinPrice:       30,
			MaxPrice:       200,
			AlertThreshold: 70,
			RulePreset:     "any_match",
		}}, prods...)
	}

	now := time.Now().UTC()
	for _, p := range prods {
		stJSON, _ := json.Marshal(p.SearchTerms)
		exactJSON, _ := json.Marshal(p.MatchExactKeywords)
		ctxJSON, _ := json.Marshal(p.MatchContextKeywords)
		modelJSON, _ := json.Marshal(p.MatchModelNumbers)
		exclJSON, _ := json.Marshal(p.MatchExcludeKeywords)

		if p.RulePreset == "" {
			p.RulePreset = "great_deal"
		}

		insertQ := `
		INSERT OR IGNORE INTO products (
			id, name, icon, category, description, enabled, search_terms,
			min_price, max_price, alert_threshold, exact_keywords, context_keywords,
			model_numbers, exclude_keywords, rule_preset, custom_rule, max_alert_price,
			min_alert_price, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

		_, _ = db.ExecContext(ctx, insertQ,
			p.ID, p.Name, p.Icon, p.Category, p.Description, p.Enabled, string(stJSON),
			p.MinPrice, p.MaxPrice, p.AlertThreshold, string(exactJSON), string(ctxJSON),
			string(modelJSON), string(exclJSON), p.RulePreset, p.CustomRule, p.MaxAlertPrice,
			p.MinAlertPrice, now, now,
		)
	}

	return nil
}
