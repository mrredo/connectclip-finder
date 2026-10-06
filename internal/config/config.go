package config

import (
	"bufio"
	"encoding/json"
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

	// Target item configuration (legacy/fallback)
	TargetName           string
	TargetMinPrice       float64
	TargetMaxPrice       float64
	MatchExactKeywords   []string
	MatchContextKeywords []string
	MatchModelNumbers    []string
	MatchExcludeKeywords []string

	// Search queries to use
	SearchTerms []string

	// Multi-product configurations
	ProductsPath string
	Products     []ProductConfig
}

// ProductConfig defines target product search terms, scoring rules, and metadata.
type ProductConfig struct {
	ID                   string    `json:"id"`
	Name                 string    `json:"name"`
	Icon                 string    `json:"icon"`
	Category             string    `json:"category"`
	Description          string    `json:"description"`
	Enabled              bool      `json:"enabled"`
	SearchTerms          []string  `json:"search_terms"`
	MinPrice             float64   `json:"min_price"`
	MaxPrice             float64   `json:"max_price"`
	AlertThreshold       int       `json:"alert_threshold"`
	MatchExactKeywords   []string  `json:"exact_keywords"`
	MatchContextKeywords []string  `json:"context_keywords"`
	MatchModelNumbers    []string  `json:"model_numbers"`
	MatchExcludeKeywords []string  `json:"exclude_keywords"`
	RulePreset           string    `json:"rule_preset,omitempty"`
	CustomRule           string    `json:"custom_rule,omitempty"`
	MaxAlertPrice        float64   `json:"max_alert_price,omitempty"`
	MinAlertPrice        float64   `json:"min_alert_price,omitempty"`
	CreatedAt            time.Time `json:"created_at,omitempty"`
	UpdatedAt            time.Time `json:"updated_at,omitempty"`
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

	terms := getEnvSlice("SEARCH_TERMS")
	if len(terms) == 0 {
		terms = DefaultSearchTerms()
	}

	productsPath := getEnv("PRODUCTS_PATH", "products.json")

	cfg := &Config{
		ScanInterval:         scanInterval,
		MinAlertScore:        minAlertScore,
		DBPath:               getEnv("DB_PATH", "data/connectclip.db"),
		LogLevel:             getEnv("LOG_LEVEL", "info"),
		TelegramBotToken:     getEnv("TELEGRAM_BOT_TOKEN", ""),
		TelegramChatID:       getEnv("TELEGRAM_CHAT_ID", ""),
		LLMEnabled:           getEnvBool("LLM_ENABLED", false),
		LLMProvider:          getEnv("LLM_PROVIDER", "gemini"),
		LLMAPIKey:            getEnv("LLM_API_KEY", ""),
		LLMModel:             getEnv("LLM_MODEL", "gemini-2.0-flash"),
		SSComEnabled:         getEnvBool("SSCOM_ENABLED", true),
		VitaLombardsEnabled:  getEnvBool("VITALOMBARDS_ENABLED", true),
		VintedEnabled:        getEnvBool("VINTED_ENABLED", true),
		VintedSessionCookie:  getEnv("VINTED_SESSION_COOKIE", ""),
		VintedUserAgent:      getEnv("VINTED_USER_AGENT", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
		BanknoteEnabled:      getEnvBool("BANKNOTE_ENABLED", true),
		BanknoteCookies:      getEnv("BANKNOTE_COOKIES", ""),
		AndeleEnabled:        getEnvBool("ANDELE_ENABLED", true),
		AndeleCookies:        getEnv("ANDELE_COOKIES", ""),
		FBEnabled:            getEnvBool("FB_ENABLED", false),
		FBCookies:            getEnv("FB_COOKIES", ""),
		HTTPEnabled:          getEnvBool("HTTP_ENABLED", true),
		HTTPAddr:             getEnv("HTTP_ADDR", ":8080"),
		TargetName:           getEnv("TARGET_NAME", "Oticon ConnectClip"),
		TargetMinPrice:       getEnvFloat("TARGET_MIN_PRICE", 30),
		TargetMaxPrice:       getEnvFloat("TARGET_MAX_PRICE", 200),
		MatchExactKeywords:   getEnvSlice("MATCH_EXACT_KEYWORDS"),
		MatchContextKeywords: getEnvSlice("MATCH_CONTEXT_KEYWORDS"),
		MatchModelNumbers:    getEnvSlice("MATCH_MODEL_NUMBERS"),
		MatchExcludeKeywords: getEnvSlice("MATCH_EXCLUDE_KEYWORDS"),
		SearchTerms:          terms,
		ProductsPath:         productsPath,
	}

	cfg.Products = cfg.LoadProducts()

	return cfg, nil
}

// LoadProducts loads products from products.json or initializes defaults.
func (c *Config) LoadProducts() []ProductConfig {
	if c.ProductsPath != "" {
		if data, err := os.ReadFile(c.ProductsPath); err == nil && len(data) > 0 {
			var products []ProductConfig
			if err := json.Unmarshal(data, &products); err == nil && len(products) > 0 {
				for i := range products {
					if products[i].AlertThreshold <= 0 {
						products[i].AlertThreshold = c.MinAlertScore
					}
					if products[i].Icon == "" {
						products[i].Icon = "📦"
					}
				}
				return products
			}
		}
	}

	// Fallback to default product list
	defaults := DefaultProducts(c)

	// Save default template if file does not exist
	if c.ProductsPath != "" {
		if _, err := os.Stat(c.ProductsPath); os.IsNotExist(err) {
			if data, err := json.MarshalIndent(defaults, "", "  "); err == nil {
				_ = os.WriteFile(c.ProductsPath, data, 0644)
			}
		}
	}

	return defaults
}

// GetProduct returns the product configuration with the given ID.
func (c *Config) GetProduct(id string) *ProductConfig {
	for i := range c.Products {
		if c.Products[i].ID == id {
			return &c.Products[i]
		}
	}
	return nil
}

// DefaultProducts returns preconfigured products.
func DefaultProducts(cfg *Config) []ProductConfig {
	return []ProductConfig{
		{
			ID:                   "oticon-connectclip",
			Name:                 cfg.TargetName,
			Icon:                 "🎧",
			Category:             "Audio / Hearing",
			Description:          "Wireless hearing aid microphone and Bluetooth audio streamer",
			Enabled:              true,
			SearchTerms:          cfg.SearchTerms,
			MinPrice:             cfg.TargetMinPrice,
			MaxPrice:             cfg.TargetMaxPrice,
			AlertThreshold:       cfg.MinAlertScore,
			MatchExactKeywords:   cfg.MatchExactKeywords,
			MatchContextKeywords: cfg.MatchContextKeywords,
			MatchModelNumbers:    cfg.MatchModelNumbers,
			MatchExcludeKeywords: cfg.MatchExcludeKeywords,
			RulePreset:           "any_match",
		},
		{
			ID:                   "samsung-s22-fe",
			Name:                 "Samsung Galaxy S22 FE / S22",
			Icon:                 "📱",
			Category:             "Smartphones",
			Description:          "Samsung Galaxy S22 Series (S22 / S22 FE) flagship phone",
			Enabled:              true,
			SearchTerms:          []string{"Samsung S22", "Galaxy S22", "Samsung S22 FE", "Galaxy S22 FE", "SM-S901B"},
			MinPrice:             120,
			MaxPrice:             280,
			AlertThreshold:       70,
			RulePreset:           "great_deal",
			MaxAlertPrice:        250,
			MatchExactKeywords:   []string{"galaxy s22", "samsung s22", "s22 fe"},
			MatchContextKeywords: []string{"samsung", "galaxy", "128gb", "256gb", "snapdragon", "exynos", "viedtālrunis"},
			MatchModelNumbers:    []string{"sm-s901b", "sm-s901"},
			MatchExcludeKeywords: []string{"vāciņš", "case", "stikliņš", "screen protector", "salauzts", "defekts", "detaļām"},
		},
		{
			ID:                   "samsung-s22-ultra",
			Name:                 "Samsung Galaxy S22 Ultra",
			Icon:                 "📱",
			Category:             "Smartphones",
			Description:          "Samsung Galaxy S22 Ultra with S-Pen & 108MP camera",
			Enabled:              true,
			SearchTerms:          []string{"Samsung S22 Ultra", "Galaxy S22 Ultra", "S22 Ultra", "SM-S908B"},
			MinPrice:             220,
			MaxPrice:             450,
			AlertThreshold:       70,
			RulePreset:           "great_deal",
			MaxAlertPrice:        400,
			MatchExactKeywords:   []string{"galaxy s22 ultra", "samsung s22 ultra", "s22 ultra"},
			MatchContextKeywords: []string{"samsung", "galaxy", "ultra", "s-pen", "256gb", "512gb", "12gb"},
			MatchModelNumbers:    []string{"sm-s908b", "sm-s908"},
			MatchExcludeKeywords: []string{"vāciņš", "case", "stikliņš", "plēve", "salauzts", "defekts", "detaļām"},
		},
		{
			ID:                   "samsung-s23-fe",
			Name:                 "Samsung Galaxy S23 FE",
			Icon:                 "📱",
			Category:             "Smartphones",
			Description:          "Samsung Galaxy S23 Fan Edition phone",
			Enabled:              true,
			SearchTerms:          []string{"Samsung S23 FE", "Galaxy S23 FE", "S23 FE", "SM-S711B"},
			MinPrice:             220,
			MaxPrice:             390,
			AlertThreshold:       70,
			RulePreset:           "great_deal",
			MaxAlertPrice:        350,
			MatchExactKeywords:   []string{"galaxy s23 fe", "samsung s23 fe", "s23 fe"},
			MatchContextKeywords: []string{"samsung", "galaxy", "128gb", "256gb", "5g", "viedtālrunis"},
			MatchModelNumbers:    []string{"sm-s711b", "sm-s711"},
			MatchExcludeKeywords: []string{"vāciņš", "case", "stikliņš", "defekts", "salauzts", "detaļām"},
		},
		{
			ID:                   "samsung-s23-ultra",
			Name:                 "Samsung Galaxy S23 Ultra",
			Icon:                 "📱",
			Category:             "Smartphones",
			Description:          "Samsung Galaxy S23 Ultra with Snapdragon 8 Gen 2 & 200MP",
			Enabled:              true,
			SearchTerms:          []string{"Samsung S23 Ultra", "Galaxy S23 Ultra", "S23 Ultra", "SM-S918B"},
			MinPrice:             380,
			MaxPrice:             650,
			AlertThreshold:       70,
			RulePreset:           "great_deal",
			MaxAlertPrice:        580,
			MatchExactKeywords:   []string{"galaxy s23 ultra", "samsung s23 ultra", "s23 ultra"},
			MatchContextKeywords: []string{"samsung", "galaxy", "ultra", "s-pen", "256gb", "512gb", "snapdragon"},
			MatchModelNumbers:    []string{"sm-s918b", "sm-s918"},
			MatchExcludeKeywords: []string{"vāciņš", "case", "stikliņš", "defekts", "salauzts", "detaļām"},
		},
		{
			ID:                   "samsung-s24-fe",
			Name:                 "Samsung Galaxy S24 FE",
			Icon:                 "📱",
			Category:             "Smartphones",
			Description:          "Samsung Galaxy S24 Fan Edition phone with Galaxy AI",
			Enabled:              true,
			SearchTerms:          []string{"Samsung S24 FE", "Galaxy S24 FE", "S24 FE", "SM-S721B"},
			MinPrice:             380,
			MaxPrice:             580,
			AlertThreshold:       70,
			RulePreset:           "great_deal",
			MaxAlertPrice:        520,
			MatchExactKeywords:   []string{"galaxy s24 fe", "samsung s24 fe", "s24 fe"},
			MatchContextKeywords: []string{"samsung", "galaxy", "128gb", "256gb", "ai", "viedtālrunis"},
			MatchModelNumbers:    []string{"sm-s721b", "sm-s721"},
			MatchExcludeKeywords: []string{"vāciņš", "case", "stikliņš", "defekts", "salauzts", "detaļām"},
		},
		{
			ID:                   "samsung-s24-ultra",
			Name:                 "Samsung Galaxy S24 Ultra",
			Icon:                 "📱",
			Category:             "Smartphones",
			Description:          "Samsung Galaxy S24 Ultra Titanium with Galaxy AI & S-Pen",
			Enabled:              true,
			SearchTerms:          []string{"Samsung S24 Ultra", "Galaxy S24 Ultra", "S24 Ultra", "SM-S928B"},
			MinPrice:             550,
			MaxPrice:             880,
			AlertThreshold:       70,
			RulePreset:           "great_deal",
			MaxAlertPrice:        750,
			MatchExactKeywords:   []string{"galaxy s24 ultra", "samsung s24 ultra", "s24 ultra"},
			MatchContextKeywords: []string{"samsung", "galaxy", "ultra", "titanium", "s-pen", "256gb", "512gb", "ai"},
			MatchModelNumbers:    []string{"sm-s928b", "sm-s928"},
			MatchExcludeKeywords: []string{"vāciņš", "case", "stikliņš", "defekts", "salauzts", "detaļām"},
		},
		{
			ID:                   "samsung-s25-ultra",
			Name:                 "Samsung Galaxy S25 Ultra",
			Icon:                 "📱",
			Category:             "Smartphones",
			Description:          "Samsung Galaxy S25 Ultra flagship with Snapdragon 8 Elite",
			Enabled:              true,
			SearchTerms:          []string{"Samsung S25 Ultra", "Galaxy S25 Ultra", "S25 Ultra", "SM-S938B"},
			MinPrice:             750,
			MaxPrice:             1200,
			AlertThreshold:       70,
			RulePreset:           "great_deal",
			MaxAlertPrice:        980,
			MatchExactKeywords:   []string{"galaxy s25 ultra", "samsung s25 ultra", "s25 ultra"},
			MatchContextKeywords: []string{"samsung", "galaxy", "ultra", "snapdragon", "s-pen", "ai"},
			MatchModelNumbers:    []string{"sm-s938b", "sm-s938"},
			MatchExcludeKeywords: []string{"vāciņš", "case", "stikliņš", "defekts", "salauzts", "detaļām"},
		},
		{
			ID:                   "samsung-s26-ultra",
			Name:                 "Samsung Galaxy S26 Ultra",
			Icon:                 "📱",
			Category:             "Smartphones",
			Description:          "Samsung Galaxy S26 Ultra flagship generation phone",
			Enabled:              true,
			SearchTerms:          []string{"Samsung S26 Ultra", "Galaxy S26 Ultra", "S26 Ultra", "SM-S948B"},
			MinPrice:             900,
			MaxPrice:             1450,
			AlertThreshold:       70,
			RulePreset:           "great_deal",
			MaxAlertPrice:        1200,
			MatchExactKeywords:   []string{"galaxy s26 ultra", "samsung s26 ultra", "s26 ultra"},
			MatchContextKeywords: []string{"samsung", "galaxy", "ultra", "s-pen", "flagship"},
			MatchModelNumbers:    []string{"sm-s948b", "sm-s948"},
			MatchExcludeKeywords: []string{"vāciņš", "case", "stikliņš", "defekts", "salauzts", "detaļām"},
		},
		{
			ID:                   "nintendo-switch-oled",
			Name:                 "Nintendo Switch OLED",
			Icon:                 "🎮",
			Category:             "Gaming",
			Description:          "Nintendo Switch OLED model console and accessories",
			Enabled:              true,
			SearchTerms: []string{
				"Nintendo Switch OLED",
				"Switch OLED",
				"Nintendo OLED",
			},
			MinPrice:             150,
			MaxPrice:             320,
			AlertThreshold:       70,
			RulePreset:           "great_deal",
			MatchExactKeywords:   []string{"switch oled", "nintendo oled"},
			MatchContextKeywords: []string{"nintendo", "switch", "oled", "konsole", "console"},
			MatchModelNumbers:    []string{"heg-001"},
			MatchExcludeKeywords: []string{"switch lite", "v1", "v2", "spēle", "game only"},
		},
		{
			ID:                   "airpods-pro-2",
			Name:                 "Apple AirPods Pro 2",
			Icon:                 "🎵",
			Category:             "Audio / Electronics",
			Description:          "Apple AirPods Pro 2nd Generation with MagSafe / USB-C",
			Enabled:              true,
			SearchTerms: []string{
				"AirPods Pro 2",
				"AirPods Pro 2nd",
				"AirPods Pro USB-C",
			},
			MinPrice:             100,
			MaxPrice:             230,
			AlertThreshold:       70,
			RulePreset:           "great_deal",
			MatchExactKeywords:   []string{"airpods pro 2", "airpods pro 2nd"},
			MatchContextKeywords: []string{"apple", "airpods", "pro", "austiņas", "magsafe", "usb-c"},
			MatchModelNumbers:    []string{"a2968", "a3047", "a3048", "mqd83"},
			MatchExcludeKeywords: []string{"case only", "tikai kastīte", "kopija", "replica"},
		},
	}
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

func getEnvSlice(key string) []string {
	val := getEnv(key, "")
	if val == "" {
		return nil
	}
	var res []string
	for _, part := range strings.Split(val, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			res = append(res, part)
		}
	}
	return res
}

func getEnvFloat(key string, defaultVal float64) float64 {
	val := getEnv(key, "")
	if val == "" {
		return defaultVal
	}
	f, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return defaultVal
	}
	return f
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
