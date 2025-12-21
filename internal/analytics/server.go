package analytics

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PatrickWalther/twitch-miner-go/internal/config"
	"github.com/PatrickWalther/twitch-miner-go/internal/database"
	"github.com/PatrickWalther/twitch-miner-go/internal/models"
	"github.com/PatrickWalther/twitch-miner-go/internal/notifications"
	"github.com/PatrickWalther/twitch-miner-go/internal/settings"
	"github.com/PatrickWalther/twitch-miner-go/internal/version"
)

//go:embed templates/*.html templates/partials/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

type AnalyticsServer struct {
	host           string
	port           int
	refresh        int
	daysAgo        int
	username       string
	basePath       string
	streamers      []*models.Streamer
	discordEnabled bool

	db                  *database.DB
	repo                Repository
	server              *http.Server
	templates           map[string]*template.Template
	settingsProvider    settings.SettingsProvider
	onSettingsUpdate    settings.SettingsUpdateCallback
	notificationManager *notifications.Manager
	status              *StatusBroadcaster
	ready               bool
	mu                  sync.RWMutex
}

func NewAnalyticsServer(analyticsSettings config.AnalyticsSettings, username string, basePath string, db *database.DB, streamers []*models.Streamer) *AnalyticsServer {
	repo, err := NewSQLiteRepository(db, basePath)
	if err != nil {
		slog.Error("Failed to create analytics repository", "error", err)
		return nil
	}

	templates := make(map[string]*template.Template)

	pages := []string{"dashboard.html", "streamer.html", "settings.html", "notifications.html"}
	for _, page := range pages {
		tmpl, err := template.ParseFS(templatesFS,
			"templates/base.html",
			"templates/"+page,
			"templates/partials/*.html",
		)
		if err != nil {
			slog.Error("Failed to parse template", "page", page, "error", err)
			continue
		}
		templates[page] = tmpl
	}

	partials, err := template.ParseFS(templatesFS, "templates/partials/*.html")
	if err != nil {
		slog.Error("Failed to parse partials", "error", err)
	} else {
		templates["partials"] = partials
	}

	return &AnalyticsServer{
		host:      analyticsSettings.Host,
		port:      analyticsSettings.Port,
		refresh:   analyticsSettings.Refresh,
		daysAgo:   analyticsSettings.DaysAgo,
		username:  username,
		basePath:  basePath,
		streamers: streamers,
		db:        db,
		repo:      repo,
		templates: templates,
		status:    NewStatusBroadcaster(),
		ready:     len(streamers) > 0,
	}
}

func NewAnalyticsServerEarly(analyticsSettings config.AnalyticsSettings, username string, basePath string, db *database.DB) *AnalyticsServer {
	repo, err := NewSQLiteRepository(db, basePath)
	if err != nil {
		slog.Error("Failed to create analytics repository", "error", err)
		return nil
	}

	templates := make(map[string]*template.Template)

	pages := []string{"dashboard.html", "streamer.html", "settings.html", "notifications.html"}
	for _, page := range pages {
		tmpl, err := template.ParseFS(templatesFS,
			"templates/base.html",
			"templates/"+page,
			"templates/partials/*.html",
		)
		if err != nil {
			slog.Error("Failed to parse template", "page", page, "error", err)
			continue
		}
		templates[page] = tmpl
	}

	partials, err := template.ParseFS(templatesFS, "templates/partials/*.html")
	if err != nil {
		slog.Error("Failed to parse partials", "error", err)
	} else {
		templates["partials"] = partials
	}

	return &AnalyticsServer{
		host:      analyticsSettings.Host,
		port:      analyticsSettings.Port,
		refresh:   analyticsSettings.Refresh,
		daysAgo:   analyticsSettings.DaysAgo,
		username:  username,
		basePath:  basePath,
		streamers: nil,
		db:        db,
		repo:      repo,
		templates: templates,
		status:    NewStatusBroadcaster(),
		ready:     false,
	}
}

func (s *AnalyticsServer) AttachStreamers(streamers []*models.Streamer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.streamers = streamers
	s.ready = true
}

func (s *AnalyticsServer) GetStatusBroadcaster() *StatusBroadcaster {
	return s.status
}

func (s *AnalyticsServer) GetDB() *database.DB {
	return s.db
}

func (s *AnalyticsServer) GetBasePath() string {
	return s.basePath
}

func (s *AnalyticsServer) SetSettingsProvider(provider settings.SettingsProvider) {
	s.settingsProvider = provider
}

func (s *AnalyticsServer) SetSettingsUpdateCallback(callback settings.SettingsUpdateCallback) {
	s.onSettingsUpdate = callback
}

