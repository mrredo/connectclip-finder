package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"connectclip-finder/internal/config"
	"connectclip-finder/internal/matcher"
	"connectclip-finder/internal/model"
	"connectclip-finder/internal/notifier"
	"connectclip-finder/internal/scheduler"
	"connectclip-finder/internal/scraper"
	"connectclip-finder/internal/scraper/sources"
	"connectclip-finder/internal/storage"
	"connectclip-finder/internal/web"
)

func main() {
	configPath := flag.String("config", ".env", "Path to .env configuration file")
	scanOnce := flag.Bool("scan-once", false, "Execute a single scan cycle and exit immediately")
	serveOnly := flag.Bool("serve-only", false, "Start only the HTTP dashboard without running background marketplace scans")
	httpAddrFlag := flag.String("http", "", "HTTP dashboard address (e.g. :8080, overrides .env)")
	noHTTP := flag.Bool("no-http", false, "Disable the HTTP dashboard server")
	testTelegram := flag.Bool("test-telegram", false, "Test Telegram notification credentials and exit")
	evalText := flag.String("eval", "", "Evaluate matching score for a title string and exit")
	flag.Parse()

	// 1. Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading configuration: %v\n", err)
		os.Exit(1)
	}

	// 2. Quick matcher evaluation mode
	if *evalText != "" {
		m := matcher.NewCustomMatcher(
			cfg.TargetName,
			cfg.MatchExactKeywords,
			cfg.MatchContextKeywords,
			cfg.MatchModelNumbers,
			cfg.MatchExcludeKeywords,
			cfg.TargetMinPrice,
			cfg.TargetMaxPrice,
		)
		testListing := &model.Listing{
			Title:       *evalText,
			Description: *evalText,
			Price:       85.0,
		}
		m.Evaluate(testListing)
		fmt.Printf("Target: %s\nListing: %s\nScore: %d%%\nConfidence: %s\nSignals: %s\n",
			cfg.TargetName, testListing.Title, testListing.Score, testListing.Confidence, strings.Join(testListing.MatchReasons, "; "))
		os.Exit(0)
	}

	// 3. Configure structured logger
	var logLevel slog.Level
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	slog.Info("Starting Marketplace Finder",
		"target", cfg.TargetName,
		"db_path", cfg.DBPath,
		"scan_interval", cfg.ScanInterval,
		"alert_threshold", cfg.MinAlertScore,
	)

	// 4. Initialize Telegram Notifier
	telegram := notifier.NewTelegramNotifier(cfg.TelegramBotToken, cfg.TelegramChatID)
	telegram.SetTargetName(cfg.TargetName)
	if *testTelegram {
		ctx, cancel := context.WithTimeout(context.Background(), cfg.ScanInterval)
		defer cancel()
		slog.Info("Testing Telegram connection...")
		if err := telegram.TestConnection(ctx); err != nil {
			slog.Error("Telegram test failed", "error", err)
			os.Exit(1)
		}
		slog.Info("Telegram test message delivered successfully!")
		os.Exit(0)
	}

	// 5. Initialize Storage
	db, err := storage.Open(cfg.DBPath)
	if err != nil {
		slog.Error("Failed to initialize database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	repo := storage.NewRepository(db)

	// 6. Initialize Matcher & Optional AI Classifier
	matchEngine := matcher.NewCustomMatcher(
		cfg.TargetName,
		cfg.MatchExactKeywords,
		cfg.MatchContextKeywords,
		cfg.MatchModelNumbers,
		cfg.MatchExcludeKeywords,
		cfg.TargetMinPrice,
		cfg.TargetMaxPrice,
	)
	aiClassifier := matcher.NewAIClassifier(cfg.LLMEnabled, cfg.LLMProvider, cfg.LLMAPIKey, cfg.LLMModel)
	if aiClassifier.IsEnabled() {
		slog.Info("AI classifier enabled", "provider", cfg.LLMProvider, "model", cfg.LLMModel)
	}

	// 7. Initialize Marketplace Adapters
	ssAdapter := sources.NewSSComAdapter(cfg.SSComEnabled)
	vitaAdapter := sources.NewVitaLombardsAdapter(cfg.VitaLombardsEnabled)
	andeleAdapter := sources.NewAndeleAdapter(cfg.AndeleEnabled, cfg.AndeleCookies)
	banknoteAdapter := sources.NewBanknoteAdapter(cfg.BanknoteEnabled, cfg.BanknoteCookies)
	vintedAdapter := sources.NewVintedAdapter(cfg.VintedEnabled, cfg.VintedSessionCookie, cfg.VintedUserAgent)
	fbAdapter := sources.NewFacebookAdapter(cfg.FBEnabled, cfg.FBCookies)

	engine := scraper.NewEngine(
		cfg.SearchTerms,
		ssAdapter,
		vitaAdapter,
		andeleAdapter,
		banknoteAdapter,
		vintedAdapter,
		fbAdapter,
	)

	// 8. Initialize Scheduler & Multi-Product Configuration
	sched := scheduler.NewScheduler(
		cfg.ScanInterval,
		cfg.MinAlertScore,
		engine,
		matchEngine,
		aiClassifier,
		repo,
		telegram,
	)

	// Load products from SQLite DB (seeded on startup from products.json or defaults)
	dbProducts, err := repo.GetAllProducts(context.Background())
	if err == nil && len(dbProducts) > 0 {
		cfg.Products = dbProducts
	}
	sched.SetProducts(cfg.Products)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// 9. Single-scan mode
	if *scanOnce {
		report := sched.RunOnce(ctx)
		if report == nil {
			slog.Error("Single scan cycle failed")
			os.Exit(1)
		}
		os.Exit(0)
	}

	// 10. Start HTTP Dashboard Server (if enabled)
	var webServer *web.Server
	if cfg.HTTPEnabled && !*noHTTP {
		addr := cfg.HTTPAddr
		if *httpAddrFlag != "" {
			addr = *httpAddrFlag
		}
		webServer = web.NewServer(addr, repo, sched, cfg.MinAlertScore, cfg.Products)
		go func() {
			if err := webServer.Start(); err != nil {
				slog.Error("Web dashboard server stopped", "error", err)
			}
		}()
	}

	// 11. Run daemon or serve-only mode
	if *serveOnly {
		slog.Info("Running in dashboard serve-only mode (background scraper disabled). Press Ctrl+C to stop.")
		<-ctx.Done()
	} else {
		sched.Start(ctx)
	}

	// 12. Graceful shutdown for web server
	if webServer != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = webServer.Shutdown(shutdownCtx)
	}

	slog.Info("ConnectClip Finder shutdown gracefully")
}
