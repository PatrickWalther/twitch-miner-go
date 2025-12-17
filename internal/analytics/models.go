package analytics

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
}

type StreamerGridData struct {
	Streamers []StreamerInfo
}

type ChatMessage struct {
	ID          int64  `json:"id"`
	Timestamp   int64  `json:"timestamp"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Message     string `json:"message"`
	Emotes      string `json:"emotes,omitempty"`
	Badges      string `json:"badges,omitempty"`
	Color       string `json:"color,omitempty"`
}

type ChatLogData struct {
	Messages   []ChatMessage `json:"messages"`
	TotalCount int           `json:"total_count"`
	HasMore    bool          `json:"has_more"`
}
