package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"connectclip-finder/internal/model"
	"connectclip-finder/internal/storage"
)

func TestDashboardServer(t *testing.T) {
	tmpDB := "test_web.db"
	_ = os.Remove(tmpDB)
	defer os.Remove(tmpDB)

	db, err := storage.Open(tmpDB)
	if err != nil {
		t.Fatalf("Failed to open test DB: %v", err)
	}
	defer db.Close()

	repo := storage.NewRepository(db)
	ctx := context.Background()

	// Seed dummy listing
	testListing := &model.Listing{
		ID:           "test_source_1",
		Source:       "SS.com",
		Title:        "Oticon ConnectClip labā stāvoklī",
		URL:          "https://www.ss.com/msg/test.html",
		Price:        85.0,
		Score:        95,
		Confidence:   model.ConfidenceHigh,
		MatchReasons: []string{"exact match", "model number"},
		FirstSeenAt:  time.Now().UTC(),
		LastSeenAt:   time.Now().UTC(),
	}
	_, _, err = repo.UpsertListing(ctx, testListing)
	if err != nil {
		t.Fatalf("Failed to upsert test listing: %v", err)
	}

	server := NewServer(":0", repo, nil, 70)

	// Test 1: GET / (Dashboard HTML)
	t.Run("GET / HTML Dashboard", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		server.httpServer.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d", rec.Code)
		}

		body := rec.Body.String()
		if !strings.Contains(body, "ConnectClip Finder") {
			t.Errorf("HTML dashboard missing title header")
		}
		if !strings.Contains(body, "Oticon ConnectClip labā stāvoklī") {
			t.Errorf("HTML dashboard missing seeded listing title")
		}
		if !strings.Contains(body, "€85.00") {
			t.Errorf("HTML dashboard missing price formatting")
		}
	})

	// Test 2: GET /api/listings
	t.Run("GET /api/listings JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/listings", nil)
		rec := httptest.NewRecorder()

		server.httpServer.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d", rec.Code)
		}

		var listings []*model.Listing
		if err := json.Unmarshal(rec.Body.Bytes(), &listings); err != nil {
			t.Fatalf("Failed to unmarshal JSON response: %v", err)
		}

		if len(listings) != 1 {
			t.Fatalf("Expected 1 listing, got %d", len(listings))
		}
		if listings[0].Score != 95 {
			t.Errorf("Expected score 95, got %d", listings[0].Score)
		}
	})

	// Test 3: 404 for unknown path
	t.Run("GET /unknown 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/notfound", nil)
		rec := httptest.NewRecorder()

		server.httpServer.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("Expected HTTP 404, got %d", rec.Code)
		}
	})
}
