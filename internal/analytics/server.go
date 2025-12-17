package analytics

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PatrickWalther/twitch-miner-go/internal/config"
	"github.com/PatrickWalther/twitch-miner-go/internal/models"
)

//go:embed templates/*.html templates/partials/*.html
var templatesFS embed.FS

type AnalyticsServer struct {
	host      string
	port      int
	refresh   int
	daysAgo   int
	username  string
	streamers []*models.Streamer

	repo      Repository
	server    *http.Server
	templates map[string]*template.Template
	mu        sync.RWMutex
}

func NewAnalyticsServer(settings config.AnalyticsSettings, username string, streamers []*models.Streamer) *AnalyticsServer {
	basePath := filepath.Join("analytics", username)

	repo, err := NewSQLiteRepository(basePath)
	if err != nil {
		slog.Error("Failed to create analytics repository", "error", err)
		return nil
	}

	templates := make(map[string]*template.Template)

	pages := []string{"dashboard.html", "streamer.html"}
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
		host:      settings.Host,
		port:      settings.Port,
		refresh:   settings.Refresh,
		daysAgo:   settings.DaysAgo,
		username:  username,
		streamers: streamers,
		repo:      repo,
		templates: templates,
	}
}

func (s *AnalyticsServer) Start() {
	mux := http.NewServeMux()

	mux.HandleFunc("/", s.handleDashboard)
	mux.HandleFunc("/streamer/", s.handleStreamerPage)
	mux.HandleFunc("/api/streamers", s.handleAPIStreamers)
	mux.HandleFunc("/api/status", s.handleAPIStatus)

	mux.HandleFunc("/streamers", s.handleStreamers)
	mux.HandleFunc("/json/", s.handleJSON)
	mux.HandleFunc("/json_all", s.handleJSONAll)
	mux.HandleFunc("/api/chat/", s.handleAPIChatMessages)

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
		s.server.Close()
	}
	if s.repo != nil {
		s.repo.Close()
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

	data := DashboardData{
		Username:       s.username,
		RefreshMinutes: s.refresh,
		TotalPoints:    formatNumber(totalPoints),
		StreamerCount:  len(streamers),
		PointsToday:    formatNumber(pointsToday),
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

	startDate := time.Now().AddDate(0, 0, -s.daysAgo).Format("2006-01-02")
	endDate := time.Now().Format("2006-01-02")

	startTS := time.Now().AddDate(0, 0, -s.daysAgo).UnixMilli()
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
		RefreshMinutes: s.refresh,
		Streamer: StreamerInfo{
			Name:            name,
			Points:          currentPoints,
			PointsFormatted: formatNumber(currentPoints),
		},
		PointsGained: formatNumber(pointsGained),
		DataPoints:   len(data.Series),
		DaysAgo:      s.daysAgo,
		StartDate:    startDate,
		EndDate:      endDate,
	}

	s.renderPage(w, "streamer.html", pageData)
}

func (s *AnalyticsServer) handleAPIStreamers(w http.ResponseWriter, r *http.Request) {
	streamers, err := s.repo.ListStreamers()
	if err != nil {
		http.Error(w, "Failed to list streamers", http.StatusInternalServerError)
		return
	}

	gridData := StreamerGridData{
		Streamers: streamers,
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
