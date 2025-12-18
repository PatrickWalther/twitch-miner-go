package miner

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/PatrickWalther/twitch-miner-go/internal/analytics"
	"github.com/PatrickWalther/twitch-miner-go/internal/api"
	"github.com/PatrickWalther/twitch-miner-go/internal/auth"
	"github.com/PatrickWalther/twitch-miner-go/internal/chat"
	"github.com/PatrickWalther/twitch-miner-go/internal/config"
	"github.com/PatrickWalther/twitch-miner-go/internal/drops"
	"github.com/PatrickWalther/twitch-miner-go/internal/models"
	"github.com/PatrickWalther/twitch-miner-go/internal/pubsub"
	"github.com/PatrickWalther/twitch-miner-go/internal/settings"
	"github.com/PatrickWalther/twitch-miner-go/internal/watcher"
)

type Miner struct {
	config     *config.Config
	configPath string
	auth       *auth.TwitchAuth
	client     *api.TwitchClient
	streamers  []*models.Streamer

	wsPool       *pubsub.WebSocketPool
	chatManager  *chat.ChatManager
	watcher      *watcher.MinuteWatcher
	dropsTracker *drops.DropsTracker
	analytics    *analytics.AnalyticsServer

	deviceID string
	running  bool
	stopChan chan struct{}

	mu sync.RWMutex
}

func New(cfg *config.Config, configPath string) *Miner {
	deviceID := generateDeviceID()

	return &Miner{
		config:     cfg,
		configPath: configPath,
		deviceID:   deviceID,
		stopChan:   make(chan struct{}),
	}
}

func (m *Miner) Run() error {
	if err := m.initialize(); err != nil {
		return fmt.Errorf("initialization failed: %w", err)
	}

	if err := m.authenticate(); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	if err := m.loadStreamers(); err != nil {
		return fmt.Errorf("failed to load streamers: %w", err)
	}

	m.setupComponents()

	if err := m.subscribeToTopics(); err != nil {
		return fmt.Errorf("failed to subscribe to topics: %w", err)
	}

	m.startMining()

	m.waitForShutdown()

	return nil
}

func (m *Miner) initialize() error {
	slog.Info("Initializing Twitch Channel Points Miner")

	if err := os.MkdirAll("cookies", 0755); err != nil {
		return fmt.Errorf("failed to create cookies directory: %w", err)
	}
	if err := os.MkdirAll("logs", 0755); err != nil {
		return fmt.Errorf("failed to create logs directory: %w", err)
	}

	if m.config.EnableAnalytics {
		if err := os.MkdirAll("analytics", 0755); err != nil {
			return fmt.Errorf("failed to create analytics directory: %w", err)
		}
	}

	return nil
}

func (m *Miner) authenticate() error {
	slog.Info("Authenticating with Twitch")

	m.auth = auth.NewTwitchAuth(m.config.Username, m.deviceID)

	if err := m.auth.Login(); err != nil {
		return err
	}

	m.client = api.NewTwitchClient(m.auth, m.deviceID)
	m.client.UpdateClientVersion()

	userID, err := m.client.GetChannelID(m.config.Username)
	if err != nil {
		return fmt.Errorf("failed to get user ID: %w", err)
	}
	m.auth.SetUserID(userID)

	if err := m.auth.SaveAuth(); err != nil {
		slog.Warn("Failed to save auth", "error", err)
	}

	slog.Info("Authentication successful", "username", m.config.Username, "userID", userID)
	return nil
}

func (m *Miner) loadStreamers() error {
	slog.Info("Loading streamers", "count", len(m.config.Streamers))

	for _, sc := range m.config.Streamers {
		settings := m.config.StreamerSettings
		if sc.Settings != nil {
			settings = *sc.Settings
		}

		streamer := models.NewStreamer(strings.ToLower(sc.Username), settings)

		channelID, err := m.client.GetChannelID(streamer.Username)
		if err != nil {
			slog.Warn("Streamer not found, skipping", "username", sc.Username, "error", err)
			continue
		}
		streamer.ChannelID = channelID

		if err := m.client.LoadChannelPointsContext(streamer); err != nil {
			slog.Warn("Failed to load channel points", "streamer", streamer.Username, "error", err)
		}

		m.streamers = append(m.streamers, streamer)
		slog.Info("Loaded streamer",
			"username", streamer.Username,
			"channelID", streamer.ChannelID,
			"points", streamer.GetChannelPoints(),
		)
	}

	if len(m.streamers) == 0 {
		return fmt.Errorf("no valid streamers found")
	}

	return nil
}

