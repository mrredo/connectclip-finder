package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

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
	`
	_, err := db.ExecContext(ctx, schema)
	return err
}
