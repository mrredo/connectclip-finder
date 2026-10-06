# Quick Start Guide 🚀

This guide explains how to get **ConnectClip Finder** up and running in under 2 minutes.

---

## 📋 Prerequisites

You can run the application either with **Go (native binary)** or **Docker**:

- **Native Go:** Go 1.22 or higher installed ([Download Go](https://go.dev/dl/))
- **Docker:** Docker and Docker Compose installed ([Download Docker](https://www.docker.com/products/docker-desktop/))

---

## ⚡ 1. Fast Track (Run in 3 Steps)

### Step 1: Clone the Repository
```bash
git clone https://github.com/mrredo/connectclip-finder.git
cd connectclip-finder
```

### Step 2: Create Your Environment File
```bash
# Linux / macOS
cp .env.example .env

# Windows (PowerShell)
Copy-Item .env.example .env
```
*(By default, `.env.example` is pre-configured to search for Oticon ConnectClip on public marketplaces without requiring any API keys or logins).*

### Step 3: Run the Application
```bash
# Windows
go run ./cmd/finder

# Linux / macOS
go run ./cmd/finder
```

Open your browser and navigate to:
👉 **[http://localhost:8080](http://localhost:8080)**

You will immediately see the **Product Selection Welcome Screen Grid** displaying all configured products (Oticon ConnectClip, Nintendo Switch OLED, AirPods Pro 2) along with live statistics! Click on any product card to open its dedicated listing records dashboard.

---

## 🛠️ 2. Build a Standalone Binary

You can compile a single standalone executable with no external runtime dependencies (pure Go, CGO-free):

```powershell
# Windows
go build -o connectclip-finder.exe ./cmd/finder

# Linux / macOS
go build -o connectclip-finder ./cmd/finder
```

Run the compiled executable:
```powershell
# Windows
.\connectclip-finder.exe

# Linux / macOS
./connectclip-finder
```

---

## 🕹️ 3. Execution Modes & CLI Flags

The binary includes several specialized operational modes:

| Flag | Description | Example Usage |
| :--- | :--- | :--- |
| *(no flags)* | **Continuous Daemon Mode:** Runs background scrapers on schedule and hosts the web dashboard. | `.\connectclip-finder.exe` |
| `-serve-only` | **Web Dashboard Only:** Serves the web UI without running background scraper routines. | `.\connectclip-finder.exe -serve-only` |
| `-scan-once` | **Single Scan Cycle:** Runs a complete scan of all enabled marketplaces once, prints a summary, and exits. | `.\connectclip-finder.exe -scan-once` |
| `-eval "<text>"` | **Match Tester:** Scores a listing title against the matching rules and outputs signal reasons. | `.\connectclip-finder.exe -eval "Oticon ConnectClip bezvadu mikrofons"` |
| `-http <addr>` | **Custom Web Port:** Overrides the HTTP bind address from `.env`. | `.\connectclip-finder.exe -http :9000` |
| `-no-http` | **Headless Mode:** Runs the background scraper daemon without starting the web dashboard. | `.\connectclip-finder.exe -no-http` |
| `-test-telegram` | **Verify Telegram Alerts:** Sends a test notification to verify your bot token and chat ID. | `.\connectclip-finder.exe -test-telegram` |
| `-config <path>` | **Custom Config Path:** Loads a custom `.env` file from another directory. | `.\connectclip-finder.exe -config custom.env` |

---

## 🐳 4. Running with Docker & Docker Compose

To run the application inside an isolated Docker container:

### Start Container in Background:
```bash
docker compose up -d
```

### View Live Logs:
```bash
docker compose logs -f
```

### Stop Container:
```bash
docker compose down
```

The web dashboard is automatically published at `http://localhost:8080`, and database records persist in the `./data` volume.

---

## 📊 5. Using the Web Dashboard

Once started, open **[http://localhost:8080](http://localhost:8080)** in your browser:

### 🏠 Welcome Screen (`/`)
- **Product Cards Grid:** Displays all target products monitored from `products.json`.
- **Live Overview:** Each card shows Total Listings, Candidate Matches ($\ge 70\%$), Active Alerts, Price Range, and Last Seen date.
- **Direct Navigation:** Click anywhere on a product card or click **"Atvērt ierakstus →" / "Open Records →"** to access that product's dashboard.

### 📋 Product Records Dashboard (`/?product=<id>`)
- **Breadcrumb Navigation:** Click **"← Visi produkti" / "← All Products"** to return to the welcome grid at any time.
- **Language Toggle:** Click `🇱🇻 Latviešu` or `🇬🇧 English` in the top right to switch languages instantly.
- **View Mode Switcher:**
  - Click `[ 📄 Pagination ]` to browse fixed pages (25, 50, 100, 200 items per page).
  - Click `[ ♾️ Infinite Scroll ]` to automatically load items as you scroll down.
- **Dual Pagination (Top & Bottom):** Navigate pages conveniently using synchronized pagination controls located both above and below the listings table.
- **Grouped Filter Cards & Interactive Sliders:**
  - **Price Range Dual Slider:** Drag min/max price sliders or type values with real-time range badges.
  - **Match Score Threshold Slider:** Slide anywhere from 0% to 100% (with quick presets for Candidates $\ge 70\%$, Medium $\ge 50\%$, High $\ge 75\%$).
  - **Search & Marketplaces Card:** Search input, source dropdown, status filter, and photo-only toggle neatly grouped.
- **Sorting:** Click any column header (`Score`, `Title`, `Source`, `Price`, `Location`, `Last Seen`) to toggle ascending/descending order.
- **Manual Scan Trigger:** Click `🔄 Sākt meklēšanu tagad` / `🔄 Trigger Scan Now` to trigger an immediate scrape in the background for this product.
