# ConnectClip Finder 🔍

An automated, reliable marketplace monitoring daemon designed to find a lost **Oticon ConnectClip** hearing-aid accessory being resold online on Latvian second-hand marketplaces.

---

## 🎯 Target Product Specifications

- **Product:** Oticon ConnectClip (AC1A)
- **Primary Function:** 2.4 GHz Bluetooth streamer, wireless microphone, remote control, and hands-free headset for Oticon wireless hearing aids (Opn, More, Real, Intent, etc.).
- **Part / Article Number:** `178509` (also packaging variants: `180424`, `180425`)
- **FCC ID:** `2ACAHAC01`
- **Retail Price:** ~€300 new, typically €40 - €150 second-hand.

---

## 🏗️ Architecture

```
Scrapers (SS.com, Vita Lombards, Andele Mandele, Banknote, Vinted, Facebook)
       │
       ▼
Normalized Listing Model (source, ID, title, price, images, location)
       │
       ▼
Deduplication & Fingerprinting (SHA-256 / source-specific ID)
       │
       ▼
Multi-Signal Matching Engine (Positive & Negative signals, diacritics normalization)
       │
       ▼ [Optional AI Verification for ambiguous candidates]
SQLite Persistence (WAL mode, price history tracking, scan audit logs)
       │
       ▼
Telegram Bot Notifier (Instant photo + HTML alert on matches >= threshold)
```

### Marketplace Status & Integration Technique

| Marketplace | Access Technique | Authentication / Cookies | Status |
| :--- | :--- | :--- | :--- |
| **SS.com** | Direct HTTP GET `/lv/search-result/?q=...` | None required | ✅ Fully automated |
| **Vita Lombards** | Direct HTTP GET `/lv/?search=...` (GA4 JSON) | None required | ✅ Fully automated |
| **Andele Mandele** | Catalog search `/search/?search=...` | None required (optional session) | ✅ Fully automated |
| **Banknote** | Store search `/lv/filter?q=...` | Session headers / `BANKNOTE_COOKIES` | ✅ Automated |
| **Vinted** | Catalog API `/api/v2/catalog/items` | `VINTED_SESSION_COOKIE` | ⚠️ Cloudflare protected |
| **Facebook Marketplace** | Search URL with session | `FB_COOKIES` required | 🔒 Auth gated |

*Note on Vinted & Facebook:*
- **Vinted** enforces Cloudflare Managed Challenges on raw automated requests. Setting `VINTED_SESSION_COOKIE` (exported from your browser) allows requests to pass legitimately.
- **Facebook Marketplace** requires an active user session (`FB_COOKIES`). When cookies are not provided, the adapter logs an informational message and gracefully continues scanning all other marketplaces.

---

## ⚙️ Configuration (`.env`)

Copy `.env.example` to `.env`:

```bash
cp .env.example .env
```

Key configuration options:

```env
# Scanning cadence
SCAN_INTERVAL=30m

# Alert threshold (0-100)
MIN_ALERT_SCORE=70

# Database location
DB_PATH=data/connectclip.db

# Telegram Bot Alerts
TELEGRAM_BOT_TOKEN=123456789:ABCdefGHIjklMNOpqrSTUvwxYZ
TELEGRAM_CHAT_ID=-1001234567890

# Optional AI candidate classifier (Gemini or OpenAI API)
LLM_ENABLED=false
LLM_PROVIDER=gemini
LLM_API_KEY=your_key_here
LLM_MODEL=gemini-2.0-flash

# Source-specific session cookies (when available)
VINTED_SESSION_COOKIE=
BANKNOTE_COOKIES=
FB_COOKIES=
```

---

## 🚀 Running the Application

### 1. Run Locally (Windows / Linux / macOS)

**Execute a single test scan and print results:**
```powershell
go run ./cmd/finder -scan-once
```

**Evaluate a custom listing title with the matching engine:**
```powershell
go run ./cmd/finder -eval "Oticon ConnectClip bezvadu mikrofons"
```

**Verify Telegram Bot alerts:**
```powershell
go run ./cmd/finder -test-telegram
```

**Run continuously as a background monitor:**
```powershell
go run ./cmd/finder
```

### 2. Run with Docker / Docker Compose

Build and launch the daemon and web dashboard in the background:
```bash
docker compose up -d
```

Access the dashboard at `http://localhost:8080`.

View real-time logs:
```bash
docker compose logs -f
```

---

## 🌐 Web Dashboard (No Login Required)

ConnectClip Finder includes an integrated HTTP web server designed for instant access without any login or authentication required:

- **URL:** [http://localhost:8080](http://localhost:8080) (customizable via `HTTP_ADDR` or `-http :PORT`)
- **Key Features:**
  - **Live Listings Table:** Displays all tracked items with thumbnail images, match score badges (`HIGH`, `MED`, `LOW`), prices in EUR, locations, and matched signal tags.
  - **Zero Authentication:** Open dashboard, just visit the URL in any browser.
  - **Instant Filtering:** Real-time client-side search input for filtering by title, source, or location, and quick score threshold filter buttons (All, $\ge 50\%$, Candidates $\ge 70\%$).
  - **Manual Trigger Button:** Click "🔄 Trigger Scan Now" to immediately trigger a background marketplace scrape cycle on demand.
  - **JSON API:** Fetch structured data at `GET /api/listings` or filter candidates with `GET /api/listings?min_score=70`.
  - **Serve-Only Mode:** Inspect database findings without scheduling scrapes using `go run ./cmd/finder -serve-only`.


---

## 🧪 Testing

Run all unit tests:
```bash
go test -v ./...
```

The test suite covers:
- Exact & fuzzy matching for `Oticon ConnectClip`
- Part number detection (`178509`, `AC1A`)
- Latvian diacritic normalization (`ā`, `č`, `ē`, `ģ`, `ī`, `ķ`, `ļ`, `ņ`, `š`, `ū`, `ž`)
- False-positive penalties (chargers, TV adapters, EduMic, competing brands like Phonak/Widex)
- Zero-context penalties (generic Bluetooth microphones)
- SQLite database upserts and deduplication
- Telegram HTML notification formatting