func (s *AnalyticsServer) SetNotificationManager(mgr *notifications.Manager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notificationManager = mgr
}

func (s *AnalyticsServer) SetDiscordEnabled(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.discordEnabled = enabled
}

func (s *AnalyticsServer) Start() {
	mux := http.NewServeMux()

	// Serve static files from embedded filesystem
	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		slog.Error("Failed to create static filesystem", "error", err)
	} else {
		mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))
	}

	mux.HandleFunc("/", s.handleDashboard)
	mux.HandleFunc("/settings", s.handleSettingsPage)
	mux.HandleFunc("/streamer/", s.handleStreamerPage)
	mux.HandleFunc("/api/streamers", s.handleAPIStreamers)
	mux.HandleFunc("/api/status", s.handleAPIStatus)
	mux.HandleFunc("/api/miner-status", s.handleAPIMinerStatus)
	mux.HandleFunc("/api/miner-status/stream", s.handleAPIMinerStatusStream)
	mux.HandleFunc("/api/settings", s.handleAPISettings)
	mux.HandleFunc("/api/settings/reset", s.handleAPISettingsReset)

	mux.HandleFunc("/streamers", s.handleStreamers)
	mux.HandleFunc("/json/", s.handleJSON)
	mux.HandleFunc("/json_all", s.handleJSONAll)
	mux.HandleFunc("/api/chat/", s.handleAPIChatMessages)

	mux.HandleFunc("/notifications", s.handleNotificationsPage)
	mux.HandleFunc("/api/notifications/config", s.handleAPINotificationsConfig)
	mux.HandleFunc("/api/notifications/channels", s.handleAPINotificationsChannels)
	mux.HandleFunc("/api/notifications/points", s.handleAPINotificationsPoints)
	mux.HandleFunc("/api/notifications/points/", s.handleAPINotificationsPointsDelete)
	mux.HandleFunc("/api/notifications/test", s.handleAPINotificationsTest)

	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	s.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	slog.Info("Analytics server starting", "url", "http://"+addr+"/")

	go func() {
		if err := s.server.ListenAndServe(); err != http.ErrServerClosed {
			slog.Error("Analytics server error", "error", err)
		}
	}()
}

func (s *AnalyticsServer) Stop() {
	if s.server != nil {
		_ = s.server.Close()
	}
	if s.repo != nil {
		_ = s.repo.Close()
	}
}

