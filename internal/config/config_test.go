package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"connectclip-finder/internal/config"
)

func TestConfigLoadAndProducts(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cfg_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	jsonPath := filepath.Join(tempDir, "products.json")
	t.Setenv("PRODUCTS_PATH", jsonPath)
	t.Setenv("TARGET_NAME", "Oticon ConnectClip")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("config.Load failed: %v", err)
	}

	if len(cfg.Products) < 3 {
		t.Fatalf("expected at least 3 default products, got %d", len(cfg.Products))
	}

	p := cfg.GetProduct("oticon-connectclip")
	if p == nil {
		t.Fatalf("expected to find oticon-connectclip product")
	}
	if p.Name != "Oticon ConnectClip" {
		t.Errorf("expected product name 'Oticon ConnectClip', got %q", p.Name)
	}

	// Verify the default file was created
	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		t.Errorf("expected default products.json to be created at %s", jsonPath)
	}
}