func (m *Miner) setupComponents() {
	m.wsPool = pubsub.NewWebSocketPool(m.client, m.auth.GetAuthToken(), m.streamers)
	m.wsPool.SetMessageHandler(m.handlePubSubMessage)

	if m.config.EnableAnalytics {
		m.analytics = analytics.NewAnalyticsServer(
			m.config.Analytics,
			m.config.Username,
			m.streamers,
		)
		if m.analytics != nil {
			m.analytics.SetSettingsProvider(m)
			m.analytics.SetSettingsUpdateCallback(m.ApplySettings)
		}
	}

	var chatLogger chat.ChatLogger
	chatLogsEnabled := m.config.EnableAnalytics && m.config.Analytics.EnableChatLogs
	slog.Debug("Chat logging config", "enableAnalytics", m.config.EnableAnalytics, "enableChatLogs", m.config.Analytics.EnableChatLogs, "chatLogsEnabled", chatLogsEnabled)
	if chatLogsEnabled && m.analytics != nil {
		chatLogger = analytics.NewChatLoggerAdapter(m.analytics)
	}
	m.chatManager = chat.NewChatManager(m.config.Username, m.auth.GetAuthToken(), chatLogger, chatLogsEnabled)

	m.watcher = watcher.NewMinuteWatcher(
		m.client,
		m.streamers,
		m.config.Priority,
		m.config.RateLimits,
	)

	m.dropsTracker = drops.NewDropsTracker(
		m.client,
		m.streamers,
		m.config.RateLimits,
	)

	if m.config.ClaimDropsOnStartup {
		slog.Info("Claiming all drops from inventory on startup")
	}
}

func (m *Miner) subscribeToTopics() error {
	slog.Info("Subscribing to PubSub topics")

	userID := m.auth.GetUserID()

	if err := m.wsPool.Submit(pubsub.NewTopic(pubsub.TopicCommunityPointsUser, userID)); err != nil {
		return err
	}
	if err := m.wsPool.Submit(pubsub.NewTopic(pubsub.TopicPredictionsUser, userID)); err != nil {
		return err
	}

	for _, streamer := range m.streamers {
		channelID := streamer.ChannelID

		_ = m.wsPool.Submit(pubsub.NewTopic(pubsub.TopicVideoPlaybackByID, channelID))

		if streamer.Settings.FollowRaid {
			_ = m.wsPool.Submit(pubsub.NewTopic(pubsub.TopicRaid, channelID))
		}

		if streamer.Settings.MakePredictions {
			_ = m.wsPool.Submit(pubsub.NewTopic(pubsub.TopicPredictionsChannel, channelID))
		}

		if streamer.Settings.ClaimMoments {
			_ = m.wsPool.Submit(pubsub.NewTopic(pubsub.TopicCommunityMomentsChannel, channelID))
		}

		if streamer.Settings.CommunityGoals {
			_ = m.wsPool.Submit(pubsub.NewTopic(pubsub.TopicCommunityPointsChannel, channelID))
		}
	}

	return nil
}

func (m *Miner) startMining() {
	m.mu.Lock()
	m.running = true
	m.mu.Unlock()

	slog.Info("Starting mining operations")

	for _, streamer := range m.streamers {
		m.client.CheckStreamerOnline(streamer)
		m.chatManager.ToggleChat(streamer)
	}

	m.watcher.Start()
	m.dropsTracker.Start()

	if m.analytics != nil {
		m.analytics.Start()
	}

	go m.streamCheckLoop()
}

func (m *Miner) streamCheckLoop() {
	interval := time.Duration(m.config.RateLimits.StreamCheckInterval) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			return
		case <-ticker.C:
			for _, streamer := range m.streamers {
				m.client.CheckStreamerOnline(streamer)
				m.chatManager.ToggleChat(streamer)
			}
		}
	}
}

