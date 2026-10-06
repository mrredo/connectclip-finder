package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"connectclip-finder/internal/config"
	"connectclip-finder/internal/model"
	"connectclip-finder/internal/ruleengine"
	"connectclip-finder/internal/scheduler"
	"connectclip-finder/internal/storage"
)

// Server provides an HTTP dashboard displaying tracked listings.
type Server struct {
	addr          string
	repo          *storage.Repository
	sched         *scheduler.Scheduler
	minAlertScore int
	targetName    string
	products      []config.ProductConfig
	httpServer    *http.Server
	mu            sync.RWMutex
}

// NewServer initializes the dashboard HTTP server.
// productsOrTargetName can be []config.ProductConfig, a string (target name), or omitted.
func NewServer(addr string, repo *storage.Repository, sched *scheduler.Scheduler, minAlertScore int, productsOrTargetName ...any) *Server {
	tName := "Oticon ConnectClip"
	var products []config.ProductConfig

	for _, arg := range productsOrTargetName {
		switch v := arg.(type) {
		case []config.ProductConfig:
			products = v
		case config.ProductConfig:
			products = append(products, v)
		case string:
			if v != "" {
				tName = v
			}
		}
	}

	if repo != nil {
		dbProducts, err := repo.GetAllProducts(context.Background())
		if err == nil && len(dbProducts) > 0 {
			products = dbProducts
		}
	}

	if len(products) == 0 {
		products = config.DefaultProducts(&config.Config{
			TargetName:    tName,
			MinAlertScore: minAlertScore,
		})
	}

	s := &Server{
		addr:          addr,
		repo:          repo,
		sched:         sched,
		minAlertScore: minAlertScore,
		targetName:    tName,
		products:      products,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleDashboard)
	mux.HandleFunc("/records", s.handleDashboard)
	mux.HandleFunc("/api/products", s.handleAPIProducts)
	mux.HandleFunc("/api/products/import", s.handleAPIProductsImport)
	mux.HandleFunc("/api/products/export", s.handleAPIProductsExport)
	mux.HandleFunc("/api/products/validate-rule", s.handleAPIValidateRule)
	mux.HandleFunc("/api/listings", s.handleAPIListings)
	mux.HandleFunc("/api/scan", s.handleAPITriggerScan)

	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	return s
}

func (s *Server) getProducts(ctx context.Context) []config.ProductConfig {
	if s.repo != nil {
		prods, err := s.repo.GetAllProducts(ctx)
		if err == nil && len(prods) > 0 {
			s.mu.Lock()
			s.products = prods
			s.mu.Unlock()
			return prods
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]config.ProductConfig{}, s.products...)
}

// Start begins listening on the configured HTTP address.
func (s *Server) Start() error {
	slog.Info("Web dashboard started", "url", fmt.Sprintf("http://localhost%s", s.addr))
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Shutdown stops the HTTP server gracefully.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// ProductCardData contains summary information displayed on the Welcome Screen grid.
type ProductCardData struct {
	ID                   string
	Name                 string
	Icon                 string
	Category             string
	Description          string
	Enabled              bool
	SearchTerms          []string
	MinPrice             float64
	MaxPrice             float64
	AlertThreshold       int
	RulePreset           string
	CustomRule           string
	MaxAlertPrice        float64
	MinAlertPrice        float64
	MatchExactKeywords   []string
	MatchContextKeywords []string
	MatchModelNumbers    []string
	MatchExcludeKeywords []string
	RulePresetDesc       string
	RulePresetDescLv     string
	TotalListings        int
	Candidates           int
	Notified             int
	LastSeenAt           string
}

// welcomeGridData represents the template data for the Welcome Screen Grid.
type welcomeGridData struct {
	TotalProducts   int
	TotalListings   int
	TotalCandidates int
	TotalNotified   int
	Products        []ProductCardData
	LastUpdated     string
}

// dashboardData represents the template data for the Product Records Page.
type dashboardData struct {
	ProductID      string
	TargetName     string
	ProductIcon    string
	Category       string
	TargetMinPrice float64
	TargetMaxPrice float64
	TotalListings  int
	Candidates     int
	Notified       int
	MinAlertScore  int
	Listings       []*model.Listing
	Sources        []string
	LastUpdated    string
	Page           int
	PageSize       int
	TotalPages     int
	MatchedCount   int
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/records" {
		http.NotFound(w, r)
		return
	}

	productID := strings.TrimSpace(r.URL.Query().Get("product"))
	if productID == "" && r.URL.Path == "/" {
		s.handleWelcomeScreen(w, r)
		return
	}

	s.handleRecordsScreen(w, r, productID)
}

