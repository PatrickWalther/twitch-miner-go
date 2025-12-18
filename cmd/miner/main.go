package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/PatrickWalther/twitch-miner-go/internal/analytics"
	"github.com/PatrickWalther/twitch-miner-go/internal/config"
	"github.com/PatrickWalther/twitch-miner-go/internal/logger"
	"github.com/PatrickWalther/twitch-miner-go/internal/miner"
	"github.com/PatrickWalther/twitch-miner-go/internal/models"
	"github.com/PatrickWalther/twitch-miner-go/internal/version"
)

var (
	configFile = flag.String("config", "config.json", "Path to configuration file")
	debug      = flag.Bool("debug", false, "Enable debug logging")
	genConfig  = flag.Bool("generate-config", false, "Generate a sample configuration file")
)

func main() {
	flag.Parse()

	if *genConfig {
		setupBasicLogger(*debug)
		generateSampleConfig()
		return
	}

	cfg, err := loadOrCreateConfig(*configFile)
	if err != nil {
		setupBasicLogger(*debug)
		slog.Error("Failed to load configuration", "error", err)
		os.Exit(1)
	}

	if cfg.Username == "" {
		setupBasicLogger(*debug)
		slog.Error("Username is required in configuration")
		os.Exit(1)
	}

	if len(cfg.Streamers) == 0 {
		setupBasicLogger(*debug)
		slog.Error("At least one streamer is required in configuration")
		os.Exit(1)
	}

	logSettings := cfg.Logger
	if *debug {
		logSettings.ConsoleLevel = "DEBUG"
		logSettings.FileLevel = "DEBUG"
	}

	log, err := logger.Setup(cfg.Username, logSettings)
	if err != nil {
		setupBasicLogger(*debug)
		slog.Error("Failed to setup logger", "error", err)
		os.Exit(1)
	}
	defer log.Close()

	slog.Info("Twitch Channel Points Miner", "version", version.Version)

	var analyticsServer *analytics.AnalyticsServer
	if cfg.EnableAnalytics {
		if err := os.MkdirAll("analytics", 0755); err != nil {
			slog.Error("Failed to create analytics directory", "error", err)
			os.Exit(1)
		}
		analyticsServer = analytics.NewAnalyticsServerEarly(cfg.Analytics, cfg.Username)
		if analyticsServer != nil {
			analyticsServer.Start()
			defer analyticsServer.Stop()
		}
	}

	m := miner.New(cfg, *configFile)
	if analyticsServer != nil {
		m.SetAnalyticsServer(analyticsServer)
	}
	if err := m.Run(); err != nil {
		slog.Error("Miner error", "error", err)
		os.Exit(1)
	}
}

func setupBasicLogger(debug bool) {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}

	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})
	slog.SetDefault(slog.New(handler))
}

func loadOrCreateConfig(path string) (*config.Config, error) {
	cfg, err := config.LoadConfig(path)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Warn("Configuration file not found, creating sample", "path", path)
			return nil, fmt.Errorf("configuration file not found: %s. Run with -generate-config to create a sample", path)
		}
		return nil, err
	}
	return cfg, nil
}

func generateSampleConfig() {
	cfg := config.DefaultConfig()
	cfg.Username = "your_twitch_username"
	cfg.EnableAnalytics = true
	cfg.Priority = []config.Priority{
		config.PriorityStreak,
		config.PriorityDrops,
		config.PriorityOrder,
	}
	cfg.Streamers = []config.StreamerConfig{
		{
			Username: "streamer1",
		},
		{
			Username: "streamer2",
			Settings: &models.StreamerSettings{
				MakePredictions: true,
				FollowRaid:      true,
				ClaimDrops:      true,
				ClaimMoments:    true,
				WatchStreak:     true,
				CommunityGoals:  false,
				Chat:            models.ChatOnline,
				Bet: models.BetSettings{
					Strategy:      models.StrategySmart,
					Percentage:    5,
					PercentageGap: 20,
					MaxPoints:     50000,
					MinimumPoints: 0,
					StealthMode:   false,
					Delay:         6,
					DelayMode:     models.DelayModeFromEnd,
				},
			},
		},
	}

	if err := config.SaveConfig("config.sample.json", &cfg); err != nil {
		slog.Error("Failed to save sample configuration", "error", err)
		os.Exit(1)
	}

	slog.Info("Sample configuration generated", "path", "config.sample.json")
	fmt.Println("\nSample configuration saved to config.sample.json")
	fmt.Println("Rename it to config.json and update with your settings")
}
