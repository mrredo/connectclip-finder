# Configuration Guide (`.env`) ⚙️

This document describes all environment variables used by **ConnectClip Finder**, how to configure Telegram alerts, how to export browser cookies for protected marketplaces, and how to customize the system to monitor **any product or item**.

---

## 📑 Table of Contents
1. [Core Settings](#1-core-settings)
2. [Target Product Configuration (Universal Monitoring)](#2-target-product-configuration-universal-monitoring)
3. [Telegram Alert Setup](#3-telegram-alert-setup)
4. [Marketplace Adapters & Cookie Configuration](#4-marketplace-adapters--cookie-configuration)
5. [Web Dashboard Settings](#5-web-dashboard-settings)
6. [Optional AI Verification (Gemini / OpenAI)](#6-optional-ai-verification-gemini--openai)
7. [Copy-Paste Configuration Examples](#7-copy-paste-configuration-examples)

---

## 1. Core Settings

| Variable | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `SCAN_INTERVAL` | duration | `30m` | How frequently the scraper executes across marketplaces (e.g. `15m`, `30m`, `1h`, `2h`). |
| `MIN_ALERT_SCORE` | integer | `70` | Score threshold (0–100) required to trigger a Telegram alert. Listings below this score are stored in SQLite but will not ping your phone. |
| `DB_PATH` | string | `data/connectclip.db` | Relative or absolute path to the SQLite database file. Parent directory is created automatically. |
| `LOG_LEVEL` | string | `info` | Logging verbosity: `debug`, `info`, `warn`, `error`. Use `debug` for detailed HTTP request diagnostics. |

---

## 2. Target Product Configuration (Universal Monitoring)

By default, the finder is configured to look for the **Oticon ConnectClip** hearing aid accessory. You can monitor **any other item** by setting these variables:

| Variable | Type | Example / Default | Description |
| :--- | :--- | :--- | :--- |
| `TARGET_NAME` | string | `Oticon ConnectClip` | Name of the product displayed in Telegram alerts and dashboard headers. |
| `SEARCH_TERMS` | comma-separated | `Oticon ConnectClip, ConnectClip, 178509` | Search queries submitted to marketplace search bars. |
| `TARGET_MIN_PRICE` | number | `30` | Minimum reasonable price for the item (in EUR). Grants +15% score bonus if price falls in range. |
| `TARGET_MAX_PRICE` | number | `200` | Maximum reasonable price for the item (in EUR). |
| `MATCH_EXACT_KEYWORDS` | comma-separated | `Oticon ConnectClip, Connect Clip` | Exact or primary phrases that indicate a strong match (+85% score). |
| `MATCH_CONTEXT_KEYWORDS` | comma-separated | `mikrofons, dzirdes aparats, streamer` | Supporting keywords that boost confidence (+20% each). |
| `MATCH_MODEL_NUMBERS` | comma-separated | `178509, AC1A, 2ACAHAC01` | Specific part numbers, model codes, or article IDs (+75% score). |
| `MATCH_EXCLUDE_KEYWORDS` | comma-separated | `ladetajs, charger, tv adapter` | Negative keywords that incur a penalty (-50% each) to filter out accessories or wrong models. |

> **Note:** If `MATCH_EXACT_KEYWORDS`, `MATCH_MODEL_NUMBERS`, and `MATCH_CONTEXT_KEYWORDS` are omitted, the engine automatically uses handcrafted rules for **Oticon ConnectClip**.

---

## 3. Telegram Alert Setup

Instant notifications with photos and direct listing links can be delivered to your Telegram chat or channel.

### How to Get Your Credentials:
1. **Create a Bot:**
   - Open Telegram and message **[@BotFather](https://t.me/botfather)**.
   - Send `/newbot`, name your bot, and copy the **HTTP API Token** (e.g. `123456789:ABCdefGHIjklMNOpqrSTUvwxYZ`).
2. **Get Your Chat ID:**
   - Message your newly created bot and click **Start** (`/start`).
   - Open **[@userinfobot](https://t.me/userinfobot)** on Telegram to get your numeric ID (e.g. `8812732880`).
3. **Configure `.env`:**
   ```env
   TELEGRAM_BOT_TOKEN=123456789:ABCdefGHIjklMNOpqrSTUvwxYZ
   TELEGRAM_CHAT_ID=8812732880
   ```
4. **Test Delivery:**
   ```powershell
   .\connectclip-finder.exe -test-telegram
   ```

---

## 4. Marketplace Adapters & Cookie Configuration

Each marketplace adapter can be individually enabled or disabled:

| Marketplace | Toggle Variable | Authentication / Cookies |
| :--- | :--- | :--- |
| **SS.com** | `SSCOM_ENABLED=true` | None required (publicly accessible). |
| **Vita Lombards** | `VITALOMBARDS_ENABLED=true` | None required (publicly accessible). |
| **Andele Mandele** | `ANDELE_ENABLED=true` | None required (`ANDELE_COOKIES` optional). |
| **Banknote** | `BANKNOTE_ENABLED=true` | `BANKNOTE_COOKIES` optional. |
| **Vinted** | `VINTED_ENABLED=true` | `VINTED_SESSION_COOKIE` recommended. |
| **Facebook Marketplace** | `FB_ENABLED=true` | `FB_COOKIES` required. |

### How to Export Cookies for Protected Marketplaces:

#### Vinted (`VINTED_SESSION_COOKIE`)
Cloudflare Turnstile protects Vinted's API from unauthenticated bots. To allow the finder to query Vinted:
1. Open [vinted.lv](https://www.vinted.lv) (or your local Vinted portal) in Google Chrome or Firefox and log in.
2. Press `F12` to open Developer Tools and go to the **Application** tab (Chrome) or **Storage** tab (Firefox).
3. Expand **Cookies** -> `https://www.vinted.lv`.
4. Copy the cookie string (or use an extension like *Cookie-Editor* to export as Header String).
5. Paste into `.env`:
   ```env
   VINTED_SESSION_COOKIE="anon_id=...; _vinted_fr_session=...; access_token_web=..."
   ```

#### Facebook Marketplace (`FB_COOKIES`)
Facebook strictly requires a valid browser session to display Marketplace listings:
1. Open [facebook.com/marketplace](https://www.facebook.com/marketplace) in your browser.
2. Open Developer Tools (`F12`), go to the **Network** tab, refresh the page, click on any request to `facebook.com`, and copy the `Cookie` request header.
3. Paste into `.env`:
   ```env
   FB_ENABLED=true
   FB_COOKIES="c_user=...; xs=...; datr=..."
   ```

---

## 5. Web Dashboard Settings

| Variable | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `HTTP_ENABLED` | boolean | `true` | When true, starts the local web dashboard. |
| `HTTP_ADDR` | string | `:8080` | Bind address and port (e.g. `:8080` or `127.0.0.1:3000`). |

---

## 6. Optional AI Verification (Gemini / OpenAI)

When enabled, listings with borderline scores (40–70%) can be verified via a lightweight LLM call to distinguish accessories from full sets, chargers, or wrong models:

```env
LLM_ENABLED=true
LLM_PROVIDER=gemini        # 'gemini' or 'openai'
LLM_API_KEY=AIzaSy...
LLM_MODEL=gemini-2.0-flash # or 'gpt-4o-mini'
```

---

## 7. Copy-Paste Configuration Examples

### Example A: Default Oticon ConnectClip Monitor
```env
SCAN_INTERVAL=30m
MIN_ALERT_SCORE=70
DB_PATH=data/connectclip.db
LOG_LEVEL=info

TARGET_NAME=Oticon ConnectClip
SEARCH_TERMS=Oticon ConnectClip, ConnectClip, Connect Clip, Oticon dzirdes, Oticon mikrofons, Oticon Bluetooth, Oticon streamer, 178509
TARGET_MIN_PRICE=30
TARGET_MAX_PRICE=200

SSCOM_ENABLED=true
VITALOMBARDS_ENABLED=true
ANDELE_ENABLED=true
BANKNOTE_ENABLED=true
VINTED_ENABLED=true
FB_ENABLED=false

HTTP_ENABLED=true
HTTP_ADDR=:8080
```

---

### Example B: Monitoring a Nintendo Switch OLED
```env
SCAN_INTERVAL=20m
MIN_ALERT_SCORE=75
DB_PATH=data/switch_oled.db
LOG_LEVEL=info

TARGET_NAME=Nintendo Switch OLED
SEARCH_TERMS=Nintendo Switch OLED, Switch OLED, Switch konsole
TARGET_MIN_PRICE=180
TARGET_MAX_PRICE=320
MATCH_EXACT_KEYWORDS=Nintendo Switch OLED, Switch OLED
MATCH_CONTEXT_KEYWORDS=konsole, joy con, dock
MATCH_MODEL_NUMBERS=HEG-001
MATCH_EXCLUDE_KEYWORDS=Switch Lite, spēle, game only, case only, vāciņš

SSCOM_ENABLED=true
VITALOMBARDS_ENABLED=true
ANDELE_ENABLED=true
BANKNOTE_ENABLED=true
VINTED_ENABLED=true

HTTP_ENABLED=true
HTTP_ADDR=:8080
```

---

### Example C: Monitoring Apple AirPods Pro 2 (USB-C)
```env
SCAN_INTERVAL=15m
MIN_ALERT_SCORE=70
DB_PATH=data/airpods_pro.db

TARGET_NAME=AirPods Pro 2
SEARCH_TERMS=AirPods Pro 2, AirPods Pro USB-C, AirPods Pro 2nd
TARGET_MIN_PRICE=100
TARGET_MAX_PRICE=210
MATCH_EXACT_KEYWORDS=AirPods Pro 2, AirPods Pro 2nd gen, AirPods Pro USB C
MATCH_CONTEXT_KEYWORDS=austinas, case, kārbiņa, magsafe
MATCH_MODEL_NUMBERS=A2968, A3047, A3048
MATCH_EXCLUDE_KEYWORDS=AirPods 3, AirPods 2, AirPods 1, tikai kārbiņa, case only, viena austiņa, replika, copy

SSCOM_ENABLED=true
VITALOMBARDS_ENABLED=true
ANDELE_ENABLED=true
BANKNOTE_ENABLED=true
VINTED_ENABLED=true
```

---

### Example D: Monitoring a Makita Cordless Drill
```env
SCAN_INTERVAL=1h
MIN_ALERT_SCORE=70
DB_PATH=data/makita.db

TARGET_NAME=Makita 18V Urbjmašīna
SEARCH_TERMS=Makita 18V, Makita DDF484, Makita DHP484, Makita urbjmasina
TARGET_MIN_PRICE=50
TARGET_MAX_PRICE=160
MATCH_EXACT_KEYWORDS=Makita 18V, Makita DDF484, Makita DHP484
MATCH_CONTEXT_KEYWORDS=akumulatora urbjmasina, koferis, baterija, ladetajs
MATCH_MODEL_NUMBERS=DDF484, DHP484, DDF485
MATCH_EXCLUDE_KEYWORDS=tikai korpuss, body only, 12V, 10.8V, lādētājs bez instrumenta

SSCOM_ENABLED=true
VITALOMBARDS_ENABLED=true
ANDELE_ENABLED=false
BANKNOTE_ENABLED=true
```
