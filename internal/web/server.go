package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"time"

	"connectclip-finder/internal/model"
	"connectclip-finder/internal/scheduler"
	"connectclip-finder/internal/storage"
)

// Server provides an HTTP dashboard displaying tracked listings.
type Server struct {
	addr          string
	repo          *storage.Repository
	sched         *scheduler.Scheduler
	minAlertScore int
	httpServer    *http.Server
}

// NewServer initializes the dashboard HTTP server.
func NewServer(addr string, repo *storage.Repository, sched *scheduler.Scheduler, minAlertScore int) *Server {
	s := &Server{
		addr:          addr,
		repo:          repo,
		sched:         sched,
		minAlertScore: minAlertScore,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleDashboard)
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

type dashboardData struct {
	TotalListings int
	Candidates    int
	Notified      int
	MinAlertScore int
	Listings      []*model.Listing
	Sources       []string
	LastUpdated   string
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	ctx := r.Context()
	total, candidates, notified, err := s.repo.GetStats(ctx, s.minAlertScore)
	if err != nil {
		slog.Error("Failed to fetch dashboard stats", "error", err)
	}

	minScore := 0
	if q := r.URL.Query().Get("min_score"); q != "" {
		minScore, _ = strconv.Atoi(q)
	}

	listings, err := s.repo.GetAllListings(ctx, minScore)
	if err != nil {
		slog.Error("Failed to fetch listings for dashboard", "error", err)
		http.Error(w, "Failed to load listings", http.StatusInternalServerError)
		return
	}

	sourceMap := make(map[string]bool)
	for _, l := range listings {
		if l.Source != "" {
			sourceMap[string(l.Source)] = true
		}
	}
	var sources []string
	for src := range sourceMap {
		sources = append(sources, src)
	}
	sort.Strings(sources)

	data := dashboardData{
		TotalListings: total,
		Candidates:    candidates,
		Notified:      notified,
		MinAlertScore: s.minAlertScore,
		Listings:      listings,
		Sources:       sources,
		LastUpdated:   time.Now().Format("15:04:05 02.01.2006"),
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

func (s *Server) handleAPIListings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	minScore := 0
	if q := r.URL.Query().Get("min_score"); q != "" {
		minScore, _ = strconv.Atoi(q)
	}

	listings, err := s.repo.GetAllListings(ctx, minScore)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(listings)
}

func (s *Server) handleAPITriggerScan(w http.ResponseWriter, r *http.Request) {
	if s.sched == nil {
		http.Error(w, "Scheduler not available", http.StatusBadRequest)
		return
	}

	go func() {
		slog.Info("Manual scan triggered via Web UI")
		_ = s.sched.RunOnce(context.Background())
	}()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "scan_started",
		"message": "Marketplace scan initiated in background.",
	})
}

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>ConnectClip Finder - Marketplace Monitor</title>
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

  /* Controls & Filters */
  .table-controls {
    background: var(--card);
    border: 1px solid var(--card-border);
    padding: 18px 20px;
    border-radius: 8px;
    margin-bottom: 16px;
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .controls-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    flex-wrap: wrap;
    gap: 12px;
  }
  .search-wrap {
    position: relative;
    flex: 1;
    min-width: 260px;
    max-width: 380px;
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
    padding: 8px 12px;
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
    padding: 7px 10px;
    border-radius: 6px;
    width: 78px;
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
    gap: 8px;
    align-items: center;
    flex-wrap: wrap;
    font-size: 0.85rem;
    color: var(--text-muted);
  }
  .filter-btn {
    background: transparent;
    border: 1px solid var(--card-border);
    color: var(--text-muted);
    padding: 5px 11px;
    border-radius: 6px;
    cursor: pointer;
    font-size: 0.82rem;
    transition: all 0.15s ease;
  }
  .filter-btn:hover { background: var(--card-border); color: #fff; }
  .filter-btn.active { background: #0284c7; color: #fff; border-color: #38bdf8; font-weight: 600; }
  .result-counter {
    font-size: 0.85rem;
    color: var(--text-muted);
    margin-left: auto;
  }
  .result-counter strong { color: #fff; }

  /* Table */
  .table-container {
    background: var(--card);
    border: 1px solid var(--card-border);
    border-radius: 8px;
    overflow-x: auto;
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
  <header>
    <div>
      <h1>ConnectClip Finder <span class="badge-oticon" data-i18n="badge_oticon">Oticon Monitor</span></h1>
      <p style="font-size: 0.85rem; color: var(--text-muted); margin-top: 4px;" id="subtitleText">Tracking Latvian marketplaces for lost Oticon ConnectClip • Updated {{.LastUpdated}}</p>
    </div>
    <div class="actions">
      <!-- Language Switcher -->
      <div class="lang-switch">
        <button id="lang-btn-lv" class="lang-btn" onclick="setLanguage('lv')">🇱🇻 Latviešu</button>
        <button id="lang-btn-en" class="lang-btn active" onclick="setLanguage('en')">🇬🇧 English</button>
      </div>

      <button class="btn" id="btn-scan" onclick="triggerScan()"><span data-i18n="btn_scan">🔄 Trigger Scan Now</span></button>
      <a href="/" class="btn btn-secondary" data-i18n="btn_refresh">Refresh</a>
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
    <!-- Filter Controls Row 1 -->
    <div class="controls-row">
      <div class="search-wrap">
        <span class="search-icon">🔍</span>
        <input type="text" id="searchInput" class="search-input" data-i18n-placeholder="search_placeholder" placeholder="Search title, description, location, signals..." onkeyup="applyFilters()">
      </div>

      <div style="display: flex; gap: 10px; align-items: center; flex-wrap: wrap;">
        <!-- Marketplace Source Filter -->
        <select id="sourceFilter" class="filter-select" onchange="applyFilters()">
          <option value="" data-i18n="source_all">All Marketplaces</option>
          {{range .Sources}}
            <option value="{{.}}">{{.}}</option>
          {{end}}
        </select>

        <!-- Status Filter -->
        <select id="statusFilter" class="filter-select" onchange="applyFilters()">
          <option value="" data-i18n="status_all">All Alert Statuses</option>
          <option value="alerted" data-i18n="status_alerted">🚨 Alerted Only</option>
          <option value="unalerted" data-i18n="status_unalerted">Not Yet Alerted</option>
        </select>

        <!-- Price Range -->
        <div class="price-inputs">
          <span data-i18n="label_price">Price:</span>
          <input type="number" id="minPrice" class="num-input" data-i18n-placeholder="price_min" placeholder="Min €" min="0" oninput="onPriceInput()">
          <span>–</span>
          <input type="number" id="maxPrice" class="num-input" data-i18n-placeholder="price_max" placeholder="Max €" min="0" oninput="onPriceInput()">
        </div>

        <!-- Photos Only Checkbox -->
        <label class="checkbox-label">
          <input type="checkbox" id="photoFilter" onchange="applyFilters()">
          <span data-i18n="label_with_photo">📷 With photo</span>
        </label>

        <!-- Reset Button -->
        <button class="btn btn-secondary btn-sm" onclick="resetFilters()" data-i18n="btn_clear">✕ Clear</button>
      </div>
    </div>

    <!-- Filter Controls Row 2: Presets & Counter -->
    <div class="controls-row">
      <!-- Score Presets -->
      <div class="filter-group">
        <span data-i18n="label_score">Score:</span>
        <button id="scoreBtnAll" class="filter-btn score-btn active" onclick="setScorePreset('all', this)" data-i18n="score_all">All</button>
        <button id="scoreBtnCandidates" class="filter-btn score-btn" onclick="setScorePreset('candidates', this)" data-i18n="score_candidates">Candidates (&ge;{{.MinAlertScore}}%)</button>
        <button id="scoreBtnMed" class="filter-btn score-btn" onclick="setScorePreset('med', this)" data-i18n="score_med">Medium+ (&ge;50%)</button>
        <button id="scoreBtnHigh" class="filter-btn score-btn" onclick="setScorePreset('high', this)" data-i18n="score_high">High (&ge;75%)</button>
        <button id="scoreBtnLow" class="filter-btn score-btn" onclick="setScorePreset('low', this)" data-i18n="score_low">Low (&lt;50%)</button>
      </div>

      <!-- Price Presets -->
      <div class="filter-group">
        <span data-i18n="label_price_range">Price Range:</span>
        <button id="priceBtnAll" class="filter-btn price-btn active" onclick="setPricePreset('all', this)" data-i18n="price_all">All</button>
        <button id="priceBtnUnder50" class="filter-btn price-btn" onclick="setPricePreset('under50', this)" data-i18n="price_under50">&lt; €50</button>
        <button id="priceBtnTarget" class="filter-btn price-btn" onclick="setPricePreset('50to150', this)" data-i18n="price_target">€50 – €150 (Target)</button>
        <button id="priceBtnOver150" class="filter-btn price-btn" onclick="setPricePreset('over150', this)" data-i18n="price_over150">&gt; €150</button>
      </div>

      <!-- Live Counter -->
      <div class="result-counter" id="visibleCounterWrapper">
        Showing <strong id="visibleCount">{{len .Listings}}</strong> of {{len .Listings}} listings
      </div>
    </div>
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
    alert_scan_started: "Scan initiated! The dashboard will auto-refresh in 8 seconds."
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
    alert_scan_started: "Meklēšana sākta! Lapa tiks atjaunota pēc 8 sekundēm."
  }
};

let currentLang = 'en';

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

  applyFilters();

  try {
    localStorage.setItem('cc_finder_lang', lang);
  } catch (e) {}
}

function triggerScan() {
  const btn = document.getElementById('btn-scan');
  btn.disabled = true;
  btn.innerText = i18n[currentLang].btn_scanning;
  fetch('/api/scan', { method: 'POST' })
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

// -------------------------------------------------------------
// Column Sorting
// -------------------------------------------------------------
let currentSortCol = 'score';
let currentSortDir = 'desc';

function handleSort(col, headerEl) {
  if (currentSortCol === col) {
    currentSortDir = (currentSortDir === 'desc') ? 'asc' : 'desc';
  } else {
    currentSortCol = col;
    if (col === 'title' || col === 'source' || col === 'location') {
      currentSortDir = 'asc';
    } else {
      currentSortDir = 'desc';
    }
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

  sortRows();
}

function sortRows() {
  const tbody = document.querySelector('#listingsTable tbody');
  const rows = Array.from(tbody.querySelectorAll('tr.listing-row'));
  if (rows.length === 0) return;

  rows.sort((a, b) => {
    const valA = a.dataset[currentSortCol] || '';
    const valB = b.dataset[currentSortCol] || '';

    if (['score', 'price', 'signals', 'time', 'status'].includes(currentSortCol)) {
      const numA = parseFloat(valA) || 0;
      const numB = parseFloat(valB) || 0;
      return currentSortDir === 'asc' ? numA - numB : numB - numA;
    }

    const comp = valA.localeCompare(valB, undefined, { sensitivity: 'base', numeric: true });
    return currentSortDir === 'asc' ? comp : -comp;
  });

  const noResultsRow = document.getElementById('noResultsRow');
  rows.forEach(r => tbody.appendChild(r));
  if (noResultsRow) {
    tbody.appendChild(noResultsRow);
  }
}

// -------------------------------------------------------------
// Extended Filtering
// -------------------------------------------------------------
let currentScorePreset = 'all';

function setScorePreset(preset, btn) {
  currentScorePreset = preset;
  document.querySelectorAll('.score-btn').forEach(b => b.classList.remove('active'));
  btn.classList.add('active');
  applyFilters();
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
  applyFilters();
}

function onPriceInput() {
  document.querySelectorAll('.price-btn').forEach(b => b.classList.remove('active'));
  applyFilters();
}

function applyFilters() {
  const query = (document.getElementById('searchInput').value || '').toLowerCase().trim();
  const source = (document.getElementById('sourceFilter').value || '').toLowerCase();
  const status = document.getElementById('statusFilter').value;
  const photoOnly = document.getElementById('photoFilter').checked;

  const minPriceVal = parseFloat(document.getElementById('minPrice').value);
  const maxPriceVal = parseFloat(document.getElementById('maxPrice').value);

  const rows = document.querySelectorAll('#listingsTable tbody tr.listing-row');
  let visibleCount = 0;

  rows.forEach(r => {
    const rScore = parseInt(r.dataset.score || '0', 10);
    const rPrice = parseFloat(r.dataset.price || '0');
    const rSource = (r.dataset.source || '').toLowerCase();
    const rStatus = r.dataset.status;
    const rPhoto = r.dataset.photo;
    const rText = r.innerText.toLowerCase();

    // 1. Text Search
    let matchText = true;
    if (query) {
      matchText = rText.includes(query);
    }

    // 2. Score Preset Filter
    let matchScore = true;
    if (currentScorePreset === 'candidates') {
      matchScore = rScore >= minAlertThreshold;
    } else if (currentScorePreset === 'med') {
      matchScore = rScore >= 50;
    } else if (currentScorePreset === 'high') {
      matchScore = rScore >= 75;
    } else if (currentScorePreset === 'low') {
      matchScore = rScore < 50;
    }

    // 3. Source Filter
    let matchSource = true;
    if (source) {
      matchSource = rSource === source;
    }

    // 4. Alert Status Filter
    let matchStatus = true;
    if (status === 'alerted') {
      matchStatus = rStatus === '1';
    } else if (status === 'unalerted') {
      matchStatus = rStatus === '0';
    }

    // 5. Photo Filter
    let matchPhoto = true;
    if (photoOnly) {
      matchPhoto = rPhoto === '1';
    }

    // 6. Price Range
    let matchPrice = true;
    if (!isNaN(minPriceVal) && rPrice < minPriceVal) {
      matchPrice = false;
    }
    if (!isNaN(maxPriceVal) && rPrice > maxPriceVal) {
      matchPrice = false;
    }

    const isVisible = matchText && matchScore && matchSource && matchStatus && matchPhoto && matchPrice;
    r.style.display = isVisible ? '' : 'none';
    if (isVisible) visibleCount++;
  });

  const counterWrapper = document.getElementById('visibleCounterWrapper');
  if (counterWrapper) {
    if (currentLang === 'lv') {
      counterWrapper.innerHTML = 'Rāda <strong id="visibleCount">' + visibleCount + '</strong> no ' + totalListingCount + ' sludinājumiem';
    } else {
      counterWrapper.innerHTML = 'Showing <strong id="visibleCount">' + visibleCount + '</strong> of ' + totalListingCount + ' listings';
    }
  }

  const noResultsRow = document.getElementById('noResultsRow');
  if (noResultsRow) {
    noResultsRow.style.display = (visibleCount === 0 && rows.length > 0) ? '' : 'none';
  }
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

  applyFilters();
}

// Initialize on page load
document.addEventListener('DOMContentLoaded', () => {
  sortRows();
  try {
    const urlLang = new URLSearchParams(window.location.search).get('lang');
    const savedLang = localStorage.getItem('cc_finder_lang');
    if (urlLang === 'lv' || urlLang === 'en') {
      setLanguage(urlLang);
    } else if (savedLang === 'lv' || savedLang === 'en') {
      setLanguage(savedLang);
    } else if (navigator.language && navigator.language.startsWith('lv')) {
      setLanguage('lv');
    }
  } catch (e) {}
});
</script>
</body>
</html>`
