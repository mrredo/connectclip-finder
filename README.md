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
