package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
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

	data := dashboardData{
		TotalListings: total,
		Candidates:    candidates,
		Notified:      notified,
		MinAlertScore: s.minAlertScore,
		Listings:      listings,
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
  .container { max-width: 1400px; margin: 0 auto; }
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
  }
  .btn:hover { background: var(--primary-hover); }
  .btn-secondary { background: var(--card-border); color: #fff; }
  .btn-secondary:hover { background: #475569; }

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

  /* Controls & Search */
  .table-controls {
    display: flex;
    justify-content: space-between;
    align-items: center;
    flex-wrap: wrap;
    gap: 16px;
    background: var(--card);
    border: 1px solid var(--card-border);
    padding: 14px 18px;
    border-radius: 8px;
    margin-bottom: 16px;
  }
  .search-input {
    background: #0f172a;
    border: 1px solid var(--card-border);
    color: #fff;
    padding: 8px 14px;
    border-radius: 6px;
    width: 280px;
    font-size: 0.9rem;
  }
  .search-input:focus { outline: 1px solid var(--primary); }
  .filter-group { display: flex; gap: 8px; align-items: center; font-size: 0.9rem; color: var(--text-muted); }
  .filter-btn {
    background: transparent;
    border: 1px solid var(--card-border);
    color: var(--text-muted);
    padding: 6px 12px;
    border-radius: 6px;
    cursor: pointer;
    font-size: 0.85rem;
  }
  .filter-btn.active, .filter-btn:hover { background: var(--card-border); color: #fff; }

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
      <h1>ConnectClip Finder <span class="badge-oticon">Oticon Monitor</span></h1>
      <p style="font-size: 0.85rem; color: var(--text-muted); margin-top: 4px;">Tracking Latvian marketplaces for lost Oticon ConnectClip • Updated {{.LastUpdated}}</p>
    </div>
    <div class="actions">
      <button class="btn" id="btn-scan" onclick="triggerScan()">🔄 Trigger Scan Now</button>
      <a href="/" class="btn btn-secondary">Refresh</a>
    </div>
  </header>

  <div class="stats-grid">
    <div class="stat-card">
      <div class="stat-label">Total Listings Tracked</div>
      <div class="stat-val">{{.TotalListings}}</div>
    </div>
    <div class="stat-card">
      <div class="stat-label">Promising Candidates (&ge;{{.MinAlertScore}}%)</div>
      <div class="stat-val high">{{.Candidates}}</div>
    </div>
    <div class="stat-card">
      <div class="stat-label">Telegram Alerts Dispatched</div>
      <div class="stat-val">{{.Notified}}</div>
    </div>
    <div class="stat-card">
      <div class="stat-label">Alert Threshold</div>
      <div class="stat-val">{{.MinAlertScore}}%</div>
    </div>
  </div>

  <div class="table-controls">
    <input type="text" id="searchInput" class="search-input" placeholder="Search titles, sources, locations..." onkeyup="filterTable()">
    <div class="filter-group">
      <span>Filter:</span>
      <button class="filter-btn active" onclick="setScoreFilter(0, this)">All ({{len .Listings}})</button>
      <button class="filter-btn" onclick="setScoreFilter(50, this)">Medium+ (&ge;50%)</button>
      <button class="filter-btn" onclick="setScoreFilter({{.MinAlertScore}}, this)">Candidates Only (&ge;{{.MinAlertScore}}%)</button>
    </div>
  </div>

  <div class="table-container">
    <table id="listingsTable">
      <thead>
        <tr>
          <th style="width: 60px;">Photo</th>
          <th>Score</th>
          <th>Title & Details</th>
          <th>Source</th>
          <th>Price</th>
          <th>Location</th>
          <th>Signals</th>
          <th>Last Seen</th>
          <th>Status</th>
        </tr>
      </thead>
      <tbody>
        {{range .Listings}}
        <tr data-score="{{.Score}}">
          <td>
            {{if .PrimaryImage}}
              <img src="{{.PrimaryImage}}" alt="" class="thumb" onerror="this.style.display='none'">
            {{else}}
              <div class="no-img">No img</div>
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
              <span class="notified-tag">🚨 Alerted</span>
            {{else}}
              <span style="color: var(--text-muted); font-size: 0.75rem;">—</span>
            {{end}}
          </td>
        </tr>
        {{else}}
        <tr>
          <td colspan="9" style="text-align: center; padding: 40px; color: var(--text-muted);">
            No listings tracked yet. Click "Trigger Scan Now" to begin scanning.
          </td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </div>
</div>

<script>
function triggerScan() {
  const btn = document.getElementById('btn-scan');
  btn.disabled = true;
  btn.innerText = '⏳ Scanning...';
  fetch('/api/scan', { method: 'POST' })
    .then(r => r.json())
    .then(data => {
      alert('Scan initiated! The dashboard will auto-refresh in 8 seconds.');
      setTimeout(() => { window.location.reload(); }, 8000);
    })
    .catch(e => {
      alert('Scan error: ' + e);
      btn.disabled = false;
      btn.innerText = '🔄 Trigger Scan Now';
    });
}

function filterTable() {
  const query = document.getElementById('searchInput').value.toLowerCase();
  const rows = document.querySelectorAll('#listingsTable tbody tr');
  rows.forEach(r => {
    const text = r.innerText.toLowerCase();
    const score = parseInt(r.getAttribute('data-score') || '0', 10);
    const scorePass = score >= currentMinScore;
    const textPass = text.includes(query);
    r.style.display = (scorePass && textPass) ? '' : 'none';
  });
}

let currentMinScore = 0;
function setScoreFilter(min, btn) {
  currentMinScore = min;
  document.querySelectorAll('.filter-btn').forEach(b => b.classList.remove('active'));
  btn.classList.add('active');
  filterTable();
}
</script>
</body>
</html>`
