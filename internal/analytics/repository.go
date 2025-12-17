package analytics

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Repository interface {
	RecordPoints(streamer string, points int, eventType string) error
	RecordAnnotation(streamer string, eventType, text, color string) error
	GetStreamerData(streamer string) (*StreamerData, error)
	GetStreamerDataFiltered(streamer string, startTime, endTime time.Time) (*StreamerData, error)
	ListStreamers() ([]StreamerInfo, error)
	RecordChatMessage(streamer string, msg ChatMessage) error
	GetChatMessages(streamer string, limit, offset int) (*ChatLogData, error)
	SearchChatMessages(streamer string, query string, limit, offset int) (*ChatLogData, error)
	Close() error
}

type SQLiteRepository struct {
	db       *sql.DB
	basePath string
}

const schemaVersion = 2

func NewSQLiteRepository(basePath string) (*SQLiteRepository, error) {
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create analytics directory: %w", err)
	}

	dbPath := filepath.Join(basePath, "analytics.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db.SetMaxOpenConns(1)

	repo := &SQLiteRepository{
		db:       db,
		basePath: basePath,
	}

	if err := repo.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	if err := repo.migrateFromJSON(); err != nil {
		slog.Warn("JSON migration had errors", "error", err)
	}

	return repo, nil
}

func (r *SQLiteRepository) migrate() error {
	var version int
	err := r.db.QueryRow("PRAGMA user_version").Scan(&version)
	if err != nil {
		return err
	}

	if version < 1 {
		_, err = r.db.Exec(`
			CREATE TABLE IF NOT EXISTS streamers (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT UNIQUE NOT NULL,
				created_at INTEGER NOT NULL
			);

			CREATE TABLE IF NOT EXISTS points (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				streamer_id INTEGER NOT NULL,
				timestamp INTEGER NOT NULL,
				points INTEGER NOT NULL,
				event_type TEXT,
				FOREIGN KEY (streamer_id) REFERENCES streamers(id)
			);

			CREATE TABLE IF NOT EXISTS annotations (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				streamer_id INTEGER NOT NULL,
				timestamp INTEGER NOT NULL,
				text TEXT NOT NULL,
				color TEXT NOT NULL,
				FOREIGN KEY (streamer_id) REFERENCES streamers(id)
			);

			CREATE INDEX IF NOT EXISTS idx_points_streamer_time ON points(streamer_id, timestamp);
			CREATE INDEX IF NOT EXISTS idx_annotations_streamer_time ON annotations(streamer_id, timestamp);
		`)
		if err != nil {
			return err
		}
		version = 1
	}

	if version < 2 {
		_, err = r.db.Exec(`
			CREATE TABLE IF NOT EXISTS chat_messages (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				streamer_id INTEGER NOT NULL,
				timestamp INTEGER NOT NULL,
				username TEXT NOT NULL,
				display_name TEXT NOT NULL,
				message TEXT NOT NULL,
				emotes TEXT,
				badges TEXT,
				color TEXT,
				FOREIGN KEY (streamer_id) REFERENCES streamers(id)
			);

			CREATE INDEX IF NOT EXISTS idx_chat_streamer_time ON chat_messages(streamer_id, timestamp);
		`)
		if err != nil {
			return err
		}
	}

	_, err = r.db.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion))
	return err
}

func (r *SQLiteRepository) migrateFromJSON() error {
	files, err := os.ReadDir(r.basePath)
	if err != nil {
		return nil
	}

	var jsonFiles []string
	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".json") {
			jsonFiles = append(jsonFiles, f.Name())
		}
	}

	if len(jsonFiles) == 0 {
		return nil
	}

	slog.Info("Migrating JSON files to SQLite", "count", len(jsonFiles))

	var migrationErrors []error
	for _, jsonFile := range jsonFiles {
		streamer := strings.TrimSuffix(jsonFile, ".json")
		jsonPath := filepath.Join(r.basePath, jsonFile)

		data, err := os.ReadFile(jsonPath)
		if err != nil {
			migrationErrors = append(migrationErrors, fmt.Errorf("failed to read %s: %w", jsonFile, err))
			continue
		}

		var sd StreamerData
		if err := json.Unmarshal(data, &sd); err != nil {
			migrationErrors = append(migrationErrors, fmt.Errorf("failed to parse %s: %w", jsonFile, err))
			continue
		}

		if err := r.importStreamerData(streamer, &sd); err != nil {
			migrationErrors = append(migrationErrors, fmt.Errorf("failed to import %s: %w", jsonFile, err))
			continue
		}

		if err := os.Remove(jsonPath); err != nil {
			slog.Warn("Failed to delete migrated JSON file", "path", jsonPath, "error", err)
		} else {
			slog.Info("Migrated and deleted JSON file", "streamer", streamer, "points", len(sd.Series), "annotations", len(sd.Annotations))
		}
	}

	if len(migrationErrors) > 0 {
		return fmt.Errorf("migration completed with %d errors", len(migrationErrors))
	}

	return nil
}

