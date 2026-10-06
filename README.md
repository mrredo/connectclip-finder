# ConnectClip Finder 🔍

An automated, reliable multi-product marketplace monitoring daemon designed to find a lost **Oticon ConnectClip** hearing-aid accessory or **track multiple target products simultaneously** (e.g. Nintendo Switch OLED, AirPods Pro, laptops, power tools) across Latvian second-hand marketplaces.

---

## 📚 Documentation

- [🚀 **Quick Start Guide**](docs/QUICKSTART.md) — Fast-track setup, running binary or Docker, CLI modes, and dashboard walkthrough.
- [⚙️ **Configuration Guide (`products.json` & `.env`)**](docs/CONFIGURATION.md) — Detailed guide for multi-product JSON definitions, environment variables, Telegram alerts, and session cookies.
- [🧠 **Developer Docs & Architecture**](docs/ARCHITECTURE.md) — System internals, data flow, deterministic matching engine, SQLite WAL layer, and step-by-step tutorial for adding new marketplaces.

---

## 🎯 Multi-Product Tracking & Welcome Screen

ConnectClip Finder supports monitoring **multiple independent products simultaneously** using `products.json`:

1. **Welcome Screen Grid (`GET /`):** A responsive grid displaying cards for each configured product with live statistics (Total Listings, Candidate Matches, Active Alerts, Price Range, and Last Seen).
2. **Product Records Dashboard (`GET /?product=<id>`):** Dedicated tabular view for each product featuring column sorting, server-side pagination, infinite scroll, multi-criteria filtering, and bilingual support (🇱🇻 / 🇬🇧).
3. **Scoped Scraping & Alerts:** Each product maintains its own keywords, search queries, price boundaries, and scoring rules. Scrapes can run globally across all products or scoped to a specific target.

Pre-configured in `products.json`:
- **Oticon ConnectClip (`oticon-connectclip`):** 2.4 GHz Bluetooth streamer, part numbers `178509`, `AC1A`, `2ACAHAC01`.
- **Nintendo Switch OLED (`nintendo-switch-oled`):** Handheld gaming console, model `HEG-001`.
- **AirPods Pro 2 (`airpods-pro-2`):** Wireless noise-cancelling earbuds, models `A2698`, `A2699`, `A2700`.

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

# -------------------------------------------------------------
# Target Product Configuration (Customizable for ANY item)
# -------------------------------------------------------------
TARGET_NAME=Oticon ConnectClip
SEARCH_TERMS=Oticon ConnectClip, ConnectClip, Connect Clip, Oticon dzirdes, Oticon mikrofons, Oticon Bluetooth, Oticon streamer, 178509
TARGET_MIN_PRICE=30
TARGET_MAX_PRICE=200

# Optional custom keyword signals:
# MATCH_EXACT_KEYWORDS=Oticon ConnectClip, Connect Clip
# MATCH_CONTEXT_KEYWORDS=mikrofons, dzirdes aparats, streamer
# MATCH_MODEL_NUMBERS=178509, AC1A, 2ACAHAC01
# MATCH_EXCLUDE_KEYWORDS=ladetajs, charger, tv adapter, edumic, phonak, widex

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

### 🎯 Monitoring Other Products
To monitor another item instead of Oticon ConnectClip (e.g. Nintendo Switch OLED, AirPods, Makita drill, camera, etc.), simply update your `.env`:

```env
TARGET_NAME=Nintendo Switch OLED
SEARCH_TERMS=Nintendo Switch OLED, Switch OLED, Switch konsole
TARGET_MIN_PRICE=180
TARGET_MAX_PRICE=320
MATCH_EXACT_KEYWORDS=Nintendo Switch OLED, Switch OLED
MATCH_CONTEXT_KEYWORDS=konsole, joy con, dock
MATCH_MODEL_NUMBERS=HEG-001
MATCH_EXCLUDE_KEYWORDS=Switch Lite, spēle, game only, case only, vāciņš
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

- **Welcome Screen (Product Grid):** [http://localhost:8080/](http://localhost:8080/)
  - Displays all monitored targets defined in `products.json`.
  - Shows real-time statistics cards: total listings, candidate matches, active alerts, price range, and last seen timestamp.
  - One-click navigation into any product's records.
- **Product Records Dashboard:** [http://localhost:8080/?product=oticon-connectclip](http://localhost:8080/?product=oticon-connectclip)
  - **Live Listings Table:** Displays tracked items with thumbnail images, match score badges (`HIGH`, `MED`, `LOW`), prices in EUR, locations, and matched signal tags.
  - **Sorting & Filtering:** Sort by any column (Score, Title, Price, Source, Date) and filter by search text, source, minimum score, and photo availability.
  - **Pagination & Infinite Scroll:** Switch between fixed-size pages (25, 50, 100, 200) and smooth infinite scrolling.
  - **Bilingual Interface:** Instant language toggle between `🇱🇻 Latviešu` and `🇬🇧 English`.
  - **Manual Trigger Button:** Click "🔄 Trigger Scan Now" to immediately trigger a background scrape cycle for the selected product.
- **JSON REST API:**
  - `GET /api/products`: List all configured products with their live statistics.
  - `GET /api/listings?product=<id>`: Fetch filtered, paginated listing records.
  - `POST /api/scan?product=<id>`: Trigger an asynchronous scrape job.
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
