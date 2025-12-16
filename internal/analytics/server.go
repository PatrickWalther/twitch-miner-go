package analytics

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/patrickdappollonio/twitch-miner/internal/config"
	"github.com/patrickdappollonio/twitch-miner/internal/models"
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
	basePath  string

	server    *http.Server
	templates map[string]*template.Template
	mu        sync.RWMutex
}

type SeriesPoint struct {
	X int64  `json:"x"`
	Y int    `json:"y"`
	Z string `json:"z,omitempty"`
}

type Annotation struct {
	X           int64           `json:"x"`
	BorderColor string          `json:"borderColor"`
	Label       AnnotationLabel `json:"label"`
}

type AnnotationLabel struct {
	Style map[string]string `json:"style"`
	Text  string            `json:"text"`
}

type StreamerData struct {
	Series      []SeriesPoint `json:"series"`
	Annotations []Annotation  `json:"annotations"`
}

type StreamerInfo struct {
	Name                  string `json:"name"`
	Points                int    `json:"points"`
	PointsFormatted       string `json:"points_formatted"`
	LastActivity          int64  `json:"last_activity"`
	LastActivityFormatted string `json:"last_activity_formatted"`
}

type DashboardData struct {
	Username       string
	RefreshMinutes int
	TotalPoints    string
	StreamerCount  int
	PointsToday    string
}

type StreamerPageData struct {
	Username       string
	RefreshMinutes int
	Streamer       StreamerInfo
	PointsGained   string
	DataPoints     int
	DaysAgo        int
	StartDate      string
	EndDate        string
}

type StreamerGridData struct {
	Streamers []StreamerInfo
}

func NewAnalyticsServer(settings config.AnalyticsSettings, username string, streamers []*models.Streamer) *AnalyticsServer {
	basePath := filepath.Join("analytics", username)
	if err := os.MkdirAll(basePath, 0755); err != nil {
		slog.Error("Failed to create analytics directory", "path", basePath, "error", err)
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
		basePath:  basePath,
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
}

func (s *AnalyticsServer) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	streamers := s.getStreamerInfos()

	totalPoints := 0
	pointsToday := 0
	todayStart := time.Now().Truncate(24 * time.Hour).UnixMilli()

	for _, info := range streamers {
		totalPoints += info.Points

		data := s.readStreamerData(info.Name)
		for i := len(data.Series) - 1; i >= 0; i-- {
			if data.Series[i].X < todayStart {
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

	data := s.readStreamerData(name)
	if len(data.Series) == 0 {
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
	streamers := s.getStreamerInfos()

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

func (s *AnalyticsServer) getStreamerInfos() []StreamerInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	files, err := os.ReadDir(s.basePath)
	if err != nil {
		return nil
	}

	var streamers []StreamerInfo
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".json") {
			name := strings.TrimSuffix(file.Name(), ".json")
			data := s.readStreamerData(name)

			points := 0
			lastActivity := int64(0)
			if len(data.Series) > 0 {
				points = data.Series[len(data.Series)-1].Y
				lastActivity = data.Series[len(data.Series)-1].X
			}

			streamers = append(streamers, StreamerInfo{
				Name:                  name,
				Points:                points,
				PointsFormatted:       formatNumber(points),
				LastActivity:          lastActivity,
				LastActivityFormatted: formatTimeAgo(lastActivity),
			})
		}
	}

	sort.Slice(streamers, func(i, j int) bool {
		return streamers[i].Points > streamers[j].Points
	})

	return streamers
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
	s.mu.RLock()
	defer s.mu.RUnlock()

	files, err := os.ReadDir(s.basePath)
	if err != nil {
		http.Error(w, "Failed to read analytics", http.StatusInternalServerError)
		return
	}

	var streamers []StreamerInfo
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".json") {
			name := strings.TrimSuffix(file.Name(), ".json")
			data := s.readStreamerData(name)

			points := 0
			lastActivity := int64(0)
			if len(data.Series) > 0 {
				points = data.Series[len(data.Series)-1].Y
				lastActivity = data.Series[len(data.Series)-1].X
			}

			streamers = append(streamers, StreamerInfo{
				Name:         name,
				Points:       points,
				LastActivity: lastActivity,
			})
		}
	}

	sort.Slice(streamers, func(i, j int) bool {
		return streamers[i].Name < streamers[j].Name
	})

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

	data := s.readStreamerData(streamer)

	startDate := r.URL.Query().Get("startDate")
	endDate := r.URL.Query().Get("endDate")
	data = s.filterData(data, startDate, endDate)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

func (s *AnalyticsServer) handleJSONAll(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	files, err := os.ReadDir(s.basePath)
	if err != nil {
		http.Error(w, "Failed to read analytics", http.StatusInternalServerError)
		return
	}

	type namedData struct {
		Name string       `json:"name"`
		Data StreamerData `json:"data"`
	}

	var result []namedData
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".json") {
			name := strings.TrimSuffix(file.Name(), ".json")
			data := s.readStreamerData(name)
			result = append(result, namedData{Name: name, Data: data})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (s *AnalyticsServer) readStreamerData(streamer string) StreamerData {
	path := filepath.Join(s.basePath, streamer+".json")

	data, err := os.ReadFile(path)
	if err != nil {
		return StreamerData{}
	}

	var result StreamerData
	_ = json.Unmarshal(data, &result)
	return result
}

func (s *AnalyticsServer) filterData(data StreamerData, startDate, endDate string) StreamerData {
	var startTS, endTS int64

	if startDate != "" {
		if t, err := time.Parse("2006-01-02", startDate); err == nil {
			startTS = t.UnixMilli()
		}
	}

	if endDate != "" {
		if t, err := time.Parse("2006-01-02", endDate); err == nil {
			endTS = t.Add(24*time.Hour - time.Second).UnixMilli()
		}
	} else {
		endTS = time.Now().UnixMilli()
	}

	if startTS > 0 || endTS > 0 {
		var filtered []SeriesPoint
		for _, p := range data.Series {
			if (startTS == 0 || p.X >= startTS) && (endTS == 0 || p.X <= endTS) {
				filtered = append(filtered, p)
			}
		}
		data.Series = filtered

		var filteredAnnotations []Annotation
		for _, a := range data.Annotations {
			if (startTS == 0 || a.X >= startTS) && (endTS == 0 || a.X <= endTS) {
				filteredAnnotations = append(filteredAnnotations, a)
			}
		}
		data.Annotations = filteredAnnotations
	}

	return data
}

func (s *AnalyticsServer) RecordPoints(streamer *models.Streamer, eventType string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.basePath, streamer.Username+".json")

	var data StreamerData
	if existing, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(existing, &data)
	}

	point := SeriesPoint{
		X: time.Now().UnixMilli(),
		Y: streamer.GetChannelPoints(),
		Z: strings.ReplaceAll(eventType, "_", " "),
	}
	data.Series = append(data.Series, point)

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return
	}

	if err := os.WriteFile(path, jsonData, 0644); err != nil {
		slog.Error("Failed to write analytics data", "path", path, "error", err)
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

	path := filepath.Join(s.basePath, streamer.Username+".json")

	var data StreamerData
	if existing, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(existing, &data)
	}

	annotation := Annotation{
		X:           time.Now().UnixMilli(),
		BorderColor: color,
		Label: AnnotationLabel{
			Style: map[string]string{"color": "#000", "background": color},
			Text:  text,
		},
	}
	data.Annotations = append(data.Annotations, annotation)

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return
	}

	if err := os.WriteFile(path, jsonData, 0644); err != nil {
		slog.Error("Failed to write analytics annotation", "path", path, "error", err)
	}
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
