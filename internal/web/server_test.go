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

	// Test 1: GET / (Welcome Screen Grid)
	t.Run("GET / HTML Welcome Screen Grid", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		server.httpServer.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d", rec.Code)
		}

		body := rec.Body.String()
		if !strings.Contains(body, "Marketplace Product Finder") {
			t.Errorf("Welcome screen missing header")
		}
		if !strings.Contains(body, "Oticon ConnectClip") {
			t.Errorf("Welcome screen missing Oticon ConnectClip card")
		}
		if !strings.Contains(body, "Nintendo Switch OLED") {
			t.Errorf("Welcome screen missing Nintendo Switch OLED card")
		}
		if !strings.Contains(body, `id="productGrid"`) {
			t.Errorf("Welcome screen missing product grid container")
		}
		if !strings.Contains(body, `id="btn-records-oticon-connectclip"`) {
			t.Errorf("Welcome screen missing records button for oticon-connectclip")
		}
	})

	// Test 1b: GET /?product=oticon-connectclip (Product Records Dashboard)
	t.Run("GET /?product=oticon-connectclip HTML Dashboard with Sorting and Filters", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/?product=oticon-connectclip", nil)
		rec := httptest.NewRecorder()

		server.httpServer.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d", rec.Code)
		}

		body := rec.Body.String()
		if !strings.Contains(body, "Oticon ConnectClip") {
			t.Errorf("HTML dashboard missing product title")
		}
		if !strings.Contains(body, "btnBackToProducts") {
			t.Errorf("HTML dashboard missing breadcrumb back button")
		}
		if !strings.Contains(body, "Oticon ConnectClip labā stāvoklī") {
			t.Errorf("HTML dashboard missing seeded listing title")
		}
		if !strings.Contains(body, "€85.00") {
			t.Errorf("HTML dashboard missing price formatting")
		}

		// Verify sortable headers
		sortCols := []string{"score", "title", "source", "price", "location", "signals", "time", "status"}
		for _, col := range sortCols {
			expectedAttr := `data-col="` + col + `"`
			if !strings.Contains(body, expectedAttr) {
				t.Errorf("HTML dashboard missing sortable column: %s", col)
			}
		}

		// Verify extended filter controls
		filterElements := []string{
			`id="searchInput"`,
			`id="sourceFilter"`,
			`id="statusFilter"`,
			`id="minPrice"`,
			`id="maxPrice"`,
			`id="priceMinSlider"`,
			`id="priceMaxSlider"`,
			`id="priceRangeDisplay"`,
			`id="scoreSlider"`,
			`id="scoreSliderBadge"`,
			`id="photoFilter"`,
			`id="scoreBtnCandidates"`,
			`id="priceBtnTarget"`,
			`id="visibleCount"`,
			`id="lang-btn-lv"`,
			`id="lang-btn-en"`,
			`id="viewModePagination"`,
			`id="viewModeInfinite"`,
			`id="pageSizeSelect"`,
			`id="paginationTopContainer"`,
			`id="paginationTopInfo"`,
			`id="paginationTopNav"`,
			`id="paginationContainer"`,
			`id="infiniteContainer"`,
			`id="infiniteSentinel"`,
			`id="btnLoadMore"`,
			`data-i18n="view_pagination"`,
			`data-i18n="view_infinite"`,
			`data-i18n="filter_panel_title"`,
			`data-i18n="filter_price_title"`,
		}
		for _, el := range filterElements {
			if !strings.Contains(body, el) {
				t.Errorf("HTML dashboard missing filter element: %s", el)
			}
		}

		// Verify row data attributes used for client-side sorting & filtering
		rowAttrs := []string{
			`data-score="95"`,
			`data-price="85"`,
			`data-signals="2"`,
			`data-source="SS.com"`,
		}
		for _, attr := range rowAttrs {
			if !strings.Contains(body, attr) {
				t.Errorf("HTML row missing data attribute: %s", attr)
			}
		}
	})

	// Test 1c: GET /api/products
	t.Run("GET /api/products JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/products", nil)
		rec := httptest.NewRecorder()

		server.httpServer.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d", rec.Code)
		}

		var prods []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &prods); err != nil {
			t.Fatalf("Failed to unmarshal /api/products JSON: %v", err)
		}
		if len(prods) < 2 {
			t.Fatalf("Expected at least 2 products, got %d", len(prods))
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

	// Test 4: GET /api/listings?paged=true
	t.Run("GET /api/listings?paged=true Paginated JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/listings?paged=true&page=1&limit=10", nil)
		rec := httptest.NewRecorder()

		server.httpServer.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d", rec.Code)
		}

		var res storage.ListingQueryResult
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("Failed to unmarshal paged JSON response: %v", err)
		}

		if res.TotalCount != 1 {
			t.Errorf("Expected TotalCount=1, got %d", res.TotalCount)
		}
		if res.Page != 1 {
			t.Errorf("Expected Page=1, got %d", res.Page)
		}
		if res.PageSize != 10 {
			t.Errorf("Expected PageSize=10, got %d", res.PageSize)
		}
		if len(res.Listings) != 1 {
			t.Errorf("Expected 1 item, got %d", len(res.Listings))
		}
	})

	// Test 5: POST /api/products (Create & Update product with equation)
	t.Run("POST /api/products Create and Update Product", func(t *testing.T) {
		newProdJSON := `{
			"id": "samsung-s26-ultra",
			"name": "Samsung Galaxy S26 Ultra",
			"category": "smartphones",
			"icon": "📱",
			"search_terms": ["Samsung S26 Ultra", "Galaxy S26 Ultra"],
			"min_price": 1000,
			"max_price": 1500,
			"alert_threshold": 75,
			"rule_preset": "custom",
			"custom_rule": "price > 0 && price <= 1350 && score >= 75"
		}`

		req := httptest.NewRequest(http.MethodPost, "/api/products", strings.NewReader(newProdJSON))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		server.httpServer.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify product is in GET /api/products
		getReq := httptest.NewRequest(http.MethodGet, "/api/products", nil)
		getRec := httptest.NewRecorder()
		server.httpServer.Handler.ServeHTTP(getRec, getReq)

		if !strings.Contains(getRec.Body.String(), "samsung-s26-ultra") {
			t.Errorf("Newly created product not found in GET /api/products")
		}
	})

	// Test 6: DELETE /api/products (Safeguard oticon-connectclip & delete custom product)
	t.Run("DELETE /api/products Safeguard and Deletion", func(t *testing.T) {
		// Attempting to delete oticon-connectclip should fail
		reqSafe := httptest.NewRequest(http.MethodDelete, "/api/products?id=oticon-connectclip", nil)
		recSafe := httptest.NewRecorder()
		server.httpServer.Handler.ServeHTTP(recSafe, reqSafe)

		if recSafe.Code != http.StatusBadRequest {
			t.Errorf("Expected HTTP 400 when attempting to delete oticon-connectclip, got %d", recSafe.Code)
		}

		// Deleting samsung-s26-ultra should succeed
		reqDel := httptest.NewRequest(http.MethodDelete, "/api/products?id=samsung-s26-ultra", nil)
		recDel := httptest.NewRecorder()
		server.httpServer.Handler.ServeHTTP(recDel, reqDel)

		if recDel.Code != http.StatusOK {
			t.Errorf("Expected HTTP 200 when deleting product, got %d: %s", recDel.Code, recDel.Body.String())
		}
	})

	// Test 7: GET /api/products/export and POST /api/products/import
	t.Run("Export and Import Products", func(t *testing.T) {
		expReq := httptest.NewRequest(http.MethodGet, "/api/products/export", nil)
		expRec := httptest.NewRecorder()
		server.httpServer.Handler.ServeHTTP(expRec, expReq)

		if expRec.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 on export, got %d", expRec.Code)
		}
		if !strings.Contains(expRec.Body.String(), "oticon-connectclip") {
			t.Errorf("Export JSON missing oticon-connectclip")
		}

		// Import a new list
		importJSON := `[
			{
				"id": "samsung-s25-ultra",
				"name": "Samsung Galaxy S25 Ultra",
				"category": "smartphones",
				"icon": "📱",
				"search_terms": ["S25 Ultra"],
				"max_price": 1200,
				"alert_threshold": 70,
				"rule_preset": "great_deal"
			}
		]`
		impReq := httptest.NewRequest(http.MethodPost, "/api/products/import", strings.NewReader(importJSON))
		impReq.Header.Set("Content-Type", "application/json")
		impRec := httptest.NewRecorder()
		server.httpServer.Handler.ServeHTTP(impRec, impReq)

		if impRec.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 on import, got %d: %s", impRec.Code, impRec.Body.String())
		}
	})

	// Test 8: POST /api/products/validate-rule
	t.Run("POST /api/products/validate-rule", func(t *testing.T) {
		validReq := `{
			"rule": "price <= target_max_price && score >= alert_threshold",
			"price": 850,
			"score": 90,
			"target_max_price": 900,
			"alert_threshold": 70
		}`
		req := httptest.NewRequest(http.MethodPost, "/api/products/validate-rule", strings.NewReader(validReq))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		server.httpServer.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected HTTP 200 on validate-rule, got %d", rec.Code)
		}

		var res map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &res)
		if res["valid"] != true || res["passes"] != true {
			t.Errorf("Expected valid=true and passes=true, got %+v", res)
		}
	})
}

