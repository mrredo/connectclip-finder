package storage_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectclip-finder/internal/model"
	"connectclip-finder/internal/storage"
)

func TestRepositoryOperations(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "connectclip_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")
	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer db.Close()

	repo := storage.NewRepository(db)
	ctx := context.Background()

	listing := &model.Listing{
		Source:       model.SourceSSCom,
		SourceID:     "123456",
		URL:          "https://www.ss.com/msg/lv/cx123.html",
		Title:        "Oticon ConnectClip",
		Description:  "Audio streamer",
		Price:        85.0,
		Currency:     "EUR",
		Score:        90,
		Confidence:   model.ConfidenceHigh,
		MatchReasons: []string{"Exact match"},
	}

	// 1. Insert new listing
	isNew, priceChanged, err := repo.UpsertListing(ctx, listing)
	if err != nil {
		t.Fatalf("UpsertListing failed: %v", err)
	}
	if !isNew {
		t.Errorf("expected isNew=true, got false")
	}
	if priceChanged {
		t.Errorf("expected priceChanged=false, got true")
	}

	// 2. Upsert same listing without changes
	isNew, priceChanged, err = repo.UpsertListing(ctx, listing)
	if err != nil {
		t.Fatalf("second UpsertListing failed: %v", err)
	}
	if isNew {
		t.Errorf("expected isNew=false, got true")
	}
	if priceChanged {
		t.Errorf("expected priceChanged=false, got true")
	}

	// 3. Upsert same listing with price change
	listing.Price = 75.0
	isNew, priceChanged, err = repo.UpsertListing(ctx, listing)
	if err != nil {
		t.Fatalf("third UpsertListing failed: %v", err)
	}
	if isNew {
		t.Errorf("expected isNew=false, got true")
	}
	if !priceChanged {
		t.Errorf("expected priceChanged=true, got false")
	}

	// 4. Query unnotified candidates
	candidates, err := repo.GetUnnotifiedCandidates(ctx, 70)
	if err != nil {
		t.Fatalf("GetUnnotifiedCandidates failed: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].Price != 75.0 {
		t.Errorf("expected candidate price 75.0, got %.2f", candidates[0].Price)
	}

	// 5. Mark notified
	if err := repo.MarkNotified(ctx, listing.ID); err != nil {
		t.Fatalf("MarkNotified failed: %v", err)
	}

	// 6. Verify it is no longer returned as unnotified
	candidates, err = repo.GetUnnotifiedCandidates(ctx, 70)
	if err != nil {
		t.Fatalf("GetUnnotifiedCandidates failed: %v", err)
	}
	if len(candidates) != 0 {
		t.Errorf("expected 0 unnotified candidates, got %d", len(candidates))
	}

	// 7. Test RecordScanRun
	report := &model.ScanReport{
		StartedAt:          time.Now().Add(-10 * time.Second),
		CompletedAt:        time.Now(),
		Duration:           10 * time.Second,
		TotalSources:       2,
		ListingsDiscovered: 5,
		NewListings:        1,
		CandidatesFound:    1,
		NotificationsSent:  1,
	}
	if err := repo.RecordScanRun(ctx, report); err != nil {
		t.Fatalf("RecordScanRun failed: %v", err)
	}
	if report.ID == 0 {
		t.Errorf("expected report ID > 0, got %d", report.ID)
	}

	// 8. Test GetAllSources
	sources, err := repo.GetAllSources(ctx)
	if err != nil {
		t.Fatalf("GetAllSources failed: %v", err)
	}
	if len(sources) != 1 || sources[0] != string(model.SourceSSCom) {
		t.Errorf("expected [ss.com], got %v", sources)
	}

	// 9. Test GetListingsPaged
	pagedRes, err := repo.GetListingsPaged(ctx, storage.ListingFilter{
		Page:     1,
		PageSize: 10,
		SortCol:  "score",
		SortDir:  "desc",
	})
	if err != nil {
		t.Fatalf("GetListingsPaged failed: %v", err)
	}
	if pagedRes.TotalCount != 1 {
		t.Errorf("expected TotalCount=1, got %d", pagedRes.TotalCount)
	}
	if len(pagedRes.Listings) != 1 {
		t.Errorf("expected 1 item, got %d", len(pagedRes.Listings))
	}
	if pagedRes.TotalPages != 1 {
		t.Errorf("expected TotalPages=1, got %d", pagedRes.TotalPages)
	}
}