func (m *Miner) handlePubSubMessage(msg *pubsub.PubSubMessage, streamer *models.Streamer) {
	if m.analytics == nil {
		return
	}

	switch msg.Topic.Type {
	case pubsub.TopicCommunityPointsUser:
		if msg.Type == "points-earned" {
			if data := msg.Data; data != nil {
				if pointGain, ok := data["point_gain"].(map[string]interface{}); ok {
					if reasonCode, ok := pointGain["reason_code"].(string); ok {
						m.analytics.RecordPoints(streamer, reasonCode)

						if reasonCode == "WATCH_STREAK" {
							if earned, ok := pointGain["total_points"].(float64); ok {
								m.analytics.RecordAnnotation(streamer, "WATCH_STREAK", fmt.Sprintf("+%d - Watch Streak", int(earned)))
							}
						}
					}
				}
			}
		} else if msg.Type == "points-spent" {
			m.analytics.RecordPoints(streamer, "Spent")
		}

	case pubsub.TopicPredictionsUser:
		if msg.Type == "prediction-made" {
			m.analytics.RecordAnnotation(streamer, "PREDICTION_MADE", "Prediction placed")
		} else if msg.Type == "prediction-result" {
			if data := msg.Data; data != nil {
				if prediction, ok := data["prediction"].(map[string]interface{}); ok {
					if result, ok := prediction["result"].(map[string]interface{}); ok {
						if resultType, ok := result["type"].(string); ok {
							m.analytics.RecordAnnotation(streamer, resultType, "Prediction "+resultType)
						}
					}
				}
			}
		}
	}
}

func (m *Miner) waitForShutdown() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan
	slog.Info("Shutting down...")

	m.stop()
}

func (m *Miner) stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.running = false
	m.mu.Unlock()

	close(m.stopChan)

	m.chatManager.Close()
	m.wsPool.Close()
	m.watcher.Stop()
	m.dropsTracker.Stop()

	if m.analytics != nil {
		m.analytics.Stop()
	}

	m.printSessionReport()
}

func (m *Miner) printSessionReport() {
	slog.Info("=== Session Report ===")

	for _, streamer := range m.streamers {
		slog.Info("Streamer stats",
			"username", streamer.Username,
			"points", streamer.GetChannelPoints(),
		)

		for reason, entry := range streamer.History {
			if entry.Counter > 0 || entry.Amount != 0 {
				slog.Info("  History",
					"reason", reason,
					"count", entry.Counter,
					"amount", entry.Amount,
				)
			}
		}
	}
}

func generateDeviceID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(b)
}

// GetRuntimeSettings returns a snapshot of the current runtime configuration.
// It implements settings.SettingsProvider and is safe for concurrent use.
func (m *Miner) GetRuntimeSettings() settings.RuntimeSettings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return settings.BuildRuntimeSettings(m.config)
}

// GetDefaultSettings returns factory default settings with the current streamer list preserved.
// It implements settings.SettingsProvider and is used for the "Reset to Defaults" feature.
func (m *Miner) GetDefaultSettings() settings.RuntimeSettings {
	m.mu.RLock()
	currentStreamers := m.config.Streamers
	m.mu.RUnlock()
	return settings.BuildDefaultSettings(currentStreamers)
}

// ApplySettings updates the in-memory config, propagates changes to watchers
// and streamers, and persists them to the config file. It is called by the
// analytics server in response to UI changes and is safe to call while mining
// is running. Note: Adding/removing streamers requires a restart to take effect
// on the active streaming connections.
func (m *Miner) ApplySettings(s settings.RuntimeSettings) {
	m.mu.Lock()
	defer m.mu.Unlock()

	settings.ApplyToConfig(m.config, s)

	if m.watcher != nil {
		m.watcher.UpdateSettings(m.config.Priority, m.config.RateLimits)
	}

	for _, streamer := range m.streamers {
		for _, sc := range m.config.Streamers {
			if streamer.Username == sc.Username {
				if sc.Settings != nil {
					streamer.SetSettings(*sc.Settings)
				} else {
					streamer.SetSettings(m.config.StreamerSettings)
				}
				break
			}
		}
	}

	if m.configPath != "" {
		if err := config.SaveConfig(m.configPath, m.config); err != nil {
			slog.Error("Failed to save config", "error", err)
		} else {
			slog.Info("Settings saved to config file")
		}
	}

	slog.Info("Runtime settings updated")
}