func (s *AnalyticsServer) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	streamers, err := s.repo.ListStreamers()
	if err != nil {
		slog.Error("Failed to list streamers", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	totalPoints := 0
	pointsToday := 0
	todayStart := time.Now().Truncate(24 * time.Hour)

	for _, info := range streamers {
		totalPoints += info.Points

		data, err := s.repo.GetStreamerData(info.Name)
		if err != nil {
			continue
		}
		todayStartMS := todayStart.UnixMilli()
		for i := len(data.Series) - 1; i >= 0; i-- {
			if data.Series[i].X < todayStartMS {
				if i+1 < len(data.Series) {
					pointsToday += info.Points - data.Series[i+1].Y
				}
				break
			}
			if i == 0 && len(data.Series) > 0 {
				pointsToday += info.Points - data.Series[0].Y
			}
		}
	}

	s.mu.RLock()
	refresh := s.refresh
	discordEnabled := s.discordEnabled
	s.mu.RUnlock()

	data := DashboardData{
		Username:       s.username,
		RefreshMinutes: refresh,
		Version:        version.Version,
		TotalPoints:    formatNumber(totalPoints),
		StreamerCount:  len(streamers),
		PointsToday:    formatNumber(pointsToday),
		DiscordEnabled: discordEnabled,
	}

	s.renderPage(w, "dashboard.html", data)
}

func (s *AnalyticsServer) handleStreamerPage(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/streamer/")
	if name == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	data, err := s.repo.GetStreamerData(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	currentPoints := 0
	if len(data.Series) > 0 {
		currentPoints = data.Series[len(data.Series)-1].Y
	}

	s.mu.RLock()
	refresh := s.refresh
	daysAgo := s.daysAgo
	discordEnabled := s.discordEnabled
	s.mu.RUnlock()

	startTS := time.Now().AddDate(0, 0, -daysAgo).UnixMilli()
	pointsGained := 0
	for i, p := range data.Series {
		if p.X >= startTS {
			if i > 0 {
				pointsGained = currentPoints - data.Series[i-1].Y
			} else {
				pointsGained = currentPoints - p.Y
			}
			break
		}
	}

	pageData := StreamerPageData{
		Username:       s.username,
		RefreshMinutes: refresh,
		Version:        version.Version,
		Streamer: StreamerInfo{
			Name:            name,
			Points:          currentPoints,
			PointsFormatted: formatNumber(currentPoints),
		},
		PointsGained:   formatNumber(pointsGained),
		DataPoints:     len(data.Series),
		DaysAgo:        daysAgo,
		DiscordEnabled: discordEnabled,
	}

	s.renderPage(w, "streamer.html", pageData)
}

func (s *AnalyticsServer) handleAPIStreamers(w http.ResponseWriter, r *http.Request) {
	streamers, err := s.repo.ListStreamers()
	if err != nil {
		http.Error(w, "Failed to list streamers", http.StatusInternalServerError)
		return
	}

	streamerMap := make(map[string]*models.Streamer)
	configOrder := make(map[string]int)
	for i, st := range s.streamers {
		streamerMap[st.Username] = st
		configOrder[st.Username] = i
	}

	var trackedLive, trackedOffline, untracked []StreamerInfo

	for i := range streamers {
		if st, ok := streamerMap[streamers[i].Name]; ok {
			streamers[i].IsLive = st.GetIsOnline()
			if streamers[i].IsLive {
				streamers[i].LiveDuration = formatDuration(time.Since(st.GetOnlineAt()))
				trackedLive = append(trackedLive, streamers[i])
			} else {
				offlineAt := st.GetOfflineAt()
				if !offlineAt.IsZero() {
					streamers[i].OfflineDuration = formatDuration(time.Since(offlineAt))
				}
				trackedOffline = append(trackedOffline, streamers[i])
			}
		} else {
			untracked = append(untracked, streamers[i])
		}
	}

	sort.Slice(trackedLive, func(i, j int) bool {
		return configOrder[trackedLive[i].Name] < configOrder[trackedLive[j].Name]
	})
	sort.Slice(trackedOffline, func(i, j int) bool {
		return configOrder[trackedOffline[i].Name] < configOrder[trackedOffline[j].Name]
	})
	sort.Slice(untracked, func(i, j int) bool {
		return untracked[i].Name < untracked[j].Name
	})

	gridData := StreamerGridData{
		TrackedLive:    trackedLive,
		TrackedOffline: trackedOffline,
		Untracked:      untracked,
	}

	w.Header().Set("Content-Type", "text/html")
	tmpl := s.templates["partials"]
	if tmpl == nil {
		http.Error(w, "Partials not loaded", http.StatusInternalServerError)
		return
	}
	if err := tmpl.ExecuteTemplate(w, "streamer_grid", gridData); err != nil {
		slog.Error("Failed to render streamer grid", "error", err)
		http.Error(w, "Failed to render", http.StatusInternalServerError)
	}
}

func (s *AnalyticsServer) handleAPIStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	_, _ = w.Write([]byte("Connected"))
}

func (s *AnalyticsServer) handleAPIMinerStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	status := s.status.GetStatus()
	_ = json.NewEncoder(w).Encode(status)
}

func (s *AnalyticsServer) handleAPIMinerStatusStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := s.status.Subscribe()
	defer s.status.Unsubscribe(ch)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case status, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(status)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

func (s *AnalyticsServer) handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	refresh := s.refresh
	discordEnabled := s.discordEnabled
	s.mu.RUnlock()

	data := SettingsPageData{
		Username:       s.username,
		RefreshMinutes: refresh,
		Version:        version.Version,
		DiscordEnabled: discordEnabled,
	}
	s.renderPage(w, "settings.html", data)
}