func (r *SQLiteRepository) importStreamerData(streamer string, data *StreamerData) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	streamerID, err := r.getOrCreateStreamerTx(tx, streamer)
	if err != nil {
		return err
	}

	pointsStmt, err := tx.Prepare("INSERT INTO points (streamer_id, timestamp, points, event_type) VALUES (?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer pointsStmt.Close()

	for _, p := range data.Series {
		_, err = pointsStmt.Exec(streamerID, p.X, p.Y, p.Z)
		if err != nil {
			return err
		}
	}

	annotationsStmt, err := tx.Prepare("INSERT INTO annotations (streamer_id, timestamp, text, color) VALUES (?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer annotationsStmt.Close()

	for _, a := range data.Annotations {
		_, err = annotationsStmt.Exec(streamerID, a.X, a.Label.Text, a.BorderColor)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *SQLiteRepository) getOrCreateStreamer(name string) (int64, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	id, err := r.getOrCreateStreamerTx(tx, name)
	if err != nil {
		return 0, err
	}

	return id, tx.Commit()
}

func (r *SQLiteRepository) getOrCreateStreamerTx(tx *sql.Tx, name string) (int64, error) {
	var id int64
	err := tx.QueryRow("SELECT id FROM streamers WHERE name = ?", name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}

	result, err := tx.Exec("INSERT INTO streamers (name, created_at) VALUES (?, ?)", name, time.Now().UnixMilli())
	if err != nil {
		return 0, err
	}

	return result.LastInsertId()
}

func (r *SQLiteRepository) RecordPoints(streamer string, points int, eventType string) error {
	streamerID, err := r.getOrCreateStreamer(streamer)
	if err != nil {
		return err
	}

	_, err = r.db.Exec(
		"INSERT INTO points (streamer_id, timestamp, points, event_type) VALUES (?, ?, ?, ?)",
		streamerID, time.Now().UnixMilli(), points, eventType,
	)
	return err
}

func (r *SQLiteRepository) RecordAnnotation(streamer string, eventType, text, color string) error {
	streamerID, err := r.getOrCreateStreamer(streamer)
	if err != nil {
		return err
	}

	_, err = r.db.Exec(
		"INSERT INTO annotations (streamer_id, timestamp, text, color) VALUES (?, ?, ?, ?)",
		streamerID, time.Now().UnixMilli(), text, color,
	)
	return err
}

func (r *SQLiteRepository) GetStreamerData(streamer string) (*StreamerData, error) {
	return r.GetStreamerDataFiltered(streamer, time.Time{}, time.Time{})
}

func (r *SQLiteRepository) GetStreamerDataFiltered(streamer string, startTime, endTime time.Time) (*StreamerData, error) {
	var streamerID int64
	err := r.db.QueryRow("SELECT id FROM streamers WHERE name = ?", streamer).Scan(&streamerID)
	if err == sql.ErrNoRows {
		return &StreamerData{}, nil
	}
	if err != nil {
		return nil, err
	}

	data := &StreamerData{}

	pointsQuery := "SELECT timestamp, points, COALESCE(event_type, '') FROM points WHERE streamer_id = ?"
	var args []interface{}
	args = append(args, streamerID)

	if !startTime.IsZero() {
		pointsQuery += " AND timestamp >= ?"
		args = append(args, startTime.UnixMilli())
	}
	if !endTime.IsZero() {
		pointsQuery += " AND timestamp <= ?"
		args = append(args, endTime.UnixMilli())
	}
	pointsQuery += " ORDER BY timestamp ASC"

	rows, err := r.db.Query(pointsQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var p SeriesPoint
		if err := rows.Scan(&p.X, &p.Y, &p.Z); err != nil {
			return nil, err
		}
		data.Series = append(data.Series, p)
	}

	annotationsQuery := "SELECT timestamp, text, color FROM annotations WHERE streamer_id = ?"
	args = []interface{}{streamerID}

	if !startTime.IsZero() {
		annotationsQuery += " AND timestamp >= ?"
		args = append(args, startTime.UnixMilli())
	}
	if !endTime.IsZero() {
		annotationsQuery += " AND timestamp <= ?"
		args = append(args, endTime.UnixMilli())
	}
	annotationsQuery += " ORDER BY timestamp ASC"

	rows, err = r.db.Query(annotationsQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var a Annotation
		var text, color string
		if err := rows.Scan(&a.X, &text, &color); err != nil {
			return nil, err
		}
		a.BorderColor = color
		a.Label = AnnotationLabel{
			Style: map[string]string{"color": "#000", "background": color},
			Text:  text,
		}
		data.Annotations = append(data.Annotations, a)
	}

	return data, nil
}

func (r *SQLiteRepository) ListStreamers() ([]StreamerInfo, error) {
	query := `
		SELECT s.name, 
			COALESCE((SELECT points FROM points WHERE streamer_id = s.id ORDER BY timestamp DESC LIMIT 1), 0) as points,
			COALESCE((SELECT timestamp FROM points WHERE streamer_id = s.id ORDER BY timestamp DESC LIMIT 1), 0) as last_activity
		FROM streamers s
		ORDER BY points DESC
	`

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var streamers []StreamerInfo
	for rows.Next() {
		var info StreamerInfo
		if err := rows.Scan(&info.Name, &info.Points, &info.LastActivity); err != nil {
			return nil, err
		}
		info.PointsFormatted = formatNumber(info.Points)
		info.LastActivityFormatted = formatTimeAgo(info.LastActivity)
		streamers = append(streamers, info)
	}

	return streamers, nil
}

func (r *SQLiteRepository) RecordChatMessage(streamer string, msg ChatMessage) error {
	streamerID, err := r.getOrCreateStreamer(streamer)
	if err != nil {
		return err
	}

	_, err = r.db.Exec(
		`INSERT INTO chat_messages (streamer_id, timestamp, username, display_name, message, emotes, badges, color) 
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		streamerID, time.Now().UnixMilli(), msg.Username, msg.DisplayName, msg.Message, msg.Emotes, msg.Badges, msg.Color,
	)
	return err
}

func (r *SQLiteRepository) GetChatMessages(streamer string, limit, offset int) (*ChatLogData, error) {
	var streamerID int64
	err := r.db.QueryRow("SELECT id FROM streamers WHERE name = ?", streamer).Scan(&streamerID)
	if err == sql.ErrNoRows {
		return &ChatLogData{Messages: []ChatMessage{}}, nil
	}
	if err != nil {
		return nil, err
	}

	var totalCount int
	err = r.db.QueryRow("SELECT COUNT(*) FROM chat_messages WHERE streamer_id = ?", streamerID).Scan(&totalCount)
	if err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 100
	}

	rows, err := r.db.Query(
		`SELECT id, timestamp, username, display_name, message, COALESCE(emotes, ''), COALESCE(badges, ''), COALESCE(color, '')
		 FROM chat_messages 
		 WHERE streamer_id = ? 
		 ORDER BY timestamp DESC 
		 LIMIT ? OFFSET ?`,
		streamerID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []ChatMessage
	for rows.Next() {
		var msg ChatMessage
		if err := rows.Scan(&msg.ID, &msg.Timestamp, &msg.Username, &msg.DisplayName, &msg.Message, &msg.Emotes, &msg.Badges, &msg.Color); err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}

	if messages == nil {
		messages = []ChatMessage{}
	}

	return &ChatLogData{
		Messages:   messages,
		TotalCount: totalCount,
		HasMore:    offset+len(messages) < totalCount,
	}, nil
}

func (r *SQLiteRepository) SearchChatMessages(streamer string, query string, limit, offset int) (*ChatLogData, error) {
	var streamerID int64
	err := r.db.QueryRow("SELECT id FROM streamers WHERE name = ?", streamer).Scan(&streamerID)
	if err == sql.ErrNoRows {
		return &ChatLogData{Messages: []ChatMessage{}}, nil
	}
	if err != nil {
		return nil, err
	}

	searchPattern := "%" + query + "%"

	var totalCount int
	err = r.db.QueryRow(
		"SELECT COUNT(*) FROM chat_messages WHERE streamer_id = ? AND (message LIKE ? OR username LIKE ? OR display_name LIKE ?)",
		streamerID, searchPattern, searchPattern, searchPattern,
	).Scan(&totalCount)
	if err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 100
	}

	rows, err := r.db.Query(
		`SELECT id, timestamp, username, display_name, message, COALESCE(emotes, ''), COALESCE(badges, ''), COALESCE(color, '')
		 FROM chat_messages 
		 WHERE streamer_id = ? AND (message LIKE ? OR username LIKE ? OR display_name LIKE ?)
		 ORDER BY timestamp DESC 
		 LIMIT ? OFFSET ?`,
		streamerID, searchPattern, searchPattern, searchPattern, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []ChatMessage
	for rows.Next() {
		var msg ChatMessage
		if err := rows.Scan(&msg.ID, &msg.Timestamp, &msg.Username, &msg.DisplayName, &msg.Message, &msg.Emotes, &msg.Badges, &msg.Color); err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}

	if messages == nil {
		messages = []ChatMessage{}
	}

	return &ChatLogData{
		Messages:   messages,
		TotalCount: totalCount,
		HasMore:    offset+len(messages) < totalCount,
	}, nil
}

func (r *SQLiteRepository) Close() error {
	return r.db.Close()
}
