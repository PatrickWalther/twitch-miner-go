package miner

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/PatrickWalther/twitch-miner-go/internal/analytics"
	"github.com/PatrickWalther/twitch-miner-go/internal/api"
	"github.com/PatrickWalther/twitch-miner-go/internal/auth"
	"github.com/PatrickWalther/twitch-miner-go/internal/chat"
	"github.com/PatrickWalther/twitch-miner-go/internal/config"
	"github.com/PatrickWalther/twitch-miner-go/internal/database"
	"github.com/PatrickWalther/twitch-miner-go/internal/drops"
	"github.com/PatrickWalther/twitch-miner-go/internal/models"
	"github.com/PatrickWalther/twitch-miner-go/internal/notifications"
	"github.com/PatrickWalther/twitch-miner-go/internal/pubsub"
	"github.com/PatrickWalther/twitch-miner-go/internal/settings"
	"github.com/PatrickWalther/twitch-miner-go/internal/util"
	"github.com/PatrickWalther/twitch-miner-go/internal/watcher"
	"github.com/PatrickWalther/twitch-miner-go/internal/web"
)

type Miner struct {
	config     *config.Config
	configPath string
	auth       *auth.TwitchAuth
	client     *api.TwitchClient
	streamers  []*models.Streamer

	db            *database.DB
	dbBasePath    string
	wsPool        *pubsub.WebSocketPool
	chatManager   *chat.ChatManager
	watcher       *watcher.MinuteWatcher
	dropsTracker  *drops.DropsTracker
	analyticsSvc  *analytics.Service
	webServer     *web.Server
	notifications *notifications.Manager

	deviceID          string
	running           bool
	stopChan          chan struct{}
	externalAnalytics bool

	mu sync.RWMutex
}

func New(cfg *config.Config, configPath string) *Miner {
	deviceID := util.DeviceID()

	return &Miner{
		config:     cfg,
		configPath: configPath,
		deviceID:   deviceID,
		stopChan:   make(chan struct{}),
	}
}

func (m *Miner) SetAnalyticsService(svc *analytics.Service) {
	m.analyticsSvc = svc
	m.externalAnalytics = true
}

