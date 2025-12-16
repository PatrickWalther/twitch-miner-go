package analytics

import (
	"encoding/json"
	"fmt"
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

type AnalyticsServer struct {
	host      string
	port      int
	refresh   int
	daysAgo   int
	username  string
	streamers []*models.Streamer
	basePath  string

	server *http.Server
	mu     sync.RWMutex
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
	Name         string `json:"name"`
	Points       int    `json:"points"`
	LastActivity int64  `json:"last_activity"`
}

func NewAnalyticsServer(settings config.AnalyticsSettings, username string, streamers []*models.Streamer) *AnalyticsServer {
	basePath := filepath.Join("analytics", username)
	os.MkdirAll(basePath, 0755)

	return &AnalyticsServer{
		host:      settings.Host,
		port:      settings.Port,
		refresh:   settings.Refresh,
		daysAgo:   settings.DaysAgo,
		username:  username,
		streamers: streamers,
		basePath:  basePath,
	}
}

func (s *AnalyticsServer) Start() {
	mux := http.NewServeMux()

	mux.HandleFunc("/", s.handleIndex)
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

func (s *AnalyticsServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <title>Twitch Points Miner Analytics</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; background: #1a1a2e; color: #eee; }
        h1 { color: #9146ff; }
        .streamer { background: #16213e; padding: 15px; margin: 10px 0; border-radius: 8px; }
        .streamer h2 { margin: 0 0 10px 0; color: #9146ff; }
        .points { font-size: 24px; font-weight: bold; }
        a { color: #9146ff; text-decoration: none; }
        a:hover { text-decoration: underline; }
    </style>
    <meta http-equiv="refresh" content="%d">
</head>
<body>
    <h1>Twitch Points Miner Analytics</h1>
    <p>User: %s</p>
    <div id="streamers"></div>
    <script>
        fetch('/streamers')
            .then(r => r.json())
            .then(data => {
                const container = document.getElementById('streamers');
                data.forEach(s => {
                    const div = document.createElement('div');
                    div.className = 'streamer';
                    div.innerHTML = '<h2>' + s.name + '</h2>' +
                        '<p class="points">' + s.points.toLocaleString() + ' points</p>' +
                        '<p><a href="/json/' + s.name + '">View JSON data</a></p>';
                    container.appendChild(div);
                });
            });
    </script>
</body>
</html>`, s.refresh*60, s.username)

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
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
	json.NewEncoder(w).Encode(streamers)
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
	json.NewEncoder(w).Encode(data)
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
	json.NewEncoder(w).Encode(result)
}

func (s *AnalyticsServer) readStreamerData(streamer string) StreamerData {
	path := filepath.Join(s.basePath, streamer+".json")
	
	data, err := os.ReadFile(path)
	if err != nil {
		return StreamerData{}
	}

	var result StreamerData
	json.Unmarshal(data, &result)
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
		json.Unmarshal(existing, &data)
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

	os.WriteFile(path, jsonData, 0644)
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
		json.Unmarshal(existing, &data)
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

	os.WriteFile(path, jsonData, 0644)
}