func (s *AnalyticsServer) handleAPISettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if s.settingsProvider == nil {
			http.Error(w, "Settings not available", http.StatusServiceUnavailable)
			return
		}
		settings := s.settingsProvider.GetRuntimeSettings()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(settings)
		return
	}

	if r.Method == http.MethodPost {
		var newSettings settings.RuntimeSettings
		if err := json.NewDecoder(r.Body).Decode(&newSettings); err != nil {
			http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		if s.onSettingsUpdate != nil {
			s.onSettingsUpdate(newSettings)
		}

		s.mu.Lock()
		s.refresh = newSettings.Analytics.Refresh
		s.daysAgo = newSettings.Analytics.DaysAgo
		s.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (s *AnalyticsServer) handleAPISettingsReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.settingsProvider == nil {
		http.Error(w, "Settings not available", http.StatusServiceUnavailable)
		return
	}

	defaults := s.settingsProvider.GetDefaultSettings()

	if s.onSettingsUpdate != nil {
		s.onSettingsUpdate(defaults)
	}

	s.mu.Lock()
	s.refresh = defaults.Analytics.Refresh
	s.daysAgo = defaults.Analytics.DaysAgo
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(defaults)
}

func (s *AnalyticsServer) renderPage(w http.ResponseWriter, page string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	tmpl, ok := s.templates[page]
	if !ok {
		slog.Error("Template not found", "page", page)
		http.Error(w, "Template not found", http.StatusInternalServerError)
		return
	}

	if err := tmpl.ExecuteTemplate(w, "base.html", data); err != nil {
		slog.Error("Failed to render page", "page", page, "error", err)
		http.Error(w, "Failed to render page", http.StatusInternalServerError)
	}
}

func (s *AnalyticsServer) handleStreamers(w http.ResponseWriter, r *http.Request) {
	streamers, err := s.repo.ListStreamers()
	if err != nil {
		http.Error(w, "Failed to list streamers", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(streamers)
}

func (s *AnalyticsServer) handleJSON(w http.ResponseWriter, r *http.Request) {
	streamer := strings.TrimPrefix(r.URL.Path, "/json/")
	streamer = strings.TrimSuffix(streamer, ".json")

	if streamer == "" {
		http.Error(w, "Streamer not specified", http.StatusBadRequest)
		return
	}

	startDate := r.URL.Query().Get("startDate")
	endDate := r.URL.Query().Get("endDate")

	var startTime, endTime time.Time
	if startDate != "" {
		if t, err := time.Parse("2006-01-02", startDate); err == nil {
			startTime = t
		}
	}
	if endDate != "" {
		if t, err := time.Parse("2006-01-02", endDate); err == nil {
			endTime = t.Add(24*time.Hour - time.Second)
		}
	}

	var data *StreamerData
	var err error
	if !startTime.IsZero() || !endTime.IsZero() {
		data, err = s.repo.GetStreamerDataFiltered(streamer, startTime, endTime)
	} else {
		data, err = s.repo.GetStreamerData(streamer)
	}

	if err != nil {
		http.Error(w, "Failed to get data", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

func (s *AnalyticsServer) handleJSONAll(w http.ResponseWriter, r *http.Request) {
	streamers, err := s.repo.ListStreamers()
	if err != nil {
		http.Error(w, "Failed to list streamers", http.StatusInternalServerError)
		return
	}

	type namedData struct {
		Name string       `json:"name"`
		Data StreamerData `json:"data"`
	}

	var result []namedData
	for _, info := range streamers {
		data, err := s.repo.GetStreamerData(info.Name)
		if err != nil {
			continue
		}
		result = append(result, namedData{Name: info.Name, Data: *data})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (s *AnalyticsServer) handleAPIChatMessages(w http.ResponseWriter, r *http.Request) {
	streamer := strings.TrimPrefix(r.URL.Path, "/api/chat/")
	if streamer == "" {
		http.Error(w, "Streamer not specified", http.StatusBadRequest)
		return
	}

	limit := 50
	offset := 0
	query := r.URL.Query().Get("q")

	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
			if limit > 200 {
				limit = 200
			}
		}
	}

	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	var data *ChatLogData
	var err error

	if query != "" {
		data, err = s.repo.SearchChatMessages(streamer, query, limit, offset)
	} else {
		data, err = s.repo.GetChatMessages(streamer, limit, offset)
	}

	if err != nil {
		http.Error(w, "Failed to get chat messages", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

func (s *AnalyticsServer) RecordPoints(streamer *models.Streamer, eventType string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	eventType = strings.ReplaceAll(eventType, "_", " ")
	if err := s.repo.RecordPoints(streamer.Username, streamer.GetChannelPoints(), eventType); err != nil {
		slog.Error("Failed to record points", "streamer", streamer.Username, "error", err)
	}
}

func (s *AnalyticsServer) RecordAnnotation(streamer *models.Streamer, eventType, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	colors := map[string]string{
		"WATCH_STREAK":    "#45c1ff",
		"PREDICTION_MADE": "#ffe045",
		"WIN":             "#36b535",
		"LOSE":            "#ff4545",
	}

	color, ok := colors[eventType]
	if !ok {
		return
	}

	if err := s.repo.RecordAnnotation(streamer.Username, eventType, text, color); err != nil {
		slog.Error("Failed to record annotation", "streamer", streamer.Username, "error", err)
	}
}

func (s *AnalyticsServer) RecordChatMessage(streamer string, username, displayName, message, emotes, badges, color string) error {
	msg := ChatMessage{
		Username:    username,
		DisplayName: displayName,
		Message:     message,
		Emotes:      emotes,
		Badges:      badges,
		Color:       color,
	}
	return s.repo.RecordChatMessage(streamer, msg)
}

func formatNumber(n int) string {
	if n == 0 {
		return "0"
	}

	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}

	s := fmt.Sprintf("%d", n)
	result := ""
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result += ","
		}
		result += string(c)
	}
	return sign + result
}

func formatTimeAgo(timestamp int64) string {
	if timestamp == 0 {
		return "Never"
	}

	seconds := (time.Now().UnixMilli() - timestamp) / 1000
	if seconds < 60 {
		return "Just now"
	}
	if seconds < 3600 {
		return fmt.Sprintf("%dm ago", seconds/60)
	}
	if seconds < 86400 {
		return fmt.Sprintf("%dh ago", seconds/3600)
	}
	return fmt.Sprintf("%dd ago", seconds/86400)
}

func formatDuration(d time.Duration) string {
	totalSeconds := int(d.Seconds())
	if totalSeconds < 60 {
		return fmt.Sprintf("%ds", totalSeconds)
	}
	if totalSeconds < 3600 {
		return fmt.Sprintf("%dm", totalSeconds/60)
	}
	if totalSeconds < 86400 {
		return fmt.Sprintf("%dh", totalSeconds/3600)
	}
	return fmt.Sprintf("%dd", totalSeconds/86400)
}

func (s *AnalyticsServer) handleNotificationsPage(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	refresh := s.refresh
	discordEnabled := s.discordEnabled
	notifMgr := s.notificationManager
	s.mu.RUnlock()

	if !discordEnabled {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	var streamers []string
	for _, st := range s.streamers {
		streamers = append(streamers, st.Username)
	}

	configValid := true
	configError := ""
	if notifMgr != nil {
		configValid, configError = notifMgr.IsConfigValid()
	}

	data := NotificationsPageData{
		Username:       s.username,
		RefreshMinutes: refresh,
		Version:        version.Version,
		DiscordEnabled: discordEnabled,
		ConfigValid:    configValid,
		ConfigError:    configError,
		Streamers:      streamers,
	}

	s.renderPage(w, "notifications.html", data)
}

func (s *AnalyticsServer) handleAPINotificationsConfig(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	notifMgr := s.notificationManager
	s.mu.RUnlock()

	if notifMgr == nil {
		http.Error(w, "Notifications not available", http.StatusServiceUnavailable)
		return
	}

	if r.Method == http.MethodGet {
		cfg, err := notifMgr.GetConfig()
		if err != nil {
			http.Error(w, "Failed to get config", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cfg)
		return
	}

	if r.Method == http.MethodPost {
		var cfg notifications.NotificationConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		if err := notifMgr.SaveConfig(&cfg); err != nil {
			http.Error(w, "Failed to save config", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (s *AnalyticsServer) handleAPINotificationsChannels(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	notifMgr := s.notificationManager
	s.mu.RUnlock()

	if notifMgr == nil {
		http.Error(w, "Notifications not available", http.StatusServiceUnavailable)
		return
	}

	forceRefresh := r.URL.Query().Get("refresh") == "1"
	channels, err := notifMgr.GetDiscordChannels(context.Background(), forceRefresh)
	if err != nil {
		http.Error(w, "Failed to get channels: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(channels)
}

func (s *AnalyticsServer) handleAPINotificationsPoints(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	notifMgr := s.notificationManager
	s.mu.RUnlock()

	if notifMgr == nil {
		http.Error(w, "Notifications not available", http.StatusServiceUnavailable)
		return
	}

	if r.Method == http.MethodGet {
		rules, err := notifMgr.GetPointRules()
		if err != nil {
			http.Error(w, "Failed to get rules", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rules)
		return
	}

	if r.Method == http.MethodPost {
		var rule notifications.PointRule
		if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
			http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		if err := notifMgr.AddPointRule(&rule); err != nil {
			http.Error(w, "Failed to add rule", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rule)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (s *AnalyticsServer) handleAPINotificationsPointsDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.RLock()
	notifMgr := s.notificationManager
	s.mu.RUnlock()

	if notifMgr == nil {
		http.Error(w, "Notifications not available", http.StatusServiceUnavailable)
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/api/notifications/points/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	if err := notifMgr.DeletePointRule(id); err != nil {
		http.Error(w, "Failed to delete rule", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *AnalyticsServer) handleAPINotificationsTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.RLock()
	notifMgr := s.notificationManager
	s.mu.RUnlock()

	if notifMgr == nil {
		http.Error(w, "Notifications not available", http.StatusServiceUnavailable)
		return
	}

	sent, err := notifMgr.SendTestNotifications()
	if err != nil {
		http.Error(w, "Failed to send test notifications: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"sent": sent})
}
