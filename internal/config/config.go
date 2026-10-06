package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config represents all application configuration settings.
type Config struct {
	// General settings
	ScanInterval  time.Duration
	MinAlertScore int
	DBPath        string
	LogLevel      string

	// Telegram
	TelegramBotToken string
	TelegramChatID   string

	// Optional AI Classifier
	LLMEnabled  bool
	LLMProvider string
	LLMAPIKey   string
	LLMModel    string

	// Marketplace Settings
	SSComEnabled        bool
	VitaLombardsEnabled bool

	VintedEnabled       bool
	VintedSessionCookie string
	VintedUserAgent     string

	BanknoteEnabled bool
	BanknoteCookies string

	AndeleEnabled bool
	AndeleCookies string

	FBEnabled bool
	FBCookies string

	// HTTP Server Dashboard
	HTTPEnabled bool
	HTTPAddr    string

	// Search queries to use
	SearchTerms []string
}

// Load loads configuration from environment variables and an optional .env file.
func Load(envPath string) (*Config, error) {
	if envPath == "" {
		envPath = ".env"
	}
	_ = loadEnvFile(envPath) // Silently ignore if .env does not exist

	scanIntervalStr := getEnv("SCAN_INTERVAL", "30m")
	scanInterval, err := time.ParseDuration(scanIntervalStr)
	if err != nil {
		scanInterval = 30 * time.Minute
	}

	minAlertScore, err := strconv.Atoi(getEnv("MIN_ALERT_SCORE", "70"))
	if err != nil {
		minAlertScore = 70
	}

	termsStr := getEnv("SEARCH_TERMS", "")
	var terms []string
	if termsStr != "" {
		for _, t := range strings.Split(termsStr, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				terms = append(terms, t)
			}
		}
	}
	if len(terms) == 0 {
		terms = DefaultSearchTerms()
	}

	cfg := &Config{
		ScanInterval:        scanInterval,
		MinAlertScore:       minAlertScore,
		DBPath:              getEnv("DB_PATH", "data/connectclip.db"),
		LogLevel:            getEnv("LOG_LEVEL", "info"),
		TelegramBotToken:    getEnv("TELEGRAM_BOT_TOKEN", ""),
		TelegramChatID:      getEnv("TELEGRAM_CHAT_ID", ""),
		LLMEnabled:          getEnvBool("LLM_ENABLED", false),
		LLMProvider:         getEnv("LLM_PROVIDER", "gemini"),
		LLMAPIKey:           getEnv("LLM_API_KEY", ""),
		LLMModel:            getEnv("LLM_MODEL", "gemini-2.0-flash"),
		SSComEnabled:        getEnvBool("SSCOM_ENABLED", true),
		VitaLombardsEnabled: getEnvBool("VITALOMBARDS_ENABLED", true),
		VintedEnabled:       getEnvBool("VINTED_ENABLED", true),
		VintedSessionCookie: getEnv("VINTED_SESSION_COOKIE", ""),
		VintedUserAgent:     getEnv("VINTED_USER_AGENT", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
		BanknoteEnabled:     getEnvBool("BANKNOTE_ENABLED", true),
		BanknoteCookies:     getEnv("BANKNOTE_COOKIES", ""),
		AndeleEnabled:       getEnvBool("ANDELE_ENABLED", true),
		AndeleCookies:       getEnv("ANDELE_COOKIES", ""),
		FBEnabled:           getEnvBool("FB_ENABLED", false),
		FBCookies:           getEnv("FB_COOKIES", ""),
		HTTPEnabled:         getEnvBool("HTTP_ENABLED", true),
		HTTPAddr:            getEnv("HTTP_ADDR", ":8080"),
		SearchTerms:         terms,
	}

	return cfg, nil
}

// DefaultSearchTerms returns the curated list of queries.
func DefaultSearchTerms() []string {
	return []string{
		"Oticon ConnectClip",
		"ConnectClip",
		"Connect Clip",
		"Oticon dzirdes",
		"Oticon mikrofons",
		"Oticon Bluetooth",
		"Oticon streamer",
		"178509",
	}
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		v := strings.ToLower(strings.TrimSpace(val))
		return v == "true" || v == "1" || v == "yes"
	}
	return defaultVal
}

func loadEnvFile(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			v := strings.TrimSpace(parts[1])
			// Strip optional quotes
			if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
				v = v[1 : len(v)-1]
			}
			if os.Getenv(k) == "" {
				_ = os.Setenv(k, v)
			}
		}
	}
	return scanner.Err()
}