func (m *Miner) SetWebServer(server *web.Server) {
	m.webServer = server
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

	m.dbBasePath = filepath.Join("database", m.config.Username)
	if err := os.MkdirAll(m.dbBasePath, 0755); err != nil {
		return fmt.Errorf("failed to create database directory: %w", err)
	}

	db, err := database.Open(m.dbBasePath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	m.db = db

	return nil
}

func (m *Miner) authenticate() error {
	slog.Info("Authenticating with Twitch")

	m.auth = auth.NewTwitchAuth(m.config.Username, m.deviceID)

	if m.webServer != nil {
		broadcaster := m.webServer.GetStatusBroadcaster()
		m.auth.SetEventCallback(func(event auth.AuthEvent) {
			switch event.Type {
			case auth.AuthEventCode:
				broadcaster.SetAuthRequired(event.VerificationURI, event.UserCode, event.ExpiresIn)
			case auth.AuthEventCompleted:
				broadcaster.SetStatus(web.StatusLoadingStreamers, "Loading streamers...")
			case auth.AuthEventError:
				if event.Error != nil {
					broadcaster.SetStatus(web.StatusError, event.Error.Error())
				}
			}
		})
	}

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

	var broadcaster *web.StatusBroadcaster
	if m.webServer != nil {
		broadcaster = m.webServer.GetStatusBroadcaster()
		broadcaster.SetStatus(web.StatusLoadingStreamers, "Loading streamers...")
	}

	total := len(m.config.Streamers)
	for i, sc := range m.config.Streamers {
		if broadcaster != nil {
			broadcaster.SetStreamerProgress(i+1, total, sc.Username)
		}

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
	m.wsPool = pubsub.NewWebSocketPool(m.client, m.auth.GetAuthToken(), m.streamers, m.config.RateLimits)
	m.wsPool.SetMessageHandler(m.handlePubSubMessage)
	m.wsPool.SetStatusHandler(m.handleStatusChange)

	if m.config.EnableAnalytics {
		if m.externalAnalytics && m.analyticsSvc != nil {
			if m.webServer != nil {
				m.webServer.AttachStreamers(m.streamers)
				m.webServer.SetSettingsProvider(m)
				m.webServer.SetSettingsUpdateCallback(m.ApplySettings)
			}
		} else {
			svc, err := analytics.NewService(m.db, m.dbBasePath)
			if err != nil {
				slog.Error("Failed to create analytics service", "error", err)
			} else {
				m.analyticsSvc = svc
			}

			m.webServer = web.NewServer(
				m.config.Analytics,
				m.config.Username,
				m.dbBasePath,
				m.analyticsSvc,
				m.streamers,
			)
			if m.webServer != nil {
				m.webServer.SetSettingsProvider(m)
				m.webServer.SetSettingsUpdateCallback(m.ApplySettings)
			}
		}
	}

	var streamerNames []string
	for _, st := range m.streamers {
		streamerNames = append(streamerNames, st.Username)
	}

	if m.config.Discord.Enabled {
		notifMgr, err := notifications.NewManager(&m.config.Discord, m.db, streamerNames)
		if err != nil {
			slog.Error("Failed to create notification manager", "error", err)
		} else {
			m.notifications = notifMgr

			streamerPoints := make(map[string]int)
			for _, st := range m.streamers {
				streamerPoints[st.Username] = st.GetChannelPoints()
			}
			m.notifications.InitializePointsTracking(streamerPoints)

			if err := m.notifications.Start(context.Background()); err != nil {
				slog.Error("Failed to start notification manager", "error", err)
			}
		}
	}

	if m.webServer != nil {
		m.webServer.SetDiscordEnabled(m.config.Discord.Enabled)
		if m.notifications != nil {
			m.webServer.SetNotificationManager(m.notifications)
		}
	}

	var mentionHandler chat.MentionHandler
	if m.notifications != nil {
		mentionHandler = m.notifications.NotifyMention
	}

	var chatLogger chat.ChatLogger
	chatLogsEnabled := m.config.EnableAnalytics && m.config.Analytics.EnableChatLogs
	slog.Debug("Chat logging config", "enableAnalytics", m.config.EnableAnalytics, "enableChatLogs", m.config.Analytics.EnableChatLogs, "chatLogsEnabled", chatLogsEnabled)
	if chatLogsEnabled && m.analyticsSvc != nil {
		chatLogger = analytics.NewChatLoggerAdapter(m.analyticsSvc)
	}
	m.chatManager = chat.NewChatManager(m.config.Username, m.auth.GetAuthToken(), chatLogger, chatLogsEnabled, mentionHandler)

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

	if m.webServer != nil {
		if !m.externalAnalytics {
			m.webServer.Start()
		}
		m.webServer.GetStatusBroadcaster().SetStatus(web.StatusRunning, "Mining active")
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
	switch msg.Topic.Type {
	case pubsub.TopicCommunityPointsUser:
		switch msg.Type {
		case "points-earned":
			if data := msg.Data; data != nil {
				if pointGain, ok := data["point_gain"].(map[string]interface{}); ok {
					if reasonCode, ok := pointGain["reason_code"].(string); ok {
						if m.analyticsSvc != nil {
							m.analyticsSvc.RecordPoints(streamer, reasonCode)

							if reasonCode == "WATCH_STREAK" {
								if earned, ok := pointGain["total_points"].(float64); ok {
									m.analyticsSvc.RecordAnnotation(streamer, "WATCH_STREAK", fmt.Sprintf("+%d - Watch Streak", int(earned)))
								}
							}
						}
					}
				}
			}

			if m.notifications != nil {
				m.notifications.NotifyPointsReached(streamer.Username, streamer.GetChannelPoints())
			}
		case "points-spent":
			if m.analyticsSvc != nil {
				m.analyticsSvc.RecordPoints(streamer, "Spent")
			}
		}

	case pubsub.TopicPredictionsUser:
		if m.analyticsSvc == nil {
			return
		}
		switch msg.Type {
		case "prediction-made":
			m.analyticsSvc.RecordAnnotation(streamer, "PREDICTION_MADE", "Prediction placed")
		case "prediction-result":
			if data := msg.Data; data != nil {
				if prediction, ok := data["prediction"].(map[string]interface{}); ok {
					if result, ok := prediction["result"].(map[string]interface{}); ok {
						if resultType, ok := result["type"].(string); ok {
							m.analyticsSvc.RecordAnnotation(streamer, resultType, "Prediction "+resultType)
						}
					}
				}
			}
		}
	}
}

func (m *Miner) handleStatusChange(streamer string, online bool) {
	if m.notifications == nil {
		return
	}

	if online {
		m.notifications.NotifyOnline(streamer)
	} else {
		m.notifications.NotifyOffline(streamer)
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

	if m.webServer != nil {
		m.webServer.Stop()
	}

	if m.analyticsSvc != nil {
		_ = m.analyticsSvc.Close()
	}

	if m.notifications != nil {
		m.notifications.Stop()
	}

	if m.db != nil {
		_ = m.db.Close()
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

func (m *Miner) GetRuntimeSettings() settings.RuntimeSettings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return settings.BuildRuntimeSettings(m.config)
}

func (m *Miner) GetDefaultSettings() settings.RuntimeSettings {
	m.mu.RLock()
	currentStreamers := m.config.Streamers
	m.mu.RUnlock()
	return settings.BuildDefaultSettings(currentStreamers)
}

func (m *Miner) ApplySettings(s settings.RuntimeSettings) {
	m.mu.Lock()

	oldDiscordEnabled := m.config.Discord.Enabled
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

	discordCfg := m.config.Discord
	notifMgr := m.notifications
	webServer := m.webServer

	m.mu.Unlock()

	if notifMgr != nil {
		if err := notifMgr.UpdateDiscordConfig(&discordCfg); err != nil {
			slog.Error("Failed to update Discord config", "error", err)
		}
	} else if discordCfg.Enabled && !oldDiscordEnabled {
		var streamerNames []string
		for _, st := range m.streamers {
			streamerNames = append(streamerNames, st.Username)
		}

		newNotifMgr, err := notifications.NewManager(&discordCfg, m.db, streamerNames)
		if err != nil {
			slog.Error("Failed to create notification manager", "error", err)
		} else {
			m.mu.Lock()
			m.notifications = newNotifMgr
			m.mu.Unlock()

			streamerPoints := make(map[string]int)
			for _, st := range m.streamers {
				streamerPoints[st.Username] = st.GetChannelPoints()
			}
			newNotifMgr.InitializePointsTracking(streamerPoints)

			if err := newNotifMgr.Start(context.Background()); err != nil {
				slog.Error("Failed to start notification manager", "error", err)
			}

			if webServer != nil {
				webServer.SetNotificationManager(newNotifMgr)
			}
		}
	}

	if webServer != nil {
		webServer.SetDiscordEnabled(discordCfg.Enabled)
	}

	m.mu.Lock()
	if m.configPath != "" {
		if err := config.SaveConfig(m.configPath, m.config); err != nil {
			slog.Error("Failed to save config", "error", err)
		} else {
			slog.Info("Settings saved to config file")
		}
	}
	m.mu.Unlock()

	slog.Info("Runtime settings updated")
}