func (s *Server) handleWelcomeScreen(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	prods := s.getProducts(ctx)
	pStats, _ := s.repo.GetProductStats(ctx, s.minAlertScore)

	var cards []ProductCardData
	var totalListings, totalCandidates, totalNotified int

	for _, p := range prods {
		ps := pStats[p.ID]
		lastSeen := "—"
		if !ps.LastSeenAt.IsZero() {
			lastSeen = ps.LastSeenAt.Format("15:04 02.01.2006")
		}

		ruleDescEn := ""
		ruleDescLv := ""
		if descs, ok := ruleengine.PresetDescriptions[p.RulePreset]; ok {
			ruleDescEn = descs["en"]
			ruleDescLv = descs["lv"]
		} else if p.RulePreset == "custom" && p.CustomRule != "" {
			ruleDescEn = "Custom: " + p.CustomRule
			ruleDescLv = "Pielāgots: " + p.CustomRule
		}

		cards = append(cards, ProductCardData{
			ID:                   p.ID,
			Name:                 p.Name,
			Icon:                 p.Icon,
			Category:             p.Category,
			Description:          p.Description,
			Enabled:              p.Enabled,
			SearchTerms:          p.SearchTerms,
			MinPrice:             p.MinPrice,
			MaxPrice:             p.MaxPrice,
			AlertThreshold:       p.AlertThreshold,
			RulePreset:           p.RulePreset,
			CustomRule:           p.CustomRule,
			MaxAlertPrice:        p.MaxAlertPrice,
			MinAlertPrice:        p.MinAlertPrice,
			MatchExactKeywords:   p.MatchExactKeywords,
			MatchContextKeywords: p.MatchContextKeywords,
			MatchModelNumbers:    p.MatchModelNumbers,
			MatchExcludeKeywords: p.MatchExcludeKeywords,
			RulePresetDesc:       ruleDescEn,
			RulePresetDescLv:     ruleDescLv,
			TotalListings:        ps.Total,
			Candidates:           ps.Candidates,
			Notified:             ps.Notified,
			LastSeenAt:           lastSeen,
		})

		totalListings += ps.Total
		totalCandidates += ps.Candidates
		totalNotified += ps.Notified
	}

	data := welcomeGridData{
		TotalProducts:   len(prods),
		TotalListings:   totalListings,
		TotalCandidates: totalCandidates,
		TotalNotified:   totalNotified,
		Products:        cards,
		LastUpdated:     time.Now().Format("15:04:05 02.01.2006"),
	}

	tmpl, err := template.New("welcome").Parse(welcomeHTML)
	if err != nil {
		slog.Error("Welcome template parse error", "error", err)
		http.Error(w, "Internal Template Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tmpl.Execute(w, data)
}

func (s *Server) handleRecordsScreen(w http.ResponseWriter, r *http.Request, productID string) {
	ctx := r.Context()
	prods := s.getProducts(ctx)

	var currProduct *config.ProductConfig
	for i := range prods {
		if prods[i].ID == productID {
			currProduct = &prods[i]
			break
		}
	}
	if currProduct == nil {
		if len(prods) > 0 {
			currProduct = &prods[0]
			productID = currProduct.ID
		} else {
			currProduct = &config.ProductConfig{
				ID:             "oticon-connectclip",
				Name:           s.targetName,
				Icon:           "🎧",
				AlertThreshold: s.minAlertScore,
			}
			productID = "oticon-connectclip"
		}
	}

	threshold := currProduct.AlertThreshold
	if threshold <= 0 {
		threshold = s.minAlertScore
	}

	total, candidates, notified, err := s.repo.GetStats(ctx, threshold, productID)
	if err != nil {
		slog.Error("Failed to fetch dashboard stats", "product_id", productID, "error", err)
	}

	sources, _ := s.repo.GetAllSources(ctx, productID)

	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}

	pageSize := 50
	if ps := r.URL.Query().Get("per_page"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil && v > 0 {
			pageSize = v
		}
	}

	filter := storage.ListingFilter{
		ProductID: productID,
		Query:     r.URL.Query().Get("q"),
		Source:    r.URL.Query().Get("source"),
		Status:    r.URL.Query().Get("status"),
		PhotoOnly: r.URL.Query().Get("photo") == "1" || r.URL.Query().Get("photo") == "true",
		SortCol:   r.URL.Query().Get("sort"),
		SortDir:   r.URL.Query().Get("dir"),
		Page:      page,
		PageSize:  pageSize,
	}
	if q := r.URL.Query().Get("min_score"); q != "" {
		filter.MinScore, _ = strconv.Atoi(q)
	}
	if p := r.URL.Query().Get("min_price"); p != "" {
		filter.MinPrice, _ = strconv.ParseFloat(p, 64)
	}
	if p := r.URL.Query().Get("max_price"); p != "" {
		filter.MaxPrice, _ = strconv.ParseFloat(p, 64)
	}

	pagedResult, err := s.repo.GetListingsPaged(ctx, filter)
	if err != nil {
		slog.Error("Failed to fetch listings for dashboard", "error", err)
		http.Error(w, "Failed to load listings", http.StatusInternalServerError)
		return
	}

	data := dashboardData{
		ProductID:      currProduct.ID,
		TargetName:     currProduct.Name,
		ProductIcon:    currProduct.Icon,
		Category:       currProduct.Category,
		TargetMinPrice: currProduct.MinPrice,
		TargetMaxPrice: currProduct.MaxPrice,
		TotalListings:  total,
		Candidates:     candidates,
		Notified:       notified,
		MinAlertScore:  threshold,
		Listings:       pagedResult.Listings,
		Sources:        sources,
		LastUpdated:    time.Now().Format("15:04:05 02.01.2006"),
		Page:           pagedResult.Page,
		PageSize:       pagedResult.PageSize,
		TotalPages:     pagedResult.TotalPages,
		MatchedCount:   pagedResult.TotalCount,
	}

	tmpl, err := template.New("dashboard").Parse(dashboardHTML)
	if err != nil {
		slog.Error("Template parse error", "error", err)
		http.Error(w, "Internal Template Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tmpl.Execute(w, data)
}

func (s *Server) handleAPIProducts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	switch r.Method {
	case http.MethodGet:
		prods := s.getProducts(ctx)
		pStats, _ := s.repo.GetProductStats(ctx, s.minAlertScore)

		type productAPIItem struct {
			ID                   string   `json:"id"`
			Name                 string   `json:"name"`
			Icon                 string   `json:"icon"`
			Category             string   `json:"category"`
			Description          string   `json:"description"`
			Enabled              bool     `json:"enabled"`
			SearchTerms          []string `json:"search_terms"`
			MinPrice             float64  `json:"min_price"`
			MaxPrice             float64  `json:"max_price"`
			AlertThreshold       int      `json:"alert_threshold"`
			RulePreset           string   `json:"rule_preset"`
			CustomRule           string   `json:"custom_rule"`
			MaxAlertPrice        float64  `json:"max_alert_price"`
			MinAlertPrice        float64  `json:"min_alert_price"`
			MatchExactKeywords   []string `json:"match_exact_keywords,omitempty"`
			MatchContextKeywords []string `json:"match_context_keywords,omitempty"`
			MatchModelNumbers    []string `json:"match_model_numbers,omitempty"`
			MatchExcludeKeywords []string `json:"match_exclude_keywords,omitempty"`
			Total                int      `json:"total"`
			Candidates           int      `json:"candidates"`
			Notified             int      `json:"notified"`
			LastSeen             string   `json:"last_seen"`
		}

		items := make([]productAPIItem, 0, len(prods))
		for _, p := range prods {
			ps := pStats[p.ID]
			lastSeenStr := ""
			if !ps.LastSeenAt.IsZero() {
				lastSeenStr = ps.LastSeenAt.Format(time.RFC3339)
			}
			items = append(items, productAPIItem{
				ID:                   p.ID,
				Name:                 p.Name,
				Icon:                 p.Icon,
				Category:             p.Category,
				Description:          p.Description,
				Enabled:              p.Enabled,
				SearchTerms:          p.SearchTerms,
				MinPrice:             p.MinPrice,
				MaxPrice:             p.MaxPrice,
				AlertThreshold:       p.AlertThreshold,
				RulePreset:           p.RulePreset,
				CustomRule:           p.CustomRule,
				MaxAlertPrice:        p.MaxAlertPrice,
				MinAlertPrice:        p.MinAlertPrice,
				MatchExactKeywords:   p.MatchExactKeywords,
				MatchContextKeywords: p.MatchContextKeywords,
				MatchModelNumbers:    p.MatchModelNumbers,
				MatchExcludeKeywords: p.MatchExcludeKeywords,
				Total:                ps.Total,
				Candidates:           ps.Candidates,
				Notified:             ps.Notified,
				LastSeen:             lastSeenStr,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(items)

	case http.MethodPost:
		var p config.ProductConfig
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		p.Name = strings.TrimSpace(p.Name)
		if p.Name == "" {
			http.Error(w, "Product name is required", http.StatusBadRequest)
			return
		}

		p.ID = strings.TrimSpace(p.ID)
		if p.ID == "" {
			slug := strings.ToLower(p.Name)
			slug = strings.ReplaceAll(slug, " ", "-")
			slug = strings.Map(func(r rune) rune {
				if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
					return r
				}
				return -1
			}, slug)
			p.ID = slug
		}

		if p.Icon == "" {
			p.Icon = "📦"
		}
		if p.Category == "" {
			p.Category = "general"
		}
		if p.AlertThreshold <= 0 {
			p.AlertThreshold = s.minAlertScore
			if p.AlertThreshold <= 0 {
				p.AlertThreshold = 70
			}
		}
		if p.RulePreset == "" {
			p.RulePreset = ruleengine.PresetGreatDeal
		}
		if p.RulePreset == ruleengine.PresetCustom && p.CustomRule != "" {
			if _, err := ruleengine.Validate(p.CustomRule); err != nil {
				http.Error(w, "Invalid custom rule formula: "+err.Error(), http.StatusBadRequest)
				return
			}
		}

		if err := s.repo.UpsertProduct(ctx, p); err != nil {
			http.Error(w, "Failed to save product: "+err.Error(), http.StatusInternalServerError)
			return
		}

		updated, _ := s.repo.GetAllProducts(ctx)
		s.mu.Lock()
		s.products = updated
		s.mu.Unlock()
		if s.sched != nil {
			s.sched.SetProducts(updated)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"product": p,
		})

	case http.MethodDelete:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			var bodyReq struct {
				ID string `json:"id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&bodyReq)
			id = strings.TrimSpace(bodyReq.ID)
		}

		if id == "" {
			http.Error(w, "Missing product id", http.StatusBadRequest)
			return
		}

		if id == "oticon-connectclip" {
			http.Error(w, "oticon-connectclip is the anchor product and cannot be deleted", http.StatusBadRequest)
			return
		}

		if err := s.repo.DeleteProduct(ctx, id); err != nil {
			http.Error(w, "Failed to delete product: "+err.Error(), http.StatusInternalServerError)
			return
		}

		updated, _ := s.repo.GetAllProducts(ctx)
		s.mu.Lock()
		s.products = updated
		s.mu.Unlock()
		if s.sched != nil {
			s.sched.SetProducts(updated)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":     "ok",
			"deleted_id": id,
		})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPIProductsImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var data []byte
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "multipart/form-data") {
		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "Failed to read uploaded file: "+err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()
		var readErr error
		data, readErr = io.ReadAll(file)
		if readErr != nil {
			http.Error(w, "Failed to read file content: "+readErr.Error(), http.StatusBadRequest)
			return
		}
	} else {
		var err error
		data, err = io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read request body: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	count, err := s.repo.ImportProductsFromJSON(r.Context(), data)
	if err != nil {
		http.Error(w, "Failed to import products: "+err.Error(), http.StatusBadRequest)
		return
	}

	updated, _ := s.repo.GetAllProducts(r.Context())
	s.mu.Lock()
	s.products = updated
	s.mu.Unlock()
	if s.sched != nil {
		s.sched.SetProducts(updated)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":         "ok",
		"imported_count": count,
		"total_products": len(updated),
	})
}

func (s *Server) handleAPIProductsExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	data, err := s.repo.ExportProductsToJSON(r.Context())
	if err != nil {
		http.Error(w, "Failed to export products: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"products.json\"")
	_, _ = w.Write(data)
}

func (s *Server) handleAPIValidateRule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type validateReq struct {
		Rule           string  `json:"rule"`
		Preset         string  `json:"preset"`
		Price          float64 `json:"price"`
		Score          float64 `json:"score"`
		TargetMinPrice float64 `json:"target_min_price"`
		TargetMaxPrice float64 `json:"target_max_price"`
		MaxAlertPrice  float64 `json:"max_alert_price"`
		MinAlertPrice  float64 `json:"min_alert_price"`
		AlertThreshold float64 `json:"alert_threshold"`
		HasPhoto       bool    `json:"has_photo"`
		Source         string  `json:"source"`
	}

	var req validateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	rule := req.Rule
	if rule == "" && req.Preset != "" {
		rule = ruleengine.ResolvePresetEquation(req.Preset, req.TargetMaxPrice, req.AlertThreshold)
	}

	ruleCtx := ruleengine.Context{
		Price:          req.Price,
		Score:          req.Score,
		TargetMinPrice: req.TargetMinPrice,
		TargetMaxPrice: req.TargetMaxPrice,
		MaxAlertPrice:  req.MaxAlertPrice,
		MinAlertPrice:  req.MinAlertPrice,
		AlertThreshold: req.AlertThreshold,
		HasPhoto:       req.HasPhoto,
		Source:         req.Source,
	}
	if ruleCtx.AlertThreshold <= 0 {
		ruleCtx.AlertThreshold = float64(s.minAlertScore)
	}

	passes, reason, err := ruleengine.Evaluate(rule, ruleCtx)
	resp := map[string]any{
		"valid":    err == nil,
		"equation": rule,
		"passes":   passes,
		"reason":   reason,
	}
	if err != nil {
		resp["error"] = err.Error()
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleAPIListings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	productID := q.Get("product")
	if productID == "" {
		productID = q.Get("product_id")
	}

	// If no page/limit/paged requested (backward compatible with unmarshaling []*model.Listing in tests)
	if q.Get("page") == "" && q.Get("paged") != "true" && q.Get("limit") == "" {
		minScore := 0
		if m := q.Get("min_score"); m != "" {
			minScore, _ = strconv.Atoi(m)
		}
		listings, err := s.repo.GetAllListings(ctx, minScore, productID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(listings)
		return
	}

	// Paginated request
	filter := storage.ListingFilter{
		ProductID: productID,
		Query:     q.Get("q"),
		Source:    q.Get("source"),
		Status:    q.Get("status"),
		PhotoOnly: q.Get("photo") == "1" || q.Get("photo") == "true",
		SortCol:   q.Get("sort"),
		SortDir:   q.Get("dir"),
		Page:      1,
		PageSize:  50,
	}
	if p := q.Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			filter.Page = v
		}
	}
	if ps := q.Get("limit"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil && v > 0 {
			filter.PageSize = v
		}
	} else if ps := q.Get("per_page"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil && v > 0 {
			filter.PageSize = v
		}
	}
	if m := q.Get("min_score"); m != "" {
		filter.MinScore, _ = strconv.Atoi(m)
	}
	if m := q.Get("max_score"); m != "" {
		filter.MaxScore, _ = strconv.Atoi(m)
	}
	if p := q.Get("min_price"); p != "" {
		filter.MinPrice, _ = strconv.ParseFloat(p, 64)
	}
	if p := q.Get("max_price"); p != "" {
		filter.MaxPrice, _ = strconv.ParseFloat(p, 64)
	}

	res, err := s.repo.GetListingsPaged(ctx, filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

func (s *Server) handleAPITriggerScan(w http.ResponseWriter, r *http.Request) {
	if s.sched == nil {
		http.Error(w, "Scheduler not available", http.StatusBadRequest)
		return
	}

	productID := r.URL.Query().Get("product")
	if productID == "" {
		productID = r.URL.Query().Get("product_id")
	}

	go func(pid string) {
		if pid != "" {
			slog.Info("Manual scan triggered for product via Web UI", "product_id", pid)
			_ = s.sched.RunOnceForProduct(context.Background(), pid)
		} else {
			slog.Info("Manual scan triggered for all products via Web UI")
			_ = s.sched.RunOnce(context.Background())
		}
	}(productID)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":     "scan_started",
		"product_id": productID,
		"message":    "Marketplace scan initiated in background.",
	})
}

const welcomeHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Marketplace Product Finder - Multi-Product Monitor</title>
<style>
  :root {
    --bg: #0f172a;
    --card: #1e293b;
    --card-border: #334155;
    --text: #f8fafc;
    --text-muted: #94a3b8;
    --primary: #38bdf8;
    --primary-hover: #0284c7;
    --high: #22c55e;
    --medium: #f59e0b;
    --danger: #ef4444;
    --low: #64748b;
  }
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
    background-color: var(--bg);
    color: var(--text);
    padding: 24px 20px;
  }
  .container { max-width: 1440px; margin: 0 auto; }
  header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    flex-wrap: wrap;
    gap: 16px;
    margin-bottom: 24px;
    padding-bottom: 20px;
    border-bottom: 1px solid var(--card-border);
  }
  h1 { font-size: 1.6rem; font-weight: 700; color: #fff; display: flex; align-items: center; gap: 10px; }
  .badge-app {
    font-size: 0.75rem;
    padding: 4px 10px;
    background: #0284c7;
    border-radius: 9999px;
    font-weight: 600;
  }
  .actions { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
  .btn {
    background: var(--primary);
    color: #0f172a;
    border: none;
    padding: 8px 16px;
    border-radius: 6px;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.2s, transform 0.15s;
    text-decoration: none;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 0.9rem;
  }
  .btn:hover { background: var(--primary-hover); transform: translateY(-1px); }
  .btn-sm { padding: 6px 12px; font-size: 0.82rem; }
  .btn-secondary { background: var(--card-border); color: #fff; }
  .btn-secondary:hover { background: #475569; }
  .btn-success { background: #10b981; color: #fff; }
  .btn-success:hover { background: #059669; }
  .btn-danger { background: rgba(239, 68, 68, 0.15); color: #ef4444; border: 1px solid rgba(239, 68, 68, 0.3); }
  .btn-danger:hover { background: #ef4444; color: #fff; }
  .btn-icon { padding: 8px 12px; font-size: 0.9rem; }

  /* Language Switcher */
  .lang-switch {
    display: inline-flex;
    background: #0f172a;
    border: 1px solid var(--card-border);
    border-radius: 8px;
    padding: 3px;
    gap: 2px;
  }
  .lang-btn {
    background: transparent;
    border: none;
    color: var(--text-muted);
    padding: 6px 12px;
    border-radius: 6px;
    cursor: pointer;
    font-size: 0.82rem;
    font-weight: 600;
    transition: all 0.15s ease;
  }
  .lang-btn.active { background: #0284c7; color: #fff; }
  .lang-btn:hover:not(.active) { color: #fff; background: #1e293b; }

  /* Global Stats Bar */
  .stats-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
    gap: 16px;
    margin-bottom: 24px;
  }
  .stat-card {
    background: var(--card);
    border: 1px solid var(--card-border);
    border-radius: 8px;
    padding: 16px 20px;
  }
  .stat-label { font-size: 0.85rem; color: var(--text-muted); margin-bottom: 6px; text-transform: uppercase; letter-spacing: 0.5px; }
  .stat-val { font-size: 1.8rem; font-weight: 700; color: #fff; }
  .stat-val.high { color: var(--high); }
  .stat-val.primary { color: var(--primary); }

  /* Grid Toolbar */
  .grid-toolbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 16px;
    margin-bottom: 24px;
    flex-wrap: wrap;
    background: var(--card);
    border: 1px solid var(--card-border);
    padding: 14px 20px;
    border-radius: 8px;
  }
  .search-wrap {
    position: relative;
    flex: 1;
    min-width: 280px;
    max-width: 420px;
  }
  .search-input {
    background: #0f172a;
    border: 1px solid var(--card-border);
    color: #fff;
    padding: 9px 14px 9px 36px;
    border-radius: 6px;
    width: 100%;
    font-size: 0.9rem;
  }
  .search-input:focus { outline: 1px solid var(--primary); }
  .search-icon {
    position: absolute;
    left: 12px;
    top: 50%;
    transform: translateY(-50%);
    color: var(--text-muted);
    font-size: 0.85rem;
    pointer-events: none;
  }

  /* Product Cards Grid */
  .product-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(360px, 1fr));
    gap: 24px;
  }
  .product-card {
    background: var(--card);
    border: 1px solid var(--card-border);
    border-radius: 12px;
    padding: 24px;
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    transition: transform 0.2s ease, border-color 0.2s ease, box-shadow 0.2s ease;
  }
  .product-card:hover {
    transform: translateY(-3px);
    border-color: #38bdf8;
    box-shadow: 0 10px 24px rgba(0, 0, 0, 0.4);
  }
  .card-header {
    display: flex;
    gap: 16px;
    align-items: flex-start;
    margin-bottom: 12px;
  }
  .card-icon {
    font-size: 2.2rem;
    width: 58px;
    height: 58px;
    background: #0f172a;
    border: 1px solid var(--card-border);
    border-radius: 12px;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
  }
  .card-titles { flex: 1; min-width: 0; }
  .card-title {
    font-size: 1.25rem;
    font-weight: 700;
    color: #fff;
    margin-bottom: 4px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .card-badges {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
    align-items: center;
  }
  .badge-category {
    font-size: 0.72rem;
    padding: 2px 8px;
    background: #334155;
    border-radius: 9999px;
    color: #cbd5e1;
    font-weight: 500;
  }
  .badge-status {
    font-size: 0.72rem;
    padding: 2px 8px;
    border-radius: 9999px;
    font-weight: 600;
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .badge-status.active {
    background: rgba(34, 197, 94, 0.15);
    color: #4ade80;
    border: 1px solid rgba(34, 197, 94, 0.3);
  }
  .badge-status.paused {
    background: rgba(148, 163, 184, 0.15);
    color: #94a3b8;
    border: 1px solid rgba(148, 163, 184, 0.3);
  }
  .dot {
    width: 6px;
    height: 6px;
    background: #22c55e;
    border-radius: 50%;
    box-shadow: 0 0 6px #22c55e;
  }
  .card-desc {
    font-size: 0.88rem;
    color: var(--text-muted);
    line-height: 1.45;
    margin-bottom: 14px;
    min-height: 38px;
  }
  .card-pills {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
    margin-bottom: 14px;
  }
  .pill {
    font-size: 0.75rem;
    padding: 4px 10px;
    border-radius: 6px;
    background: #0f172a;
    border: 1px solid var(--card-border);
    color: #cbd5e1;
  }
  .pill-price { color: #38bdf8; border-color: rgba(56, 189, 248, 0.3); }
  .pill-score { color: #4ade80; border-color: rgba(34, 197, 94, 0.3); }
  .pill-rule { color: #a855f7; border-color: rgba(168, 85, 247, 0.35); font-weight: 500; }

  .card-metrics {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    background: #0f172a;
    border: 1px solid var(--card-border);
    border-radius: 8px;
    padding: 12px;
    gap: 8px;
    text-align: center;
    margin-bottom: 14px;
  }
  .metric-label { font-size: 0.72rem; color: var(--text-muted); text-transform: uppercase; margin-bottom: 4px; }
  .metric-val { font-size: 1.25rem; font-weight: 700; color: #fff; }
  .metric-val.high { color: var(--high); }
  .metric-val.primary { color: var(--primary); }

  .card-last-seen {
    font-size: 0.78rem;
    color: var(--text-muted);
    margin-bottom: 16px;
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .card-footer {
    display: flex;
    gap: 8px;
    align-items: center;
  }
  .btn-view {
    flex: 1;
    justify-content: center;
    padding: 9px 14px;
    font-size: 0.9rem;
  }
  .btn-scan-card {
    padding: 9px 12px;
    font-size: 0.85rem;
  }

  /* Modal Dialog */
  .modal-backdrop {
    position: fixed;
    top: 0; left: 0; right: 0; bottom: 0;
    background: rgba(0, 0, 0, 0.75);
    backdrop-filter: blur(4px);
    display: none;
    align-items: center;
    justify-content: center;
    z-index: 999;
    padding: 16px;
  }
  .modal-backdrop.open { display: flex; }
  .modal-dialog {
    background: #1e293b;
    border: 1px solid var(--card-border);
    border-radius: 12px;
    width: 720px;
    max-width: 100%;
    max-height: 90vh;
    display: flex;
    flex-direction: column;
    box-shadow: 0 20px 40px rgba(0, 0, 0, 0.6);
  }
  .modal-header {
    padding: 18px 24px;
    border-bottom: 1px solid var(--card-border);
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .modal-title { font-size: 1.2rem; font-weight: 700; color: #fff; }
  .modal-close {
    background: transparent;
    border: none;
    color: var(--text-muted);
    font-size: 1.4rem;
    cursor: pointer;
    padding: 4px;
    line-height: 1;
  }
  .modal-close:hover { color: #fff; }
  .modal-tabs {
    display: flex;
    border-bottom: 1px solid var(--card-border);
    background: #0f172a;
    padding: 0 16px;
  }
  .modal-tab-btn {
    padding: 12px 18px;
    background: transparent;
    border: none;
    border-bottom: 2px solid transparent;
    color: var(--text-muted);
    font-weight: 600;
    font-size: 0.88rem;
    cursor: pointer;
    transition: all 0.15s ease;
  }
  .modal-tab-btn.active {
    color: var(--primary);
    border-bottom-color: var(--primary);
  }
  .modal-body {
    padding: 20px 24px;
    overflow-y: auto;
    flex: 1;
  }
  .modal-footer {
    padding: 16px 24px;
    border-top: 1px solid var(--card-border);
    display: flex;
    justify-content: flex-end;
    gap: 12px;
    background: #1e293b;
  }

  /* Form Elements */
  .form-group { margin-bottom: 16px; }
  .form-row { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
  .form-row-3 { display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 12px; }
  .form-label {
    display: block;
    font-size: 0.82rem;
    font-weight: 600;
    color: #cbd5e1;
    margin-bottom: 6px;
  }
  .form-input, .form-select, .form-textarea {
    width: 100%;
    background: #0f172a;
    border: 1px solid var(--card-border);
    border-radius: 6px;
    padding: 8px 12px;
    color: #fff;
    font-size: 0.9rem;
  }
  .form-input:focus, .form-select:focus, .form-textarea:focus {
    outline: 1px solid var(--primary);
    border-color: var(--primary);
  }
  .form-help {
    font-size: 0.76rem;
    color: var(--text-muted);
    margin-top: 4px;
    line-height: 1.35;
  }
  .chip-group { display: flex; flex-wrap: wrap; gap: 6px; margin: 8px 0; }
  .chip {
    padding: 3px 8px;
    border-radius: 4px;
    background: #334155;
    color: #cbd5e1;
    font-size: 0.75rem;
    cursor: pointer;
    font-family: monospace;
    border: 1px solid rgba(255, 255, 255, 0.08);
  }
  .chip:hover { background: #475569; color: #fff; border-color: var(--primary); }
  .chip.chip-op { background: #1e1e38; color: #38bdf8; }
  .chip.chip-op:hover { background: #2d2d54; }

  .formula-tester {
    margin-top: 14px;
    padding: 12px;
    background: #0f172a;
    border: 1px solid var(--card-border);
    border-radius: 8px;
  }
  .formula-result {
    margin-top: 8px;
    padding: 8px 12px;
    border-radius: 6px;
    font-size: 0.82rem;
    display: none;
  }
  .formula-result.pass { display: block; background: rgba(34, 197, 94, 0.15); color: #4ade80; border: 1px solid rgba(34, 197, 94, 0.3); }
  .formula-result.fail { display: block; background: rgba(239, 68, 68, 0.15); color: #ef4444; border: 1px solid rgba(239, 68, 68, 0.3); }

  /* Toast Notification */
  .toast {
    position: fixed;
    bottom: 24px;
    right: 24px;
    background: #1e293b;
    border: 1px solid var(--primary);
    box-shadow: 0 10px 25px rgba(0,0,0,0.5);
    border-radius: 8px;
    padding: 14px 20px;
    color: #fff;
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 0.9rem;
    z-index: 1000;
    transition: opacity 0.3s ease, transform 0.3s ease;
    transform: translateY(30px);
    opacity: 0;
    pointer-events: none;
  }
  .toast.show {
    transform: translateY(0);
    opacity: 1;
    pointer-events: auto;
  }
</style>
</head>
<body>
<div class="container">
  <header>
    <div>
      <h1>🔎 <span data-i18n="app_title">Marketplace Product Finder</span> <span class="badge-app" data-i18n="app_badge">Multi-Product Monitor</span></h1>
      <p style="font-size: 0.85rem; color: var(--text-muted); margin-top: 4px;" data-i18n="welcome_sub">
        Concurrent marketplace monitoring with custom deal equations & Telegram alerts
      </p>
    </div>
    <div class="actions">
      <!-- Language Switcher -->
      <div class="lang-switch">
        <button id="welcome-lang-lv" class="lang-btn" onclick="setWelcomeLang('lv')">🇱🇻 Latviešu</button>
        <button id="welcome-lang-en" class="lang-btn active" onclick="setWelcomeLang('en')">🇬🇧 English</button>
      </div>

      <button class="btn btn-success" onclick="openAddModal()">
        <span data-i18n="btn_add_product">➕ Add Product</span>
      </button>
      <button class="btn btn-secondary" onclick="openImportModal()">
        <span data-i18n="btn_import_json">📥 Import JSON</span>
      </button>
      <a href="/api/products/export" class="btn btn-secondary" download="products.json">
        <span data-i18n="btn_export_json">📤 Export JSON</span>
      </a>

      <button class="btn" id="btnScanAll" onclick="triggerAllScan(this)">
        <span data-i18n="btn_scan_all">⚡ Scan All Products</span>
      </button>
      <a href="/" class="btn btn-secondary" data-i18n="btn_refresh">Refresh</a>
    </div>
  </header>

  <!-- Global Stats -->
  <div class="stats-grid">
    <div class="stat-card">
      <div class="stat-label" data-i18n="stat_monitored">Monitored Products</div>
      <div class="stat-val primary">{{.TotalProducts}}</div>
    </div>
    <div class="stat-card">
      <div class="stat-label" data-i18n="stat_total_listings">Total Tracked Listings</div>
      <div class="stat-val">{{.TotalListings}}</div>
    </div>
    <div class="stat-card">
      <div class="stat-label" data-i18n="stat_candidates">Match Candidates</div>
      <div class="stat-val high">{{.TotalCandidates}}</div>
    </div>
    <div class="stat-card">
      <div class="stat-label" data-i18n="stat_alerts">Alerts Dispatched</div>
      <div class="stat-val">{{.TotalNotified}}</div>
    </div>
  </div>

  <!-- Product Grid Toolbar -->
  <div class="grid-toolbar">
    <div class="search-wrap">
      <span class="search-icon">🔍</span>
      <input type="text" id="productSearchInput" class="search-input" data-i18n-placeholder="search_placeholder" placeholder="Filter products by name or category..." oninput="filterProducts()">
    </div>
    <div style="font-size: 0.85rem; color: var(--text-muted);">
      <span id="productCounter">{{len .Products}}</span> <span data-i18n="products_count_suffix">products active</span>
    </div>
  </div>

  <!-- Product Cards Grid -->
  <div class="product-grid" id="productGrid">
    {{range .Products}}
      <div class="product-card" data-id="{{.ID}}" data-name="{{.Name}}" data-cat="{{.Category}}">
        <div>
          <div class="card-header">
            <div class="card-icon">{{.Icon}}</div>
            <div class="card-titles">
              <div class="card-title" title="{{.Name}}">{{.Name}}</div>
              <div class="card-badges">
                {{if .Category}}<span class="badge-category">{{.Category}}</span>{{end}}
                {{if .Enabled}}
                  <span class="badge-status active"><span class="dot"></span> <span data-i18n="status_active">Active</span></span>
                {{else}}
                  <span class="badge-status paused"><span data-i18n="status_paused">Paused</span></span>
                {{end}}
              </div>
            </div>
          </div>

          <div class="card-desc">{{.Description}}</div>

          <div class="card-pills">
            {{if gt .MinPrice 0.0}}
              <div class="pill pill-price">
                <span data-i18n="target_price">Target:</span> €{{printf "%.0f" .MinPrice}} – €{{printf "%.0f" .MaxPrice}}
              </div>
            {{end}}
            <div class="pill pill-score">
              <span data-i18n="score_thresh">Threshold:</span> &ge;{{.AlertThreshold}}%
            </div>
            <div class="pill pill-rule" title="{{if .CustomRule}}{{.CustomRule}}{{else}}{{.RulePresetDesc}}{{end}}">
              ⚖️ {{if .RulePresetDescLv}}{{.RulePresetDescLv}}{{else}}{{.RulePreset}}{{end}}
            </div>
            <div class="pill">
              {{len .SearchTerms}} <span data-i18n="search_terms_count">queries</span>
            </div>
          </div>

          <div class="card-metrics">
            <div>
              <div class="metric-label" data-i18n="label_total">Total</div>
              <div class="metric-val">{{.TotalListings}}</div>
            </div>
            <div>
              <div class="metric-label" data-i18n="label_candidates">Candidates</div>
              <div class="metric-val high">{{.Candidates}}</div>
            </div>
            <div>
              <div class="metric-label" data-i18n="label_alerted">Alerted</div>
              <div class="metric-val primary">{{.Notified}}</div>
            </div>
          </div>

          <div class="card-last-seen">
            <span data-i18n="last_seen">Last seen:</span>
            <strong>{{.LastSeenAt}}</strong>
          </div>
        </div>

        <div class="card-footer">
          <a href="/?product={{.ID}}" class="btn btn-view" id="btn-records-{{.ID}}">
            <span data-i18n="view_records">View Records →</span>
          </a>
          <button class="btn btn-secondary btn-scan-card" id="btn-scan-{{.ID}}" onclick="triggerProductScan('{{.ID}}', this)" title="Scan now">
            <span data-i18n="scan_now">Scan ⚡</span>
          </button>
          <button class="btn btn-secondary btn-icon" onclick="openEditModal('{{.ID}}')" title="Edit product">✏️</button>
          {{if ne .ID "oticon-connectclip"}}
          <button class="btn btn-danger btn-icon" onclick="deleteProduct('{{.ID}}', '{{.Name}}')" title="Delete product">🗑️</button>
          {{end}}
        </div>
      </div>
    {{else}}
      <div style="grid-column: 1 / -1; text-align: center; padding: 48px; background: var(--card); border: 1px solid var(--card-border); border-radius: 12px; color: var(--text-muted);" data-i18n="no_products">
        No products configured. Click "+ Add Product" or "Import JSON".
      </div>
    {{end}}
  </div>
</div>

<!-- Add / Edit Product Modal -->
<div id="productModal" class="modal-backdrop">
  <div class="modal-dialog">
    <div class="modal-header">
      <div class="modal-title" id="productModalTitle" data-i18n="modal_add_title">Add New Product</div>
      <button class="modal-close" onclick="closeProductModal()">&times;</button>
    </div>
    <div class="modal-tabs">
      <button class="modal-tab-btn active" onclick="switchModalTab('tabBasic', this)" data-i18n="tab_basic">1. Basic Info</button>
      <button class="modal-tab-btn" onclick="switchModalTab('tabSearch', this)" data-i18n="tab_search">2. Search & Keywords</button>
      <button class="modal-tab-btn" onclick="switchModalTab('tabRules', this)" data-i18n="tab_rules">3. Pricing & Equation</button>
    </div>
    <form id="productForm" onsubmit="saveProduct(event)">
      <div class="modal-body">
        <!-- Tab 1: Basic Info -->
        <div id="tabBasic" class="tab-pane active">
          <div class="form-row">
            <div class="form-group">
              <label class="form-label" data-i18n="lbl_name">Product Name *</label>
              <input type="text" id="pName" class="form-input" required placeholder="e.g. Samsung Galaxy S24 Ultra" oninput="autoSlug()">
            </div>
            <div class="form-group">
              <label class="form-label" data-i18n="lbl_id">Product ID (slug) *</label>
              <input type="text" id="pID" class="form-input" required placeholder="e.g. samsung-s24-ultra">
            </div>
          </div>

          <div class="form-row">
            <div class="form-group">
              <label class="form-label" data-i18n="lbl_icon">Icon Emoji</label>
              <input type="text" id="pIcon" class="form-input" value="📱" placeholder="Emoji symbol">
            </div>
            <div class="form-group">
              <label class="form-label" data-i18n="lbl_category">Category</label>
              <select id="pCategory" class="form-select">
                <option value="smartphones">Smartphones / Viedtālruņi</option>
                <option value="hearing-aids">Hearing Aids / Dzirdes aparāti</option>
                <option value="gaming">Gaming & Consoles / Spēles</option>
                <option value="audio">Audio & Headphones / Audio</option>
                <option value="electronics">Electronics / Elektronika</option>
                <option value="general">General / Cits</option>
              </select>
            </div>
          </div>

          <div class="form-group">
            <label class="form-label" data-i18n="lbl_description">Description</label>
            <textarea id="pDescription" class="form-textarea" rows="2" placeholder="Brief notes or target specifications..."></textarea>
          </div>

          <div class="form-group">
            <label style="display:flex; align-items:center; gap:8px; cursor:pointer;">
              <input type="checkbox" id="pEnabled" checked style="width:16px; height:16px;">
              <span class="form-label" style="margin:0;" data-i18n="lbl_enabled">Monitoring Active</span>
            </label>
          </div>
        </div>

        <!-- Tab 2: Search & Keywords -->
        <div id="tabSearch" class="tab-pane" style="display:none;">
          <div class="form-group">
            <label class="form-label" data-i18n="lbl_search_terms">Search Queries (one per line) *</label>
            <textarea id="pSearchTerms" class="form-textarea" rows="4" required placeholder="Samsung S24 Ultra&#10;Galaxy S24 Ultra&#10;S24 Ultra"></textarea>
            <div class="form-help" data-i18n="help_search_terms">Marketplace search queries used when querying ss.com, andele, banknote, etc.</div>
          </div>

          <div class="form-group">
            <label class="form-label" data-i18n="lbl_model_numbers">Model Numbers (comma separated)</label>
            <input type="text" id="pModelNumbers" class="form-input" placeholder="e.g. S928, SM-S928B, 178509">
          </div>

          <div class="form-group">
            <label class="form-label" data-i18n="lbl_exclude_words">Negative / Exclude Words (comma separated)</label>
            <input type="text" id="pExcludeWords" class="form-input" placeholder="e.g. case, vāciņš, cover, stikls, dummy, broken, bojāts">
            <div class="form-help" data-i18n="help_exclude_words">Listings containing these words are penalized or skipped.</div>
          </div>
        </div>

        <!-- Tab 3: Pricing & Custom Deal Equation -->
        <div id="tabRules" class="tab-pane" style="display:none;">
          <div class="form-row-3">
            <div class="form-group">
              <label class="form-label" data-i18n="lbl_target_min">Target Min Price (€)</label>
              <input type="number" step="1" id="pMinPrice" class="form-input" placeholder="e.g. 500">
            </div>
            <div class="form-group">
              <label class="form-label" data-i18n="lbl_target_max">Target Max Price (€)</label>
              <input type="number" step="1" id="pMaxPrice" class="form-input" placeholder="e.g. 850">
            </div>
            <div class="form-group">
              <label class="form-label" data-i18n="lbl_alert_thresh">Alert Threshold (%)</label>
              <input type="number" min="1" max="100" id="pAlertThreshold" class="form-input" value="70">
            </div>
          </div>

          <div class="form-row">
            <div class="form-group">
              <label class="form-label" data-i18n="lbl_max_alert_price">Max Alert Price Bound (€)</label>
              <input type="number" step="1" id="pMaxAlertPrice" class="form-input" placeholder="Never alert above this price">
            </div>
            <div class="form-group">
              <label class="form-label" data-i18n="lbl_deal_preset">Deal Strategy Preset</label>
              <select id="pRulePreset" class="form-select" onchange="handlePresetChange()">
                <option value="great_deal">Laba cena / Great Deal (price within target max & score >= threshold)</option>
                <option value="steal_deal">Īpaši izdevīgs / Steal Deal (price <= 75% target max)</option>
                <option value="high_match_photo">Augsta atbilstība ar foto / High Match + Photo (score >= 80 & has photo)</option>
                <option value="budget_limit">Stingrs budžets / Strict Budget (price <= target max)</option>
                <option value="any_match">Jebkura atbilstība / Any Match (ignore price)</option>
                <option value="custom">Pielāgots vienādojums / Custom Equation</option>
              </select>
            </div>
          </div>

          <div class="form-group">
            <label class="form-label" data-i18n="lbl_custom_rule">Custom Deal Equation Formula</label>
            <div style="font-size:0.75rem; color:var(--text-muted); margin-bottom:4px;" data-i18n="help_click_vars">Click variables & operators to insert into equation:</div>
            <div class="chip-group">
              <span class="chip" onclick="insertVar('price')">price</span>
              <span class="chip" onclick="insertVar('score')">score</span>
              <span class="chip" onclick="insertVar('target_max_price')">target_max_price</span>
              <span class="chip" onclick="insertVar('target_min_price')">target_min_price</span>
              <span class="chip" onclick="insertVar('max_alert_price')">max_alert_price</span>
              <span class="chip" onclick="insertVar('alert_threshold')">alert_threshold</span>
              <span class="chip" onclick="insertVar('has_photo')">has_photo</span>
              <span class="chip chip-op" onclick="insertVar(' && ')">&amp;&amp;</span>
              <span class="chip chip-op" onclick="insertVar(' || ')">||</span>
              <span class="chip chip-op" onclick="insertVar(' <= ')">&lt;=</span>
              <span class="chip chip-op" onclick="insertVar(' >= ')">&gt;=</span>
              <span class="chip chip-op" onclick="insertVar(' == 1 ')">== 1</span>
              <span class="chip chip-op" onclick="insertVar(' * 0.75 ')">* 0.75</span>
            </div>
            <textarea id="pCustomRule" class="form-textarea" rows="2" placeholder="price > 0 && price <= target_max_price && score >= alert_threshold"></textarea>
            <div class="form-help" data-i18n="help_equation">Leave blank to use the selected strategy preset automatically.</div>
          </div>

          <!-- Live Equation Tester -->
          <div class="formula-tester">
            <div style="display:flex; justify-content:space-between; align-items:center;">
              <span style="font-size:0.8rem; font-weight:600; color:#fff;" data-i18n="lbl_test_box">Live Formula Verification</span>
              <button type="button" class="btn btn-secondary btn-sm" onclick="testFormula()" data-i18n="btn_test_formula">Test Formula</button>
            </div>
            <div id="testResult" class="formula-result"></div>
          </div>
        </div>
      </div>
      <div class="modal-footer">
        <button type="button" class="btn btn-secondary" onclick="closeProductModal()" data-i18n="btn_cancel">Cancel</button>
        <button type="submit" class="btn btn-success" data-i18n="btn_save">Save Product</button>
      </div>
    </form>
  </div>
</div>

<!-- Import JSON Modal -->
<div id="importModal" class="modal-backdrop">
  <div class="modal-dialog" style="width:560px;">
    <div class="modal-header">
      <div class="modal-title" data-i18n="modal_import_title">Import Products from JSON</div>
      <button class="modal-close" onclick="closeImportModal()">&times;</button>
    </div>
    <form onsubmit="submitImport(event)">
      <div class="modal-body">
        <div class="form-group">
          <label class="form-label" data-i18n="lbl_import_file">Choose JSON File</label>
          <input type="file" id="importFileInput" accept=".json" class="form-input" onchange="handleImportFile(event)">
        </div>
        <div class="form-group">
          <label class="form-label" data-i18n="lbl_import_paste">Or Paste JSON Content</label>
          <textarea id="importJSONText" class="form-textarea" rows="8" placeholder='[&#10;  {&#10;    "id": "samsung-s24-ultra",&#10;    "name": "Samsung Galaxy S24 Ultra",&#10;    ...&#10;  }&#10;]'></textarea>
        </div>
      </div>
      <div class="modal-footer">
        <button type="button" class="btn btn-secondary" onclick="closeImportModal()" data-i18n="btn_cancel">Cancel</button>
        <button type="submit" class="btn btn-success" data-i18n="btn_import_action">Import Products</button>
      </div>
    </form>
  </div>
</div>

<div id="toast" class="toast">
  <span id="toastIcon">⚡</span>
  <span id="toastMsg">Marketplace scan started in background!</span>
</div>

<script>
const welcomeI18n = {
  en: {
    app_title: "Marketplace Product Finder",
    app_badge: "Multi-Product Monitor",
    welcome_sub: "Concurrent marketplace monitoring with custom deal equations & Telegram alerts",
    btn_add_product: "➕ Add Product",
    btn_import_json: "📥 Import JSON",
    btn_export_json: "📤 Export JSON",
    btn_scan_all: "⚡ Scan All Products",
    btn_refresh: "Refresh",
    stat_monitored: "Monitored Products",
    stat_total_listings: "Total Tracked Listings",
    stat_candidates: "Match Candidates",
    stat_alerts: "Alerts Dispatched",
    search_placeholder: "Filter products by name or category...",
    products_count_suffix: "products active",
    view_records: "View Records →",
    scan_now: "Scan ⚡",
    status_active: "Active",
    status_paused: "Paused",
    label_total: "Total",
    label_candidates: "Candidates",
    label_alerted: "Alerted",
    last_seen: "Last seen:",
    target_price: "Target:",
    score_thresh: "Threshold:",
    search_terms_count: "queries",
    modal_add_title: "Add New Product",
    modal_edit_title: "Edit Product",
    tab_basic: "1. Basic Info",
    tab_search: "2. Search & Keywords",
    tab_rules: "3. Pricing & Equation",
    lbl_name: "Product Name *",
    lbl_id: "Product ID (slug) *",
    lbl_icon: "Icon Emoji",
    lbl_category: "Category",
    lbl_description: "Description",
    lbl_enabled: "Monitoring Active",
    lbl_search_terms: "Search Queries (one per line) *",
    help_search_terms: "Marketplace search queries used when querying ss.com, andele, banknote, etc.",
    lbl_model_numbers: "Model Numbers (comma separated)",
    lbl_exclude_words: "Negative / Exclude Words (comma separated)",
    help_exclude_words: "Listings containing these words are penalized or skipped.",
    lbl_target_min: "Target Min Price (€)",
    lbl_target_max: "Target Max Price (€)",
    lbl_alert_thresh: "Alert Threshold (%)",
    lbl_max_alert_price: "Max Alert Price Bound (€)",
    lbl_deal_preset: "Deal Strategy Preset",
    lbl_custom_rule: "Custom Deal Equation Formula",
    help_click_vars: "Click variables & operators to insert into equation:",
    help_equation: "Leave blank to use the selected strategy preset automatically.",
    lbl_test_box: "Live Formula Verification",
    btn_test_formula: "Test Formula",
    btn_cancel: "Cancel",
    btn_save: "Save Product",
    modal_import_title: "Import Products from JSON",
    lbl_import_file: "Choose JSON File",
    lbl_import_paste: "Or Paste JSON Content",
    btn_import_action: "Import Products",
    toast_all_started: "Scan started for all products in background!",
    toast_single_started: "Scan started for selected product in background!",
    toast_saved: "Product saved successfully!",
    toast_deleted: "Product deleted!",
    toast_imported: "Products imported successfully!",
    no_products: "No products configured. Click '+ Add Product' or 'Import JSON'."
  },
  lv: {
    app_title: "Tirgus Preču Meklētājs",
    app_badge: "Vairāku Preču Monitors",
    welcome_sub: "Vienlaicīga tirgus uzraudzība ar pielāgotiem darījumu vienādojumiem un Telegram paziņojumiem",
    btn_add_product: "➕ Pievienot preci",
    btn_import_json: "📥 Importēt JSON",
    btn_export_json: "📤 Eksportēt JSON",
    btn_scan_all: "⚡ Skenēt visus produktus",
    btn_refresh: "Atjaunot",
    stat_monitored: "Uzraudzītie produkti",
    stat_total_listings: "Kopējie sludinājumi",
    stat_candidates: "Atbilstošie kandidāti",
    stat_alerts: "Nosūtītie paziņojumi",
    search_placeholder: "Filtrēt produktus pēc nosaukuma vai kategorijas...",
    products_count_suffix: "aktīvi produkti",
    view_records: "Skatīt ierakstus →",
    scan_now: "Skenēt ⚡",
    status_active: "Aktīvs",
    status_paused: "Apturēts",
    label_total: "Kopā",
    label_candidates: "Kandidāti",
    label_alerted: "Paziņoti",
    last_seen: "Pēdējoreiz:",
    target_price: "Mērķis:",
    score_thresh: "Slieksnis:",
    search_terms_count: "vaicājumi",
    modal_add_title: "Pievienot jaunu preci",
    modal_edit_title: "Rediģēt preci",
    tab_basic: "1. Pamatinformācija",
    tab_search: "2. Meklēšana un atslēgvārdi",
    tab_rules: "3. Cenas un vienādojums",
    lbl_name: "Preces nosaukums *",
    lbl_id: "Preces ID (slug) *",
    lbl_icon: "Ikonas emocijzīme",
    lbl_category: "Kategorija",
    lbl_description: "Apraksts",
    lbl_enabled: "Uzraudzība aktīva",
    lbl_search_terms: "Meklēšanas vaicājumi (viens rindā) *",
    help_search_terms: "Meklēšanas frāzes portālos ss.com, andele, banknote utt.",
    lbl_model_numbers: "Modeļu numuri (ar komatiem)",
    lbl_exclude_words: "Izslēdzamie / negatīvie vārdi (ar komatiem)",
    help_exclude_words: "Sludinājumi ar šiem vārdiem tiek pazemināti vai izlaisti.",
    lbl_target_min: "Mērķa minimālā cena (€)",
    lbl_target_max: "Mērķa maksimālā cena (€)",
    lbl_alert_thresh: "Brīdinājuma slieksnis (%)",
    lbl_max_alert_price: "Maks. brīdinājuma cenas griesti (€)",
    lbl_deal_preset: "Darījuma stratēģijas sagatave",
    lbl_custom_rule: "Pielāgota darījuma vienādojuma formula",
    help_click_vars: "Noklikšķiniet uz mainīgā vai operatora, lai ievietotu formulā:",
    help_equation: "Atstājiet tukšu, lai automātiski izmantotu izvēlēto sagatavi.",
    lbl_test_box: "Formulas tūlītēja pārbaude",
    btn_test_formula: "Pārbaudīt formulu",
    btn_cancel: "Atcelt",
    btn_save: "Saglabāt preci",
    modal_import_title: "Importēt preces no JSON",
    lbl_import_file: "Izvēlēties JSON failu",
    lbl_import_paste: "Vai ielīmēt JSON tekstu",
    btn_import_action: "Importēt preces",
    toast_all_started: "Visu produktu skenēšana palaista fonā!",
    toast_single_started: "Izvēlētā produkta skenēšana palaista fonā!",
    toast_saved: "Prece veiksmīgi saglabāta!",
    toast_deleted: "Prece izdzēsta!",
    toast_imported: "Preces veiksmīgi importētas!",
    no_products: "Nav konfigurētu preču. Noklikšķiniet '+ Pievienot preci' vai 'Importēt JSON'."
  }
};

let currentLang = 'en';

function setWelcomeLang(lang) {
  if (!welcomeI18n[lang]) return;
  currentLang = lang;

  document.querySelectorAll('.lang-btn').forEach(b => b.classList.remove('active'));
  const activeBtn = document.getElementById('welcome-lang-' + lang);
  if (activeBtn) activeBtn.classList.add('active');

  const dict = welcomeI18n[lang];
  document.querySelectorAll('[data-i18n]').forEach(el => {
    const key = el.getAttribute('data-i18n');
    if (dict[key]) el.innerText = dict[key];
  });

  document.querySelectorAll('[data-i18n-placeholder]').forEach(el => {
    const key = el.getAttribute('data-i18n-placeholder');
    if (dict[key]) el.placeholder = dict[key];
  });

  try {
    localStorage.setItem('cc_finder_lang', lang);
  } catch (e) {}
}

function filterProducts() {
  const query = (document.getElementById('productSearchInput').value || '').toLowerCase().trim();
  const cards = document.querySelectorAll('.product-card');
  let visible = 0;
  cards.forEach(card => {
    const name = (card.getAttribute('data-name') || '').toLowerCase();
    const cat = (card.getAttribute('data-cat') || '').toLowerCase();
    if (!query || name.includes(query) || cat.includes(query)) {
      card.style.display = '';
      visible++;
    } else {
      card.style.display = 'none';
    }
  });
  const counter = document.getElementById('productCounter');
  if (counter) counter.innerText = String(visible);
}

function showToast(msg) {
  const toast = document.getElementById('toast');
  const msgEl = document.getElementById('toastMsg');
  if (!toast || !msgEl) return;
  msgEl.innerText = msg;
  toast.classList.add('show');
  setTimeout(() => { toast.classList.remove('show'); }, 4000);
}

function triggerAllScan(btn) {
  if (btn) btn.disabled = true;
  fetch('/api/scan', { method: 'POST' })
    .then(r => r.json())
    .then(() => {
      showToast(welcomeI18n[currentLang].toast_all_started);
      setTimeout(() => { window.location.reload(); }, 6000);
    })
    .catch(err => {
      alert('Error triggering scan: ' + err);
      if (btn) btn.disabled = false;
    });
}

function triggerProductScan(productID, btn) {
  if (btn) btn.disabled = true;
  fetch('/api/scan?product=' + encodeURIComponent(productID), { method: 'POST' })
    .then(r => r.json())
    .then(() => {
      showToast(welcomeI18n[currentLang].toast_single_started);
      setTimeout(() => { window.location.reload(); }, 6000);
    })
    .catch(err => {
      alert('Error triggering product scan: ' + err);
      if (btn) btn.disabled = false;
    });
}

/* Modal Management */
function switchModalTab(tabId, btn) {
  document.querySelectorAll('.tab-pane').forEach(p => p.style.display = 'none');
  document.querySelectorAll('.modal-tab-btn').forEach(b => b.classList.remove('active'));
  const target = document.getElementById(tabId);
  if (target) target.style.display = 'block';
  if (btn) btn.classList.add('active');
}

function autoSlug() {
  const idInput = document.getElementById('pID');
  if (idInput.getAttribute('data-editing') === 'true') return;
  const name = document.getElementById('pName').value || '';
  const slug = name.toLowerCase().trim().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '');
  idInput.value = slug;
}

function openAddModal() {
  document.getElementById('productForm').reset();
  const idInput = document.getElementById('pID');
  idInput.disabled = false;
  idInput.removeAttribute('data-editing');
  document.getElementById('productModalTitle').innerText = welcomeI18n[currentLang].modal_add_title;
  document.getElementById('pIcon').value = '📱';
  document.getElementById('pEnabled').checked = true;
  document.getElementById('pAlertThreshold').value = '70';
  document.getElementById('pRulePreset').value = 'great_deal';
  document.getElementById('testResult').style.display = 'none';
  switchModalTab('tabBasic', document.querySelector('.modal-tab-btn'));
  document.getElementById('productModal').classList.add('open');
}

function openEditModal(productID) {
  fetch('/api/products')
    .then(r => r.json())
    .then(products => {
      const p = products.find(x => x.id === productID);
      if (!p) {
        alert('Product not found: ' + productID);
        return;
      }
      document.getElementById('productModalTitle').innerText = welcomeI18n[currentLang].modal_edit_title + ': ' + p.name;
      const idInput = document.getElementById('pID');
      idInput.value = p.id;
      idInput.disabled = true;
      idInput.setAttribute('data-editing', 'true');
      document.getElementById('pName').value = p.name || '';
      document.getElementById('pIcon').value = p.icon || '📱';
      document.getElementById('pCategory').value = p.category || 'general';
      document.getElementById('pDescription').value = p.description || '';
      document.getElementById('pEnabled').checked = p.enabled !== false;
      document.getElementById('pSearchTerms').value = (p.search_terms || []).join('\n');
      document.getElementById('pModelNumbers').value = (p.match_model_numbers || []).join(', ');
      document.getElementById('pExcludeWords').value = (p.match_exclude_keywords || []).join(', ');
      document.getElementById('pMinPrice').value = p.min_price > 0 ? p.min_price : '';
      document.getElementById('pMaxPrice').value = p.max_price > 0 ? p.max_price : '';
      document.getElementById('pAlertThreshold').value = p.alert_threshold || 70;
      document.getElementById('pMaxAlertPrice').value = p.max_alert_price > 0 ? p.max_alert_price : '';
      document.getElementById('pRulePreset').value = p.rule_preset || 'great_deal';
      document.getElementById('pCustomRule').value = p.custom_rule || '';
      document.getElementById('testResult').style.display = 'none';
      switchModalTab('tabBasic', document.querySelector('.modal-tab-btn'));
      document.getElementById('productModal').classList.add('open');
    })
    .catch(err => alert('Failed to load product: ' + err));
}

function closeProductModal() {
  document.getElementById('productModal').classList.remove('open');
}

function insertVar(snippet) {
  const textarea = document.getElementById('pCustomRule');
  const start = textarea.selectionStart || textarea.value.length;
  const end = textarea.selectionEnd || textarea.value.length;
  textarea.value = textarea.value.substring(0, start) + snippet + textarea.value.substring(end);
  textarea.focus();
  textarea.selectionStart = textarea.selectionEnd = start + snippet.length;
}

function handlePresetChange() {
  const preset = document.getElementById('pRulePreset').value;
  const customBox = document.getElementById('pCustomRule');
  if (preset === 'custom' && !customBox.value.trim()) {
    customBox.value = 'price > 0 && price <= target_max_price && score >= alert_threshold';
  }
}

function testFormula() {
  const rule = document.getElementById('pCustomRule').value.trim();
  const preset = document.getElementById('pRulePreset').value;
  const maxPrice = parseFloat(document.getElementById('pMaxPrice').value) || 800;
  const thresh = parseFloat(document.getElementById('pAlertThreshold').value) || 70;
  const maxAlert = parseFloat(document.getElementById('pMaxAlertPrice').value) || 0;

  const payload = {
    rule: rule,
    preset: preset,
    price: maxPrice > 0 ? (maxPrice * 0.9) : 350,
    score: 85,
    target_max_price: maxPrice,
    alert_threshold: thresh,
    max_alert_price: maxAlert,
    has_photo: true,
    source: "ss.com"
  };

  fetch('/api/products/validate-rule', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  })
  .then(r => r.json())
  .then(res => {
    const el = document.getElementById('testResult');
    if (!res.valid) {
      el.className = 'formula-result fail';
      el.innerText = '❌ Error: ' + res.error;
    } else if (res.passes) {
      el.className = 'formula-result pass';
      el.innerText = '✅ Formula Valid & Passed! Result: ' + res.reason;
    } else {
      el.className = 'formula-result fail';
      el.innerText = '⚠️ Formula Valid, but condition evaluated to FALSE with test values.';
    }
  })
  .catch(err => {
    const el = document.getElementById('testResult');
    el.className = 'formula-result fail';
    el.innerText = '❌ Request failed: ' + err;
  });
}

function saveProduct(e) {
  e.preventDefault();
  const id = document.getElementById('pID').value.trim();
  const name = document.getElementById('pName').value.trim();
  if (!id || !name) {
    alert('Product Name and ID are required.');
    return;
  }

  const searchTerms = document.getElementById('pSearchTerms').value
    .split('\n')
    .map(s => s.trim())
    .filter(Boolean);

  const modelNumbers = document.getElementById('pModelNumbers').value
    .split(',')
    .map(s => s.trim())
    .filter(Boolean);

  const excludeWords = document.getElementById('pExcludeWords').value
    .split(',')
    .map(s => s.trim())
    .filter(Boolean);

  const payload = {
    id: id,
    name: name,
    icon: document.getElementById('pIcon').value.trim() || '📱',
    category: document.getElementById('pCategory').value,
    description: document.getElementById('pDescription').value.trim(),
    enabled: document.getElementById('pEnabled').checked,
    search_terms: searchTerms,
    match_model_numbers: modelNumbers,
    match_exclude_keywords: excludeWords,
    min_price: parseFloat(document.getElementById('pMinPrice').value) || 0,
    max_price: parseFloat(document.getElementById('pMaxPrice').value) || 0,
    alert_threshold: parseInt(document.getElementById('pAlertThreshold').value) || 70,
    max_alert_price: parseFloat(document.getElementById('pMaxAlertPrice').value) || 0,
    rule_preset: document.getElementById('pRulePreset').value,
    custom_rule: document.getElementById('pCustomRule').value.trim()
  };

  fetch('/api/products', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  })
  .then(r => {
    if (!r.ok) return r.text().then(t => { throw new Error(t); });
    return r.json();
  })
  .then(() => {
    closeProductModal();
    showToast(welcomeI18n[currentLang].toast_saved);
    setTimeout(() => { window.location.reload(); }, 800);
  })
  .catch(err => alert('Save failed: ' + err.message));
}

function deleteProduct(id, name) {
  if (id === 'oticon-connectclip') {
    alert('Oticon ConnectClip is the anchor product and cannot be deleted.');
    return;
  }
  const promptMsg = currentLang === 'lv'
    ? 'Vai tiešām vēlaties dzēst preci "' + name + '"?'
    : 'Are you sure you want to delete product "' + name + '"?';
  if (!confirm(promptMsg)) return;

  fetch('/api/products?id=' + encodeURIComponent(id), { method: 'DELETE' })
    .then(r => {
      if (!r.ok) return r.text().then(t => { throw new Error(t); });
      return r.json();
    })
    .then(() => {
      showToast(welcomeI18n[currentLang].toast_deleted);
      setTimeout(() => { window.location.reload(); }, 800);
    })
    .catch(err => alert('Delete failed: ' + err.message));
}

/* Import Modal */
function openImportModal() {
  document.getElementById('importFileInput').value = '';
  document.getElementById('importJSONText').value = '';
  document.getElementById('importModal').classList.add('open');
}

function closeImportModal() {
  document.getElementById('importModal').classList.remove('open');
}

function handleImportFile(event) {
  const file = event.target.files[0];
  if (!file) return;
  const reader = new FileReader();
  reader.onload = e => {
    document.getElementById('importJSONText').value = e.target.result;
  };
  reader.readAsText(file);
}

function submitImport(e) {
  e.preventDefault();
  const text = document.getElementById('importJSONText').value.trim();
  if (!text) {
    alert('Please choose a file or paste JSON content.');
    return;
  }

  fetch('/api/products/import', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: text
  })
  .then(r => {
    if (!r.ok) return r.text().then(t => { throw new Error(t); });
    return r.json();
  })
  .then(res => {
    closeImportModal();
    showToast(welcomeI18n[currentLang].toast_imported + ' (' + res.imported_count + ')');
    setTimeout(() => { window.location.reload(); }, 1000);
  })
  .catch(err => alert('Import failed: ' + err.message));
}

function initWelcome() {
  try {
    const savedLang = localStorage.getItem('cc_finder_lang');
    if (savedLang === 'lv' || savedLang === 'en') {
      currentLang = savedLang;
    } else if (navigator.language && navigator.language.startsWith('lv')) {
      currentLang = 'lv';
    }
  } catch (e) {}
  setWelcomeLang(currentLang);
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', initWelcome);
} else {
  initWelcome();
}
</script>
</body>
</html>`;

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>{{.TargetName}} Finder - Marketplace Monitor</title>
<style>
  :root {
    --bg: #0f172a;
    --card: #1e293b;
    --card-border: #334155;
    --text: #f8fafc;
    --text-muted: #94a3b8;
    --primary: #38bdf8;
    --primary-hover: #0284c7;
    --high: #22c55e;
    --medium: #f59e0b;
    --low: #64748b;
  }
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
    background-color: var(--bg);
    color: var(--text);
    padding: 24px 20px;
  }
  .container { max-width: 1440px; margin: 0 auto; }
  header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    flex-wrap: wrap;
    gap: 16px;
    margin-bottom: 24px;
    padding-bottom: 20px;
    border-bottom: 1px solid var(--card-border);
  }
  h1 { font-size: 1.6rem; font-weight: 700; color: #fff; display: flex; align-items: center; gap: 10px; }
  .badge-oticon {
    font-size: 0.75rem;
    padding: 4px 10px;
    background: #0284c7;
    border-radius: 9999px;
    font-weight: 600;
  }
  .actions { display: flex; gap: 12px; align-items: center; }
  .btn {
    background: var(--primary);
    color: #0f172a;
    border: none;
    padding: 8px 16px;
    border-radius: 6px;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.2s;
    text-decoration: none;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 0.9rem;
  }
  .btn:hover { background: var(--primary-hover); }
  .btn-sm { padding: 5px 12px; font-size: 0.82rem; }
  .btn-secondary { background: var(--card-border); color: #fff; }
  .btn-secondary:hover { background: #475569; }

  /* Language Switcher */
  .lang-switch {
    display: inline-flex;
    background: #0f172a;
    border: 1px solid var(--card-border);
    border-radius: 8px;
    padding: 3px;
    gap: 2px;
  }
  .lang-btn {
    background: transparent;
    border: none;
    color: var(--text-muted);
    padding: 6px 12px;
    border-radius: 6px;
    cursor: pointer;
    font-size: 0.82rem;
    font-weight: 600;
    transition: all 0.15s ease;
  }
  .lang-btn.active {
    background: #0284c7;
    color: #fff;
  }
  .lang-btn:hover:not(.active) {
    color: #fff;
    background: #1e293b;
  }

  /* Stats Grid */
  .stats-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
    gap: 16px;
    margin-bottom: 24px;
  }
  .stat-card {
    background: var(--card);
    border: 1px solid var(--card-border);
    border-radius: 8px;
    padding: 16px 20px;
  }
  .stat-label { font-size: 0.85rem; color: var(--text-muted); margin-bottom: 6px; text-transform: uppercase; letter-spacing: 0.5px; }
  .stat-val { font-size: 1.8rem; font-weight: 700; color: #fff; }
  .stat-val.high { color: var(--high); }

  /* Controls & Grouped Filters Panel */
  .table-controls {
    background: var(--card);
    border: 1px solid var(--card-border);
    padding: 16px 20px;
    border-radius: 10px;
    margin-bottom: 16px;
    display: flex;
    flex-direction: column;
    gap: 14px;
    box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.2);
  }
  .filters-top-bar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    flex-wrap: wrap;
    gap: 12px;
    padding-bottom: 12px;
    border-bottom: 1px solid rgba(51, 65, 85, 0.5);
  }
  .filters-top-title {
    font-size: 0.95rem;
    font-weight: 700;
    color: #fff;
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .filters-top-actions {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
  }
  .filters-grouped-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
    gap: 14px;
  }
  .filter-card {
    background: #141f33;
    border: 1px solid var(--card-border);
    border-radius: 8px;
    padding: 12px 14px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .filter-card-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: 0.85rem;
    font-weight: 600;
    color: #f1f5f9;
  }
  .filter-pill-badge {
    font-size: 0.75rem;
    padding: 2px 8px;
    border-radius: 9999px;
    background: rgba(56, 189, 248, 0.15);
    color: var(--primary);
    border: 1px solid rgba(56, 189, 248, 0.3);
    font-weight: 600;
  }
  .filter-pill-badge.high {
    background: rgba(34, 197, 94, 0.15);
    color: var(--high);
    border-color: rgba(34, 197, 94, 0.3);
  }
  .slider-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .slider-lbl {
    font-size: 0.8rem;
    color: var(--text-muted);
    min-width: 32px;
  }
  .slider-val-tag {
    font-size: 0.8rem;
    font-weight: 600;
    color: #fff;
    min-width: 44px;
    text-align: right;
    font-variant-numeric: tabular-nums;
  }
  .custom-slider {
    -webkit-appearance: none;
    appearance: none;
    width: 100%;
    height: 6px;
    background: #334155;
    border-radius: 9999px;
    outline: none;
    cursor: pointer;
  }
  .custom-slider::-webkit-slider-thumb {
    -webkit-appearance: none;
    appearance: none;
    width: 16px;
    height: 16px;
    border-radius: 50%;
    background: #38bdf8;
    border: 2px solid #0f172a;
    box-shadow: 0 0 6px rgba(56, 189, 248, 0.6);
    cursor: pointer;
    transition: transform 0.15s, background 0.15s;
  }
  .custom-slider::-webkit-slider-thumb:hover {
    transform: scale(1.2);
    background: #0284c7;
  }
  .custom-slider::-moz-range-thumb {
    width: 16px;
    height: 16px;
    border-radius: 50%;
    background: #38bdf8;
    border: 2px solid #0f172a;
    box-shadow: 0 0 6px rgba(56, 189, 248, 0.6);
    cursor: pointer;
  }
  .search-wrap {
    position: relative;
    width: 100%;
  }
  .search-input {
    background: #0f172a;
    border: 1px solid var(--card-border);
    color: #fff;
    padding: 8px 14px 8px 34px;
    border-radius: 6px;
    width: 100%;
    font-size: 0.9rem;
  }
  .search-icon {
    position: absolute;
    left: 11px;
    top: 50%;
    transform: translateY(-50%);
    color: var(--text-muted);
    font-size: 0.85rem;
    pointer-events: none;
  }
  .filter-select {
    background: #0f172a;
    border: 1px solid var(--card-border);
    color: #fff;
    padding: 7px 10px;
    border-radius: 6px;
    font-size: 0.85rem;
    cursor: pointer;
  }
  .filter-select:focus, .search-input:focus, .num-input:focus {
    outline: 1px solid var(--primary);
  }
  .price-inputs {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 0.85rem;
    color: var(--text-muted);
  }
  .num-input {
    background: #0f172a;
    border: 1px solid var(--card-border);
    color: #fff;
    padding: 6px 8px;
    border-radius: 6px;
    width: 76px;
    font-size: 0.85rem;
  }
  .checkbox-label {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 0.85rem;
    color: #cbd5e1;
    cursor: pointer;
    user-select: none;
  }
  .checkbox-label input { cursor: pointer; }
  .filter-group {
    display: flex;
    gap: 6px;
    align-items: center;
    flex-wrap: wrap;
    font-size: 0.85rem;
    color: var(--text-muted);
  }
  .filter-btn {
    background: transparent;
    border: 1px solid var(--card-border);
    color: var(--text-muted);
    padding: 4px 9px;
    border-radius: 6px;
    cursor: pointer;
    font-size: 0.8rem;
    transition: all 0.15s ease;
  }
  .filter-btn:hover { background: var(--card-border); color: #fff; }
  .filter-btn.active { background: #0284c7; color: #fff; border-color: #38bdf8; font-weight: 600; }
  .result-counter {
    font-size: 0.85rem;
    color: var(--text-muted);
  }
  .result-counter strong { color: #fff; }

  /* View Mode Switcher */
  .view-switch {
    display: inline-flex;
    background: #0f172a;
    border: 1px solid var(--card-border);
    border-radius: 8px;
    padding: 3px;
    gap: 2px;
  }
  .view-btn {
    background: transparent;
    border: none;
    color: var(--text-muted);
    padding: 6px 12px;
    border-radius: 6px;
    cursor: pointer;
    font-size: 0.82rem;
    font-weight: 600;
    transition: all 0.15s ease;
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .view-btn.active {
    background: #0284c7;
    color: #fff;
  }
  .view-btn:hover:not(.active) {
    color: #fff;
    background: #1e293b;
  }

  /* Table Container & Top/Bottom Navigation Bars */
  .table-container {
    background: var(--card);
    border: 1px solid var(--card-border);
    overflow-x: auto;
  }
  .pagination-bar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    flex-wrap: wrap;
    gap: 12px;
    padding: 12px 18px;
    background: #172033;
    border: 1px solid var(--card-border);
  }
  .pagination-bar.pagination-top {
    border-radius: 8px 8px 0 0;
    border-bottom: none;
  }
  .pagination-bar.pagination-bottom {
    border-radius: 0 0 8px 8px;
    border-top: none;
    margin-bottom: 24px;
  }
  .pagination-nav {
    display: flex;
    align-items: center;
    gap: 4px;
    flex-wrap: wrap;
  }
  .page-btn {
    background: #0f172a;
    border: 1px solid var(--card-border);
    color: var(--text);
    padding: 6px 11px;
    border-radius: 6px;
    font-size: 0.82rem;
    font-weight: 600;
    cursor: pointer;
    transition: all 0.15s ease;
    min-width: 34px;
    text-align: center;
  }
  .page-btn:hover:not(:disabled):not(.active) {
    background: #1e293b;
    border-color: var(--primary);
    color: var(--primary);
  }
  .page-btn.active {
    background: #0284c7;
    border-color: #38bdf8;
    color: #fff;
  }
  .page-btn:disabled {
    opacity: 0.35;
    cursor: not-allowed;
  }
  .page-ellipsis {
    color: var(--text-muted);
    padding: 0 4px;
    font-size: 0.85rem;
  }

  /* Infinite Scroll Bar */
  .infinite-bar {
    text-align: center;
    padding: 20px 20px;
    background: #172033;
    border: 1px solid var(--card-border);
    border-top: none;
    border-radius: 0 0 8px 8px;
    margin-bottom: 24px;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 10px;
  }
  .infinite-status {
    font-size: 0.88rem;
    color: var(--text-muted);
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .infinite-spinner {
    display: inline-block;
    width: 18px;
    height: 18px;
    border: 2px solid rgba(56, 189, 248, 0.2);
    border-top-color: var(--primary);
    border-radius: 50%;
    animation: spin 0.75s linear infinite;
  }
  @keyframes spin {
    to { transform: rotate(360deg); }
  }
  table {
    width: 100%;
    border-collapse: collapse;
    text-align: left;
    font-size: 0.9rem;
  }
  th {
    background: #172033;
    color: var(--text-muted);
    font-weight: 600;
    padding: 12px 16px;
    border-bottom: 1px solid var(--card-border);
    white-space: nowrap;
  }
  th.sortable {
    cursor: pointer;
    user-select: none;
    transition: background 0.15s, color 0.15s;
  }
  th.sortable:hover {
    background: #1e293b;
    color: #fff;
  }
  th.sortable.active {
    color: var(--primary);
    background: #152438;
  }
  th.sortable .sort-icon {
    display: inline-block;
    margin-left: 5px;
    font-size: 0.75rem;
    color: var(--text-muted);
    width: 12px;
    text-align: center;
  }
  th.sortable.active .sort-icon {
    color: var(--primary);
    font-weight: bold;
  }
  td {
    padding: 14px 16px;
    border-bottom: 1px solid #283548;
    vertical-align: middle;
  }
  tr:hover td { background: #222f44; }

  .thumb {
    width: 52px;
    height: 52px;
    object-fit: cover;
    border-radius: 6px;
    background: #0f172a;
    border: 1px solid var(--card-border);
  }
  .no-img {
    width: 52px;
    height: 52px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: #0f172a;
    border: 1px solid var(--card-border);
    border-radius: 6px;
    font-size: 0.75rem;
    color: var(--text-muted);
  }
  .item-title {
    font-weight: 600;
    color: #fff;
    text-decoration: none;
    display: block;
    max-width: 380px;
  }
  .item-title:hover { color: var(--primary); text-decoration: underline; }
  .item-desc { font-size: 0.78rem; color: var(--text-muted); margin-top: 4px; max-width: 380px; }
  .price { font-weight: 700; color: #fff; font-size: 1rem; }
  .source-tag {
    display: inline-block;
    padding: 3px 8px;
    border-radius: 4px;
    font-size: 0.75rem;
    font-weight: 600;
    text-transform: uppercase;
    background: #334155;
    color: #e2e8f0;
  }
  .score-badge {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 4px 10px;
    border-radius: 6px;
    font-weight: 700;
    font-size: 0.85rem;
  }
  .score-high { background: rgba(34, 197, 94, 0.2); color: #4ade80; border: 1px solid rgba(34, 197, 94, 0.4); }
  .score-medium { background: rgba(245, 158, 11, 0.2); color: #fbbf24; border: 1px solid rgba(245, 158, 11, 0.4); }
  .score-low { background: rgba(100, 116, 139, 0.2); color: #94a3b8; border: 1px solid rgba(100, 116, 139, 0.4); }

  .reasons { font-size: 0.75rem; color: #cbd5e1; max-width: 280px; line-height: 1.4; }
  .time-cell { font-size: 0.8rem; color: var(--text-muted); white-space: nowrap; }
  .notified-tag {
    font-size: 0.75rem;
    padding: 2px 6px;
    border-radius: 4px;
    background: #14532d;
    color: #86efac;
    display: inline-block;
    white-space: nowrap;
  }
</style>
</head>
<body>
<div class="container">
  <div style="display: flex; align-items: center; justify-content: space-between; margin-bottom: 16px; flex-wrap: wrap; gap: 10px;">
    <a href="/" class="btn btn-secondary btn-sm" id="btnBackToProducts" style="font-size: 0.85rem; padding: 6px 14px;">
      <span>←</span> <span data-i18n="back_to_products">All Products</span>
    </a>
    <div style="display: flex; align-items: center; gap: 10px; font-size: 0.88rem; color: var(--text-muted);">
      <span style="font-size: 1.3rem;">{{.ProductIcon}}</span>
      <span style="font-weight: 700; color: #fff; font-size: 1rem;">{{.TargetName}}</span>
      {{if .Category}}<span class="badge-oticon" style="font-size: 0.72rem; padding: 2px 8px;">{{.Category}}</span>{{end}}
      {{if gt .TargetMinPrice 0.0}}<span style="color: var(--primary); font-weight: 600;">Target: €{{printf "%.0f" .TargetMinPrice}} – €{{printf "%.0f" .TargetMaxPrice}}</span>{{end}}
    </div>
  </div>
  <header>
    <div>
      <h1><span>{{.ProductIcon}}</span> {{.TargetName}} Finder <span class="badge-oticon" data-i18n="badge_oticon">Marketplace Monitor</span></h1>
      <p style="font-size: 0.85rem; color: var(--text-muted); margin-top: 4px;" id="subtitleText">Tracking Latvian marketplaces for {{.TargetName}} • Updated {{.LastUpdated}}</p>
    </div>
    <div class="actions">
      <!-- Language Switcher -->
      <div class="lang-switch">
        <button id="lang-btn-lv" class="lang-btn" onclick="setLanguage('lv')">🇱🇻 Latviešu</button>
        <button id="lang-btn-en" class="lang-btn active" onclick="setLanguage('en')">🇬🇧 English</button>
      </div>

      <button class="btn" id="btn-scan" onclick="triggerScan()"><span data-i18n="btn_scan">🔄 Trigger Scan Now</span></button>
      <a href="/?product={{.ProductID}}" class="btn btn-secondary" data-i18n="btn_refresh">Refresh</a>
    </div>
  </header>

  <div class="stats-grid">
    <div class="stat-card">
      <div class="stat-label" data-i18n="stat_total">Total Listings Tracked</div>
      <div class="stat-val">{{.TotalListings}}</div>
    </div>
    <div class="stat-card">
      <div class="stat-label" data-i18n="stat_candidates">Promising Candidates (&ge;{{.MinAlertScore}}%)</div>
      <div class="stat-val high">{{.Candidates}}</div>
    </div>
    <div class="stat-card">
      <div class="stat-label" data-i18n="stat_notified">Telegram Alerts Dispatched</div>
      <div class="stat-val">{{.Notified}}</div>
    </div>
    <div class="stat-card">
      <div class="stat-label" data-i18n="stat_threshold">Alert Threshold</div>
      <div class="stat-val">{{.MinAlertScore}}%</div>
    </div>
  </div>

  <div class="table-controls">
    <!-- Top Bar with Title, Reset, and Live Counter -->
    <div class="filters-top-bar">
      <div class="filters-top-title">
        <span>🎛️</span>
        <span data-i18n="filter_panel_title">Filters & Controls</span>
      </div>
      <div class="filters-top-actions">
        <button class="btn btn-secondary btn-sm" onclick="resetFilters()">
          <span data-i18n="btn_clear">✕ Clear</span>
        </button>
        <div class="result-counter" id="visibleCounterWrapper">
          Showing <strong id="visibleCount">{{len .Listings}}</strong> of {{.MatchedCount}} listings
        </div>
      </div>
    </div>

    <!-- Grouped Filter Cards Grid -->
    <div class="filters-grouped-grid">
      <!-- Card 1: Search & Sources -->
      <div class="filter-card">
        <div class="filter-card-header">
          <span>🔍 <span data-i18n="filter_search_title">Search & Sources</span></span>
        </div>
        <div class="search-wrap">
          <span class="search-icon">🔍</span>
          <input type="text" id="searchInput" class="search-input" data-i18n-placeholder="search_placeholder" placeholder="Search title, description, location, signals..." oninput="onSearchInput()">
        </div>
        <div style="display: flex; gap: 8px; flex-wrap: wrap;">
          <select id="sourceFilter" class="filter-select" style="flex: 1; min-width: 120px;" onchange="applyFilters()">
            <option value="" data-i18n="source_all">All Marketplaces</option>
            {{range .Sources}}
              <option value="{{.}}">{{.}}</option>
            {{end}}
          </select>
          <select id="statusFilter" class="filter-select" style="flex: 1; min-width: 120px;" onchange="applyFilters()">
            <option value="" data-i18n="status_all">All Alert Statuses</option>
            <option value="alerted" data-i18n="status_alerted">🚨 Alerted Only</option>
            <option value="unalerted" data-i18n="status_unalerted">Not Yet Alerted</option>
          </select>
        </div>
        <div>
          <label class="checkbox-label">
            <input type="checkbox" id="photoFilter" onchange="applyFilters()">
            <span data-i18n="label_with_photo">📷 With photo</span>
          </label>
        </div>
      </div>

      <!-- Card 2: Price Range & Dual Sliders -->
      <div class="filter-card">
        <div class="filter-card-header">
          <span>💶 <span data-i18n="filter_price_title">Price Range & Slider</span></span>
          <span id="priceRangeDisplay" class="filter-pill-badge">All prices</span>
        </div>
        <div class="price-inputs">
          <span data-i18n="label_price">Price:</span>
          <input type="number" id="minPrice" class="num-input" data-i18n-placeholder="price_min" placeholder="Min €" min="0" oninput="onPriceInput()">
          <span>–</span>
          <input type="number" id="maxPrice" class="num-input" data-i18n-placeholder="price_max" placeholder="Max €" min="0" oninput="onPriceInput()">
        </div>
        <!-- Dual Price Sliders -->
        <div style="display: flex; flex-direction: column; gap: 5px;">
          <div class="slider-row">
            <span class="slider-lbl" data-i18n="slider_price_min">Min:</span>
            <input type="range" id="priceMinSlider" class="custom-slider" min="0" max="500" step="5" value="0" oninput="onPriceSliderChange('min')">
            <span id="priceMinVal" class="slider-val-tag">0€</span>
          </div>
          <div class="slider-row">
            <span class="slider-lbl" data-i18n="slider_price_max">Max:</span>
            <input type="range" id="priceMaxSlider" class="custom-slider" min="0" max="500" step="5" value="500" oninput="onPriceSliderChange('max')">
            <span id="priceMaxVal" class="slider-val-tag">500€+</span>
          </div>
        </div>
        <!-- Price Presets -->
        <div class="filter-group">
          <button id="priceBtnAll" class="filter-btn price-btn active" onclick="setPricePreset('all', this)" data-i18n="price_all">All</button>
          <button id="priceBtnUnder50" class="filter-btn price-btn" onclick="setPricePreset('under50', this)" data-i18n="price_under50">&lt; €50</button>
          <button id="priceBtnTarget" class="filter-btn price-btn" onclick="setPricePreset('50to150', this)" data-i18n="price_target">€50 – €150 (Target)</button>
          <button id="priceBtnOver150" class="filter-btn price-btn" onclick="setPricePreset('over150', this)" data-i18n="price_over150">&gt; €150</button>
        </div>
      </div>

      <!-- Card 3: Match Score Threshold & Slider -->
      <div class="filter-card">
        <div class="filter-card-header">
          <span>🎯 <span data-i18n="filter_score_title">Match Score Threshold</span></span>
          <span id="scoreSliderBadge" class="filter-pill-badge high">≥ 0%</span>
        </div>
        <!-- Score Slider -->
        <div class="slider-row">
          <span class="slider-lbl" data-i18n="slider_score_label">Threshold:</span>
          <input type="range" id="scoreSlider" class="custom-slider" min="0" max="100" step="5" value="0" oninput="onScoreSliderChange()">
          <span id="scoreSliderValText" class="slider-val-tag">0%</span>
        </div>
        <!-- Score Presets -->
        <div class="filter-group">
          <button id="scoreBtnAll" class="filter-btn score-btn active" onclick="setScorePreset('all', this)" data-i18n="score_all">All</button>
          <button id="scoreBtnCandidates" class="filter-btn score-btn" onclick="setScorePreset('candidates', this)" data-i18n="score_candidates">Candidates (&ge;{{.MinAlertScore}}%)</button>
          <button id="scoreBtnMed" class="filter-btn score-btn" onclick="setScorePreset('med', this)" data-i18n="score_med">Medium+ (&ge;50%)</button>
          <button id="scoreBtnHigh" class="filter-btn score-btn" onclick="setScorePreset('high', this)" data-i18n="score_high">High (&ge;75%)</button>
          <button id="scoreBtnLow" class="filter-btn score-btn" onclick="setScorePreset('low', this)" data-i18n="score_low">Low (&lt;50%)</button>
        </div>
      </div>

      <!-- Card 4: Display & View Mode -->
      <div class="filter-card">
        <div class="filter-card-header">
          <span>⚙️ <span data-i18n="filter_view_title">View Mode & Navigation</span></span>
        </div>
        <!-- View Mode Segmented Switch -->
        <div class="view-switch" style="width: 100%;">
          <button id="viewModePagination" class="view-btn active" style="flex: 1; justify-content: center;" onclick="setViewMode('pagination')">
            <span data-i18n="view_pagination">📄 Pagination</span>
          </button>
          <button id="viewModeInfinite" class="view-btn" style="flex: 1; justify-content: center;" onclick="setViewMode('infinite')">
            <span data-i18n="view_infinite">♾️ Infinite Scroll</span>
          </button>
        </div>
        <!-- Page Size Selector -->
        <div style="display: flex; justify-content: space-between; align-items: center; gap: 8px;">
          <span style="font-size: 0.85rem; color: var(--text-muted);" data-i18n="label_per_page">Per page:</span>
          <select id="pageSizeSelect" class="filter-select" style="padding: 5px 10px;" onchange="onPageSizeChange()">
            <option value="25">25</option>
            <option value="50" selected>50</option>
            <option value="100">100</option>
            <option value="200">200</option>
          </select>
        </div>
        <div id="viewModeInfo" style="font-size: 0.82rem; color: var(--text-muted); margin-top: auto;"></div>
      </div>
    </div>
  </div>

  <!-- Pagination Top Bar -->
  <div id="paginationTopContainer" class="pagination-bar pagination-top">
    <div id="paginationTopInfo" style="font-size: 0.85rem; color: var(--text-muted);"></div>
    <div id="paginationTopNav" class="pagination-nav"></div>
  </div>

  <div class="table-container">
    <table id="listingsTable">
      <thead>
        <tr>
          <th style="width: 58px;" data-i18n="th_photo">Photo</th>
          <th class="sortable active" data-col="score" onclick="handleSort('score', this)" title="Click to sort">
            <span data-i18n="th_score">Score</span> <span class="sort-icon">▼</span>
          </th>
          <th class="sortable" data-col="title" onclick="handleSort('title', this)" title="Click to sort">
            <span data-i18n="th_title">Title & Details</span> <span class="sort-icon">↕</span>
          </th>
          <th class="sortable" data-col="source" onclick="handleSort('source', this)" title="Click to sort">
            <span data-i18n="th_source">Source</span> <span class="sort-icon">↕</span>
          </th>
          <th class="sortable" data-col="price" onclick="handleSort('price', this)" title="Click to sort">
            <span data-i18n="th_price">Price</span> <span class="sort-icon">↕</span>
          </th>
          <th class="sortable" data-col="location" onclick="handleSort('location', this)" title="Click to sort">
            <span data-i18n="th_location">Location</span> <span class="sort-icon">↕</span>
          </th>
          <th class="sortable" data-col="signals" onclick="handleSort('signals', this)" title="Click to sort">
            <span data-i18n="th_signals">Signals</span> <span class="sort-icon">↕</span>
          </th>
          <th class="sortable" data-col="time" onclick="handleSort('time', this)" title="Click to sort">
            <span data-i18n="th_time">Last Seen</span> <span class="sort-icon">↕</span>
          </th>
          <th class="sortable" data-col="status" onclick="handleSort('status', this)" title="Click to sort">
            <span data-i18n="th_status">Status</span> <span class="sort-icon">↕</span>
          </th>
        </tr>
      </thead>
      <tbody>
        {{range .Listings}}
        <tr class="listing-row"
            data-score="{{.Score}}"
            data-title="{{.Title}}"
            data-source="{{.Source}}"
            data-price="{{.Price}}"
            data-location="{{if .Location}}{{.Location}}{{else}}Latvija{{end}}"
            data-signals="{{len .MatchReasons}}"
            data-time="{{.LastSeenAt.Unix}}"
            data-status="{{if .Notified}}1{{else}}0{{end}}"
            data-photo="{{if .PrimaryImage}}1{{else}}0{{end}}">
          <td>
            {{if .PrimaryImage}}
              <img src="{{.PrimaryImage}}" alt="" class="thumb" onerror="this.style.display='none'">
            {{else}}
              <div class="no-img" data-i18n="badge_no_img">No img</div>
            {{end}}
          </td>
          <td>
            {{if ge .Score 75}}
              <span class="score-badge score-high">{{.Score}}% HIGH</span>
            {{else if ge .Score 50}}
              <span class="score-badge score-medium">{{.Score}}% MED</span>
            {{else}}
              <span class="score-badge score-low">{{.Score}}% LOW</span>
            {{end}}
          </td>
          <td>
            <a href="{{.URL}}" target="_blank" rel="noopener noreferrer" class="item-title">{{.Title}}</a>
            {{if .Description}}
              <div class="item-desc">{{.Description}}</div>
            {{end}}
          </td>
          <td><span class="source-tag">{{.Source}}</span></td>
          <td>
            <div class="price">
              {{if gt .Price 0.0}}
                €{{printf "%.2f" .Price}}
              {{else}}
                <span style="color: var(--text-muted); font-size: 0.85rem;">—</span>
              {{end}}
            </div>
          </td>
          <td style="color: var(--text-muted); font-size: 0.85rem;">{{if .Location}}{{.Location}}{{else}}Latvija{{end}}</td>
          <td>
            <div class="reasons">
              {{range .MatchReasons}}
                <div>• {{.}}</div>
              {{end}}
            </div>
          </td>
          <td class="time-cell">{{.LastSeenAt.Format "02.01 15:04"}}</td>
          <td>
            {{if .Notified}}
              <span class="notified-tag" data-i18n="tag_alerted">🚨 Alerted</span>
            {{else}}
              <span style="color: var(--text-muted); font-size: 0.75rem;">—</span>
            {{end}}
          </td>
        </tr>
        {{end}}

        <tr id="noResultsRow" style="display: none;">
          <td colspan="9" style="text-align: center; padding: 48px 20px; color: var(--text-muted);">
            <div style="font-size: 1.05rem; margin-bottom: 10px;" data-i18n="no_results_title">🔍 No listings match the current filters</div>
            <button class="btn btn-secondary btn-sm" onclick="resetFilters()" data-i18n="no_results_btn">Reset All Filters</button>
          </td>
        </tr>

        {{if eq (len .Listings) 0}}
        <tr>
          <td colspan="9" style="text-align: center; padding: 48px 20px; color: var(--text-muted);" data-i18n="empty_listings">
            No listings tracked yet. Click "Trigger Scan Now" to run an initial marketplace scan.
          </td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </div>

  <!-- Pagination Bottom Bar -->
  <div id="paginationContainer" class="pagination-bar">
    <div id="paginationInfo" style="font-size: 0.85rem; color: var(--text-muted);"></div>
    <div id="paginationNav" class="pagination-nav"></div>
  </div>

  <!-- Infinite Scroll Bottom Bar -->
  <div id="infiniteContainer" class="infinite-bar" style="display: none;">
    <div id="infiniteSentinel" style="height: 1px; width: 100%;"></div>
    <div id="infiniteStatus" class="infinite-status">
      <span class="infinite-spinner" id="infiniteSpinner" style="display: none;"></span>
      <span id="infiniteStatusText" data-i18n="loading_more">Loading more listings...</span>
    </div>
    <button id="btnLoadMore" class="btn btn-secondary btn-sm" onclick="loadMoreInfinite()" data-i18n="load_more_btn">
      Load More
    </button>
  </div>
</div>

<script>
const minAlertThreshold = {{.MinAlertScore}};
const lastUpdatedTime = "{{.LastUpdated}}";
const totalListingCount = {{len .Listings}};

// -------------------------------------------------------------
// Translations (English & Latvian)
// -------------------------------------------------------------
const i18n = {
  en: {
    badge_oticon: "Oticon Monitor",
    btn_scan: "🔄 Trigger Scan Now",
    btn_scanning: "⏳ Scanning...",
    btn_refresh: "Refresh",
    stat_total: "Total Listings Tracked",
    stat_candidates: "Promising Candidates (≥" + minAlertThreshold + "%)",
    stat_notified: "Telegram Alerts Dispatched",
    stat_threshold: "Alert Threshold",
    filter_panel_title: "Filters & Controls",
    filter_search_title: "Search & Sources",
    filter_price_title: "Price Range & Slider",
    filter_score_title: "Match Score Threshold",
    filter_view_title: "View Mode & Navigation",
    slider_score_label: "Threshold:",
    slider_price_min: "Min:",
    slider_price_max: "Max:",
    search_placeholder: "Search title, description, location, signals...",
    source_all: "All Marketplaces",
    status_all: "All Alert Statuses",
    status_alerted: "🚨 Alerted Only",
    status_unalerted: "Not Yet Alerted",
    label_price: "Price:",
    price_min: "Min €",
    price_max: "Max €",
    label_with_photo: "📷 With photo",
    btn_clear: "✕ Clear",
    label_score: "Score:",
    score_all: "All",
    score_candidates: "Candidates (≥" + minAlertThreshold + "%)",
    score_med: "Medium+ (≥50%)",
    score_high: "High (≥75%)",
    score_low: "Low (<50%)",
    label_price_range: "Price Range:",
    price_all: "All",
    price_under50: "< €50",
    price_target: "€50 – €150 (Target)",
    price_over150: "> €150",
    view_pagination: "📄 Pagination",
    view_infinite: "♾️ Infinite Scroll",
    label_per_page: "Per page:",
    page_first: "« First",
    page_prev: "‹ Prev",
    page_next: "Next ›",
    page_last: "Last »",
    loading_more: "Loading more listings...",
    all_loaded: "✓ All listings displayed",
    load_more_btn: "Load More",
    th_photo: "Photo",
    th_score: "Score",
    th_title: "Title & Details",
    th_source: "Source",
    th_price: "Price",
    th_location: "Location",
    th_signals: "Signals",
    th_time: "Last Seen",
    th_status: "Status",
    no_results_title: "🔍 No listings match the current filters",
    no_results_btn: "Reset All Filters",
    empty_listings: "No listings tracked yet. Click \"Trigger Scan Now\" to run an initial marketplace scan.",
    badge_no_img: "No img",
    tag_alerted: "🚨 Alerted",
    subtitle_prefix: "Tracking Latvian marketplaces for lost Oticon ConnectClip • Updated ",
    alert_scan_started: "Scan initiated! The dashboard will auto-refresh in 8 seconds.",
    back_to_products: "All Products",
    records_badge: "Records"
  },
  lv: {
    badge_oticon: "Oticon monitors",
    btn_scan: "🔄 Sākt meklēšanu tagad",
    btn_scanning: "⏳ Notiek meklēšana...",
    btn_refresh: "Atjaunot",
    stat_total: "Kopā atrasti sludinājumi",
    stat_candidates: "Iespējamie kandidāti (≥" + minAlertThreshold + "%)",
    stat_notified: "Nosūtītie Telegram paziņojumi",
    stat_threshold: "Paziņojumu slieksnis",
    filter_panel_title: "Filtri un vadība",
    filter_search_title: "Meklēšana un portāli",
    filter_price_title: "Cenas diapazons un slaideris",
    filter_score_title: "Atbilstības slieksnis",
    filter_view_title: "Skata režīms un lapošana",
    slider_score_label: "Slieksnis:",
    slider_price_min: "Min:",
    slider_price_max: "Max:",
    search_placeholder: "Meklēt pēc nosaukuma, apraksta, vietas, pazīmēm...",
    source_all: "Visi portāli",
    status_all: "Visi paziņojumu statusi",
    status_alerted: "🚨 Tikai paziņotie",
    status_unalerted: "Nav paziņots",
    label_price: "Cena:",
    price_min: "No €",
    price_max: "Līdz €",
    label_with_photo: "📷 Tikai ar foto",
    btn_clear: "✕ Notīrīt",
    label_score: "Atbilstība:",
    score_all: "Visi",
    score_candidates: "Kandidāti (≥" + minAlertThreshold + "%)",
    score_med: "Vidēja+ (≥50%)",
    score_high: "Augsta (≥75%)",
    score_low: "Zema (<50%)",
    label_price_range: "Cenas diapazons:",
    price_all: "Visas",
    price_under50: "< 50 €",
    price_target: "50 – 150 € (Mērķis)",
    price_over150: "> 150 €",
    view_pagination: "📄 Lapošana",
    view_infinite: "♾️ Bezgalīgā ritināšana",
    label_per_page: "Lapas izmērs:",
    page_first: "« Pirmā",
    page_prev: "‹ Iepriekšējā",
    page_next: "Nākamā ›",
    page_last: "Pēdējā »",
    loading_more: "Ielādē vairāk sludinājumu...",
    all_loaded: "✓ Visi sludinājumi parādīti",
    load_more_btn: "Ielādēt vairāk",
    th_photo: "Foto",
    th_score: "Atbilstība",
    th_title: "Nosaukums un apraksts",
    th_source: "Portāls",
    th_price: "Cena",
    th_location: "Atrašanās vieta",
    th_signals: "Pazīmes",
    th_time: "Pēdējo reizi redzēts",
    th_status: "Statuss",
    no_results_title: "🔍 Neviens sludinājums neatbilst atlasītajiem filtriem",
    no_results_btn: "Atiestatīt visus filtrus",
    empty_listings: "Pagaidām nav atrasts neviens sludinājums. Nospiediet \"Sākt meklēšanu tagad\", lai veiktu meklēšanu.",
    badge_no_img: "Nav foto",
    tag_alerted: "🚨 Paziņots",
    subtitle_prefix: "Meklē nozaudēto Oticon ConnectClip Latvijas sludinājumu portālos • Atjaunots ",
    alert_scan_started: "Meklēšana sākta! Lapa tiks atjaunota pēc 8 sekundēm.",
    back_to_products: "Visi produkti",
    records_badge: "Ieraksti"
  }
};

let currentLang = 'en';

// State
let currentViewMode = 'pagination'; // 'pagination' or 'infinite'
let currentPage = {{.Page}};
let pageSize = {{.PageSize}};
let totalMatched = {{.MatchedCount}};
let totalPages = {{.TotalPages}};
let currentSortCol = 'score';
let currentSortDir = 'desc';
let currentScorePreset = 'all';
let isFetching = false;
let currentAbortController = null;
let searchDebounceTimer = null;

function escapeHTML(str) {
  if (!str) return '';
  return String(str).replace(/[&<>'"]/g, tag => ({
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    "'": '&#39;',
    '"': '&quot;'
  }[tag] || tag));
}

function formatDate(isoStr) {
  if (!isoStr) return '';
  const d = new Date(isoStr);
  if (isNaN(d.getTime())) return '';
  const pad = function(n) { return String(n).padStart(2, '0'); };
  return pad(d.getDate()) + '.' + pad(d.getMonth() + 1) + ' ' + pad(d.getHours()) + ':' + pad(d.getMinutes());
}

function buildRowHTML(l) {
  const isHigh = l.score >= 75;
  const isMed = l.score >= 50 && l.score < 75;
  const scoreClass = isHigh ? 'score-high' : (isMed ? 'score-medium' : 'score-low');
  let scoreBadge = '';
  if (currentLang === 'lv') {
    scoreBadge = '<span class="score-badge ' + scoreClass + '">' + l.score + '% ' + (isHigh ? 'AUGSTA' : isMed ? 'VIDĒJA' : 'ZEMA') + '</span>';
  } else {
    scoreBadge = '<span class="score-badge ' + scoreClass + '">' + l.score + '% ' + (isHigh ? 'HIGH' : isMed ? 'MED' : 'LOW') + '</span>';
  }

  const primaryImage = (l.image_urls && l.image_urls.length > 0) ? l.image_urls[0] : '';
  const photoCell = primaryImage
    ? '<img src="' + escapeHTML(primaryImage) + '" alt="" class="thumb" onerror="this.style.display=\'none\'">'
    : '<div class="no-img">' + (currentLang === 'lv' ? 'Nav foto' : 'No img') + '</div>';

  const priceFormatted = l.price > 0 ? ('€' + l.price.toFixed(2)) : '<span style="color: var(--text-muted); font-size: 0.85rem;">—</span>';
  const descHTML = l.description ? '<div class="item-desc">' + escapeHTML(l.description) + '</div>' : '';
  const locText = escapeHTML(l.location || 'Latvija');

  let reasonsHTML = '';
  if (l.match_reasons && l.match_reasons.length > 0) {
    reasonsHTML = l.match_reasons.map(function(r) { return '<div>• ' + escapeHTML(r) + '</div>'; }).join('');
  }

  const statusHTML = l.notified
    ? '<span class="notified-tag">' + (currentLang === 'lv' ? '🚨 Paziņots' : '🚨 Alerted') + '</span>'
    : '<span style="color: var(--text-muted); font-size: 0.75rem;">—</span>';

  return '<tr class="listing-row" ' +
    'data-score="' + l.score + '" ' +
    'data-title="' + escapeHTML(l.title) + '" ' +
    'data-source="' + escapeHTML(l.source) + '" ' +
    'data-price="' + l.price + '" ' +
    'data-location="' + locText + '" ' +
    'data-signals="' + (l.match_reasons ? l.match_reasons.length : 0) + '" ' +
    'data-time="' + (l.last_seen_at ? new Date(l.last_seen_at).getTime() / 1000 : 0) + '" ' +
    'data-status="' + (l.notified ? '1' : '0') + '" ' +
    'data-photo="' + (primaryImage ? '1' : '0') + '">' +
    '<td>' + photoCell + '</td>' +
    '<td>' + scoreBadge + '</td>' +
    '<td>' +
      '<a href="' + escapeHTML(l.url) + '" target="_blank" rel="noopener noreferrer" class="item-title">' + escapeHTML(l.title) + '</a>' +
      descHTML +
    '</td>' +
    '<td><span class="source-tag">' + escapeHTML(l.source) + '</span></td>' +
    '<td><div class="price">' + priceFormatted + '</div></td>' +
    '<td style="color: var(--text-muted); font-size: 0.85rem;">' + locText + '</td>' +
    '<td><div class="reasons">' + reasonsHTML + '</div></td>' +
    '<td class="time-cell">' + formatDate(l.last_seen_at) + '</td>' +
    '<td>' + statusHTML + '</td>' +
  '</tr>';
}

function fetchPage(targetPage, appendRows) {
  if (typeof appendRows === 'undefined') appendRows = false;
  if (isFetching && appendRows) return;
  if (currentAbortController && !appendRows) {
    currentAbortController.abort();
  }
  currentAbortController = new AbortController();

  isFetching = true;
  const spinner = document.getElementById('infiniteSpinner');
  const tableEl = document.getElementById('listingsTable');

  if (appendRows) {
    if (spinner) spinner.style.display = 'inline-block';
  } else {
    if (tableEl) tableEl.style.opacity = '0.5';
  }

  const query = (document.getElementById('searchInput').value || '').trim();
  const source = document.getElementById('sourceFilter').value || '';
  const status = document.getElementById('statusFilter').value || '';
  const photo = document.getElementById('photoFilter').checked ? '1' : '';
  const minPrice = document.getElementById('minPrice').value || '';
  const maxPrice = document.getElementById('maxPrice').value || '';

  let minScore = '';
  let maxScore = '';
  const scoreSliderEl = document.getElementById('scoreSlider');
  const sliderScore = scoreSliderEl ? (parseInt(scoreSliderEl.value, 10) || 0) : 0;

  if (currentScorePreset === 'low') {
    maxScore = '49';
  } else if (sliderScore > 0) {
    minScore = String(sliderScore);
  } else if (currentScorePreset === 'candidates') {
    minScore = String(minAlertThreshold);
  } else if (currentScorePreset === 'med') {
    minScore = '50';
  } else if (currentScorePreset === 'high') {
    minScore = '75';
  }

  const params = new URLSearchParams({
    paged: 'true',
    product: '{{.ProductID}}',
    page: String(targetPage),
    limit: String(pageSize),
    sort: currentSortCol,
    dir: currentSortDir
  });
  if (query) params.set('q', query);
  if (source) params.set('source', source);
  if (status) params.set('status', status);
  if (photo) params.set('photo', photo);
  if (minPrice) params.set('min_price', minPrice);
  if (maxPrice) params.set('max_price', maxPrice);
  if (minScore) params.set('min_score', minScore);
  if (maxScore) params.set('max_score', maxScore);

  fetch('/api/listings?' + params.toString(), { signal: currentAbortController.signal })
    .then(function(r) { return r.json(); })
    .then(function(data) {
      isFetching = false;
      if (tableEl) tableEl.style.opacity = '1';
      if (spinner) spinner.style.display = 'none';

      const tbody = document.querySelector('#listingsTable tbody');
      const noResultsRow = document.getElementById('noResultsRow');

      currentPage = data.page;
      totalMatched = data.total;
      totalPages = data.total_pages;

      if (!appendRows) {
        tbody.querySelectorAll('tr.listing-row').forEach(function(r) { r.remove(); });
      }

      if (data.items && data.items.length > 0) {
        if (noResultsRow) noResultsRow.style.display = 'none';
        const rowsHTML = data.items.map(buildRowHTML).join('');
        if (noResultsRow) {
          noResultsRow.insertAdjacentHTML('beforebegin', rowsHTML);
        } else {
          tbody.insertAdjacentHTML('beforeend', rowsHTML);
        }
      } else if (!appendRows) {
        if (noResultsRow) noResultsRow.style.display = '';
      }

      renderUI();

      if (!appendRows && targetPage > 1) {
        tableEl.scrollIntoView({ behavior: 'smooth', block: 'start' });
      }
    })
    .catch(function(err) {
      if (err.name === 'AbortError') return;
      isFetching = false;
      if (tableEl) tableEl.style.opacity = '1';
      if (spinner) spinner.style.display = 'none';
      console.error('Fetch error:', err);
    });
}

function renderUI() {
  const dict = i18n[currentLang];
  const paginationTopContainer = document.getElementById('paginationTopContainer');
  const paginationContainer = document.getElementById('paginationContainer');
  const infiniteContainer = document.getElementById('infiniteContainer');
  const counterWrapper = document.getElementById('visibleCounterWrapper');
  const viewModeInfo = document.getElementById('viewModeInfo');

  const currentlyLoadedCount = document.querySelectorAll('#listingsTable tbody tr.listing-row').length;

  if (currentViewMode === 'pagination') {
    if (paginationTopContainer) paginationTopContainer.style.display = 'flex';
    if (paginationContainer) paginationContainer.style.display = 'flex';
    if (infiniteContainer) infiniteContainer.style.display = 'none';

    renderPaginationNav(currentPage, totalPages, 'paginationTopNav');
    renderPaginationNav(currentPage, totalPages, 'paginationNav');

    const startIndex = (currentPage - 1) * pageSize;
    const endIndex = Math.min(startIndex + currentlyLoadedCount, totalMatched);
    const dispRange = totalMatched > 0 ? (startIndex + 1) + '–' + endIndex : '0';

    const paginationTopInfo = document.getElementById('paginationTopInfo');
    const paginationInfo = document.getElementById('paginationInfo');
    let infoHTML = '';
    if (currentLang === 'lv') {
      infoHTML = 'Rāda <strong>' + dispRange + '</strong> no ' + totalMatched + ' sludinājumiem (' + currentPage + '. no ' + totalPages + ' lapām)';
      if (counterWrapper) counterWrapper.innerHTML = 'Rāda <strong id="visibleCount">' + dispRange + '</strong> no ' + totalMatched + ' sludinājumiem';
      if (viewModeInfo) viewModeInfo.innerText = currentPage + '. no ' + totalPages + ' lapām';
    } else {
      infoHTML = 'Showing <strong>' + dispRange + '</strong> of ' + totalMatched + ' listings (Page ' + currentPage + ' of ' + totalPages + ')';
      if (counterWrapper) counterWrapper.innerHTML = 'Showing <strong id="visibleCount">' + dispRange + '</strong> of ' + totalMatched + ' listings';
      if (viewModeInfo) viewModeInfo.innerText = 'Page ' + currentPage + ' of ' + totalPages;
    }
    if (paginationTopInfo) paginationTopInfo.innerHTML = infoHTML;
    if (paginationInfo) paginationInfo.innerHTML = infoHTML;
  } else {
    // Infinite Scroll Mode
    if (paginationTopContainer) paginationTopContainer.style.display = 'none';
    if (paginationContainer) paginationContainer.style.display = 'none';
    if (infiniteContainer) infiniteContainer.style.display = 'flex';

    const statusText = document.getElementById('infiniteStatusText');
    const btnLoadMore = document.getElementById('btnLoadMore');

    if (currentLang === 'lv') {
      if (counterWrapper) counterWrapper.innerHTML = 'Rāda <strong id="visibleCount">' + currentlyLoadedCount + '</strong> no ' + totalMatched + ' sludinājumiem';
      if (viewModeInfo) viewModeInfo.innerText = 'Ielādēti ' + currentlyLoadedCount + ' no ' + totalMatched;
    } else {
      if (counterWrapper) counterWrapper.innerHTML = 'Showing <strong id="visibleCount">' + currentlyLoadedCount + '</strong> of ' + totalMatched + ' listings';
      if (viewModeInfo) viewModeInfo.innerText = 'Loaded ' + currentlyLoadedCount + ' of ' + totalMatched;
    }

    if (currentlyLoadedCount >= totalMatched) {
      if (statusText) statusText.innerText = dict.all_loaded + ' (' + totalMatched + ')';
      if (btnLoadMore) btnLoadMore.style.display = 'none';
    } else {
      if (statusText) statusText.innerText = (currentLang === 'lv' ? 'Parādīti ' : 'Displayed ') + currentlyLoadedCount + ' / ' + totalMatched;
      if (btnLoadMore) btnLoadMore.style.display = 'inline-block';
    }
  }

  // Update localized price range badge
  const minSlider = document.getElementById('priceMinSlider');
  const maxSlider = document.getElementById('priceMaxSlider');
  if (minSlider && maxSlider) {
    updatePriceRangeBadge(parseInt(minSlider.value, 10) || 0, parseInt(maxSlider.value, 10) || 500);
  }
}

function renderPaginationNav(currPage, totalPgs, navId) {
  const nav = document.getElementById(navId);
  if (!nav) return;
  nav.innerHTML = '';

  const dict = i18n[currentLang];

  // First button
  const firstBtn = document.createElement('button');
  firstBtn.className = 'page-btn';
  firstBtn.innerHTML = dict.page_first;
  firstBtn.disabled = (currPage <= 1);
  firstBtn.onclick = () => goToPage(1);
  nav.appendChild(firstBtn);

  // Prev button
  const prevBtn = document.createElement('button');
  prevBtn.className = 'page-btn';
  prevBtn.innerHTML = dict.page_prev;
  prevBtn.disabled = (currPage <= 1);
  prevBtn.onclick = () => goToPage(currPage - 1);
  nav.appendChild(prevBtn);

  // Pages windowing
  let pages = [];
  if (totalPgs <= 7) {
    for (let p = 1; p <= totalPgs; p++) pages.push(p);
  } else {
    if (currPage <= 4) {
      pages = [1, 2, 3, 4, 5, '...', totalPgs];
    } else if (currPage >= totalPgs - 3) {
      pages = [1, '...', totalPgs - 4, totalPgs - 3, totalPgs - 2, totalPgs - 1, totalPgs];
    } else {
      pages = [1, '...', currPage - 1, currPage, currPage + 1, '...', totalPgs];
    }
  }

  pages.forEach(p => {
    if (p === '...') {
      const span = document.createElement('span');
      span.className = 'page-ellipsis';
      span.innerText = '…';
      nav.appendChild(span);
    } else {
      const pageBtn = document.createElement('button');
      pageBtn.className = 'page-btn' + (p === currPage ? ' active' : '');
      pageBtn.innerText = p;
      pageBtn.onclick = () => goToPage(p);
      nav.appendChild(pageBtn);
    }
  });

  // Next button
  const nextBtn = document.createElement('button');
  nextBtn.className = 'page-btn';
  nextBtn.innerHTML = dict.page_next;
  nextBtn.disabled = (currPage >= totalPgs);
  nextBtn.onclick = () => goToPage(currPage + 1);
  nav.appendChild(nextBtn);

  // Last button
  const lastBtn = document.createElement('button');
  lastBtn.className = 'page-btn';
  lastBtn.innerHTML = dict.page_last;
  lastBtn.disabled = (currPage >= totalPgs);
  lastBtn.onclick = () => goToPage(totalPgs);
  nav.appendChild(lastBtn);
}

function goToPage(page) {
  if (page < 1) page = 1;
  if (totalPages > 0 && page > totalPages) page = totalPages;
  fetchPage(page, false);
}

function onPageSizeChange() {
  const sel = document.getElementById('pageSizeSelect');
  if (!sel) return;
  pageSize = parseInt(sel.value, 10) || 50;

  try {
    localStorage.setItem('cc_finder_page_size', pageSize);
  } catch (e) {}

  fetchPage(1, false);
}

function setViewMode(mode) {
  if (mode !== 'pagination' && mode !== 'infinite') return;
  currentViewMode = mode;

  document.querySelectorAll('.view-btn').forEach(b => b.classList.remove('active'));
  const activeBtn = (mode === 'pagination') ? document.getElementById('viewModePagination') : document.getElementById('viewModeInfinite');
  if (activeBtn) activeBtn.classList.add('active');

  try {
    localStorage.setItem('cc_finder_view_mode', mode);
  } catch (e) {}

  fetchPage(1, false);
}

function loadMoreInfinite() {
  if (isFetching) return;
  const loadedCount = document.querySelectorAll('#listingsTable tbody tr.listing-row').length;
  if (loadedCount >= totalMatched) return;

  fetchPage(currentPage + 1, true);
}

let infiniteObserver;
function setupInfiniteObserver() {
  if (!('IntersectionObserver' in window)) return;
  const sentinel = document.getElementById('infiniteSentinel');
  if (!sentinel) return;
  if (infiniteObserver) infiniteObserver.disconnect();

  infiniteObserver = new IntersectionObserver((entries) => {
    if (entries[0].isIntersecting && currentViewMode === 'infinite') {
      loadMoreInfinite();
    }
  }, { rootMargin: '300px' });

  infiniteObserver.observe(sentinel);
}

window.addEventListener('scroll', () => {
  if (currentViewMode !== 'infinite') return;
  const loadedCount = document.querySelectorAll('#listingsTable tbody tr.listing-row').length;
  if (loadedCount >= totalMatched) return;

  const scrollPos = window.innerHeight + window.scrollY;
  const bottomPos = document.documentElement.offsetHeight - 400;
  if (scrollPos >= bottomPos) {
    loadMoreInfinite();
  }
}, { passive: true });

function handleSort(col, headerEl) {
  if (currentSortCol === col) {
    currentSortDir = (currentSortDir === 'desc') ? 'asc' : 'desc';
  } else {
    currentSortCol = col;
    currentSortDir = (col === 'title' || col === 'source' || col === 'location') ? 'asc' : 'desc';
  }

  document.querySelectorAll('th.sortable').forEach(th => {
    th.classList.remove('active');
    const icon = th.querySelector('.sort-icon');
    if (icon) icon.innerText = '↕';
  });

  headerEl.classList.add('active');
  const icon = headerEl.querySelector('.sort-icon');
  if (icon) {
    icon.innerText = currentSortDir === 'asc' ? '▲' : '▼';
  }

  fetchPage(1, false);
}

function onScoreSliderChange() {
  const slider = document.getElementById('scoreSlider');
  const val = parseInt(slider.value, 10) || 0;
  const badge = document.getElementById('scoreSliderBadge');
  const valText = document.getElementById('scoreSliderValText');

  if (valText) valText.innerText = val + '%';
  if (badge) badge.innerText = '≥ ' + val + '%';

  document.querySelectorAll('.score-btn').forEach(b => b.classList.remove('active'));
  if (val === 0) {
    currentScorePreset = 'all';
    const btn = document.getElementById('scoreBtnAll');
    if (btn) btn.classList.add('active');
  } else if (val >= 75) {
    currentScorePreset = 'high';
    const btn = document.getElementById('scoreBtnHigh');
    if (btn) btn.classList.add('active');
  } else if (val >= minAlertThreshold) {
    currentScorePreset = 'candidates';
    const btn = document.getElementById('scoreBtnCandidates');
    if (btn) btn.classList.add('active');
  } else if (val >= 50) {
    currentScorePreset = 'med';
    const btn = document.getElementById('scoreBtnMed');
    if (btn) btn.classList.add('active');
  } else {
    currentScorePreset = 'custom';
  }

  clearTimeout(searchDebounceTimer);
  searchDebounceTimer = setTimeout(() => {
    fetchPage(1, false);
  }, 200);
}

function setScorePreset(preset, btn) {
  currentScorePreset = preset;
  document.querySelectorAll('.score-btn').forEach(b => b.classList.remove('active'));
  btn.classList.add('active');

  const slider = document.getElementById('scoreSlider');
  const badge = document.getElementById('scoreSliderBadge');
  const valText = document.getElementById('scoreSliderValText');
  let val = 0;

  if (preset === 'all') {
    val = 0;
  } else if (preset === 'candidates') {
    val = minAlertThreshold;
  } else if (preset === 'med') {
    val = 50;
  } else if (preset === 'high') {
    val = 75;
  } else if (preset === 'low') {
    val = 0;
  }

  if (slider) slider.value = val;
  if (valText) valText.innerText = (preset === 'low' ? '< 50%' : val + '%');
  if (badge) badge.innerText = (preset === 'low' ? '< 50%' : '≥ ' + val + '%');

  fetchPage(1, false);
}

function onPriceSliderChange(which) {
  const minSlider = document.getElementById('priceMinSlider');
  const maxSlider = document.getElementById('priceMaxSlider');
  const minInput = document.getElementById('minPrice');
  const maxInput = document.getElementById('maxPrice');
  const minValTag = document.getElementById('priceMinVal');
  const maxValTag = document.getElementById('priceMaxVal');

  let minV = parseInt(minSlider.value, 10) || 0;
  let maxV = parseInt(maxSlider.value, 10) || 500;

  if (which === 'min' && minV > maxV) {
    maxV = minV;
    maxSlider.value = maxV;
  } else if (which === 'max' && maxV < minV) {
    minV = maxV;
    minSlider.value = minV;
  }

  if (minValTag) minValTag.innerText = minV + '€';
  if (maxValTag) maxValTag.innerText = (maxV >= 500 ? '500€+' : maxV + '€');

  if (minInput) minInput.value = (minV > 0 ? minV : '');
  if (maxInput) maxInput.value = (maxV < 500 ? maxV : '');

  updatePriceRangeBadge(minV, maxV);

  document.querySelectorAll('.price-btn').forEach(b => b.classList.remove('active'));
  if (minV === 0 && maxV >= 500) {
    const b = document.getElementById('priceBtnAll');
    if (b) b.classList.add('active');
  }

  clearTimeout(searchDebounceTimer);
  searchDebounceTimer = setTimeout(() => {
    fetchPage(1, false);
  }, 250);
}

function updatePriceRangeBadge(minV, maxV) {
  const badge = document.getElementById('priceRangeDisplay');
  if (!badge) return;
  if (minV <= 0 && maxV >= 500) {
    badge.innerText = (currentLang === 'lv' ? 'Visas cenas' : 'All prices');
  } else if (minV <= 0) {
    badge.innerText = '≤ ' + maxV + ' €';
  } else if (maxV >= 500) {
    badge.innerText = '≥ ' + minV + ' €';
  } else {
    badge.innerText = minV + ' – ' + maxV + ' €';
  }
}

function syncPriceSlidersFromInputs() {
  const minInput = document.getElementById('minPrice');
  const maxInput = document.getElementById('maxPrice');
  const minSlider = document.getElementById('priceMinSlider');
  const maxSlider = document.getElementById('priceMaxSlider');
  const minValTag = document.getElementById('priceMinVal');
  const maxValTag = document.getElementById('priceMaxVal');

  let minV = minInput && minInput.value ? Math.max(0, parseInt(minInput.value, 10)) : 0;
  let maxV = maxInput && maxInput.value ? Math.min(500, parseInt(maxInput.value, 10)) : 500;

  if (minSlider) minSlider.value = Math.min(minV, 500);
  if (maxSlider) maxSlider.value = Math.min(maxV, 500);
  if (minValTag) minValTag.innerText = minV + '€';
  if (maxValTag) maxValTag.innerText = (maxV >= 500 ? '500€+' : maxV + '€');

  updatePriceRangeBadge(minV, maxV);
}

function setPricePreset(preset, btn) {
  document.querySelectorAll('.price-btn').forEach(b => b.classList.remove('active'));
  btn.classList.add('active');

  const minInput = document.getElementById('minPrice');
  const maxInput = document.getElementById('maxPrice');

  if (preset === 'under50') {
    minInput.value = '';
    maxInput.value = '50';
  } else if (preset === '50to150') {
    minInput.value = '50';
    maxInput.value = '150';
  } else if (preset === 'over150') {
    minInput.value = '150';
    maxInput.value = '';
  } else {
    minInput.value = '';
    maxInput.value = '';
  }

  syncPriceSlidersFromInputs();
  fetchPage(1, false);
}

function onPriceInput() {
  document.querySelectorAll('.price-btn').forEach(b => b.classList.remove('active'));
  syncPriceSlidersFromInputs();
  clearTimeout(searchDebounceTimer);
  searchDebounceTimer = setTimeout(() => {
    fetchPage(1, false);
  }, 300);
}

function onSearchInput() {
  clearTimeout(searchDebounceTimer);
  searchDebounceTimer = setTimeout(() => {
    fetchPage(1, false);
  }, 250);
}

function applyFilters() {
  fetchPage(1, false);
}

function resetFilters() {
  document.getElementById('searchInput').value = '';
  document.getElementById('sourceFilter').value = '';
  document.getElementById('statusFilter').value = '';
  document.getElementById('photoFilter').checked = false;
  document.getElementById('minPrice').value = '';
  document.getElementById('maxPrice').value = '';

  currentScorePreset = 'all';
  document.querySelectorAll('.score-btn').forEach(b => b.classList.remove('active'));
  const allScoreBtn = document.getElementById('scoreBtnAll');
  if (allScoreBtn) allScoreBtn.classList.add('active');

  document.querySelectorAll('.price-btn').forEach(b => b.classList.remove('active'));
  const allPriceBtn = document.getElementById('priceBtnAll');
  if (allPriceBtn) allPriceBtn.classList.add('active');

  const scoreSlider = document.getElementById('scoreSlider');
  if (scoreSlider) scoreSlider.value = 0;
  const scoreBadge = document.getElementById('scoreSliderBadge');
  if (scoreBadge) scoreBadge.innerText = '≥ 0%';
  const scoreValText = document.getElementById('scoreSliderValText');
  if (scoreValText) scoreValText.innerText = '0%';

  syncPriceSlidersFromInputs();
  fetchPage(1, false);
}

function setLanguage(lang) {
  if (!i18n[lang]) return;
  currentLang = lang;

  document.querySelectorAll('.lang-btn').forEach(b => b.classList.remove('active'));
  const activeBtn = document.getElementById('lang-btn-' + lang);
  if (activeBtn) activeBtn.classList.add('active');

  const dict = i18n[lang];

  // Update elements with data-i18n
  document.querySelectorAll('[data-i18n]').forEach(el => {
    const key = el.getAttribute('data-i18n');
    if (dict[key]) {
      el.innerText = dict[key];
    }
  });

  // Update elements with data-i18n-placeholder
  document.querySelectorAll('[data-i18n-placeholder]').forEach(el => {
    const key = el.getAttribute('data-i18n-placeholder');
    if (dict[key]) {
      el.placeholder = dict[key];
    }
  });

  // Subtitle
  const subEl = document.getElementById('subtitleText');
  if (subEl) {
    subEl.innerText = dict.subtitle_prefix + lastUpdatedTime;
  }

  // Update score badges in table
  document.querySelectorAll('.score-badge').forEach(badge => {
    const isHigh = badge.classList.contains('score-high');
    const isMed = badge.classList.contains('score-medium');
    const scoreVal = badge.innerText.split('%')[0] + '%';
    if (lang === 'lv') {
      badge.innerText = scoreVal + (isHigh ? ' AUGSTA' : isMed ? ' VIDĒJA' : ' ZEMA');
    } else {
      badge.innerText = scoreVal + (isHigh ? ' HIGH' : isMed ? ' MED' : ' LOW');
    }
  });

  renderUI();

  try {
    localStorage.setItem('cc_finder_lang', lang);
  } catch (e) {}
}

function triggerScan() {
  const btn = document.getElementById('btn-scan');
  btn.disabled = true;
  btn.innerText = i18n[currentLang].btn_scanning;
  fetch('/api/scan?product=' + encodeURIComponent('{{.ProductID}}'), { method: 'POST' })
    .then(r => r.json())
    .then(data => {
      alert(i18n[currentLang].alert_scan_started);
      setTimeout(() => { window.location.reload(); }, 8000);
    })
    .catch(e => {
      alert('Scan error: ' + e);
      btn.disabled = false;
      btn.innerText = i18n[currentLang].btn_scan;
    });
}

function init() {
  try {
    const savedMode = localStorage.getItem('cc_finder_view_mode');
    if (savedMode === 'pagination' || savedMode === 'infinite') {
      currentViewMode = savedMode;
      document.querySelectorAll('.view-btn').forEach(b => b.classList.remove('active'));
      const activeBtn = (savedMode === 'pagination') ? document.getElementById('viewModePagination') : document.getElementById('viewModeInfinite');
      if (activeBtn) activeBtn.classList.add('active');
    }

    const savedPageSize = localStorage.getItem('cc_finder_page_size');
    if (savedPageSize) {
      const parsed = parseInt(savedPageSize, 10);
      if ([25, 50, 100, 200].includes(parsed)) {
        pageSize = parsed;
        const sel = document.getElementById('pageSizeSelect');
        if (sel) sel.value = String(parsed);
      }
    }

    const urlLang = new URLSearchParams(window.location.search).get('lang');
    const savedLang = localStorage.getItem('cc_finder_lang');
    if (urlLang === 'lv' || urlLang === 'en') {
      currentLang = urlLang;
    } else if (savedLang === 'lv' || savedLang === 'en') {
      currentLang = savedLang;
    } else if (navigator.language && navigator.language.startsWith('lv')) {
      currentLang = 'lv';
    }
  } catch (e) {}

  setLanguage(currentLang);
  syncPriceSlidersFromInputs();
  renderUI();
  setupInfiniteObserver();
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', init);
} else {
  init();
}
</script>
</body>
</html>`
