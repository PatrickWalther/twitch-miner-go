package notifications

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/PatrickWalther/twitch-miner-go/internal/database"
)

type Repository struct {
	db *database.DB
	mu sync.RWMutex
}

type NotificationsModule struct{}

func (m *NotificationsModule) Name() string {
	return "notifications"
}

func (m *NotificationsModule) Migrations() []database.Migration {
	return []database.Migration{
		{
			Version:     1,
			Description: "Create notification_config and point_rules tables",
			SQL: `
				CREATE TABLE IF NOT EXISTS notification_config (
					id INTEGER PRIMARY KEY CHECK (id = 1),
					mentions_channel_id TEXT DEFAULT '',
					points_channel_id TEXT DEFAULT '',
					online_channel_id TEXT DEFAULT '',
					offline_channel_id TEXT DEFAULT '',
					mentions_enabled INTEGER DEFAULT 0,
					mentions_all_chats INTEGER DEFAULT 1,
					mentions_streamers TEXT DEFAULT '[]',
					online_enabled INTEGER DEFAULT 0,
					online_all_streamers INTEGER DEFAULT 1,
					online_streamers TEXT DEFAULT '[]',
					offline_enabled INTEGER DEFAULT 0,
					offline_all_streamers INTEGER DEFAULT 1,
					offline_streamers TEXT DEFAULT '[]'
				);

				CREATE TABLE IF NOT EXISTS point_rules (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					streamer TEXT NOT NULL,
					threshold INTEGER NOT NULL,
					delete_on_trigger INTEGER DEFAULT 0,
					triggered INTEGER DEFAULT 0
				);

				INSERT OR IGNORE INTO notification_config (id) VALUES (1);
			`,
		},
	}
}

func NewRepository(db *database.DB, basePath string) (*Repository, error) {
	module := &NotificationsModule{}
	if err := db.RegisterModule(module); err != nil {
		return nil, fmt.Errorf("failed to register notifications module: %w", err)
	}

	repo := &Repository{db: db}

	if err := repo.migrateFromOldDB(basePath); err != nil {
		slog.Warn("Old notifications.db migration had errors", "error", err)
	}

	return repo, nil
}

func (r *Repository) migrateFromOldDB(basePath string) error {
	oldDBPath := filepath.Join(basePath, "notifications.db")
	if _, err := os.Stat(oldDBPath); os.IsNotExist(err) {
		return nil
	}

	slog.Info("Migrating data from old notifications.db")

	oldDB, err := sql.Open("sqlite", oldDBPath)
	if err != nil {
		return fmt.Errorf("failed to open old database: %w", err)
	}
	defer oldDB.Close()

	row := oldDB.QueryRow(`
		SELECT 
			mentions_channel_id, points_channel_id, online_channel_id, offline_channel_id,
			mentions_enabled, mentions_all_chats, mentions_streamers,
			online_enabled, online_all_streamers, online_streamers,
			offline_enabled, offline_all_streamers, offline_streamers
		FROM notification_config WHERE id = 1
	`)

	var mentionsChannelID, pointsChannelID, onlineChannelID, offlineChannelID string
	var mentionsEnabled, mentionsAllChats bool
	var mentionsStreamersJSON string
	var onlineEnabled, onlineAllStreamers bool
	var onlineStreamersJSON string
	var offlineEnabled, offlineAllStreamers bool
	var offlineStreamersJSON string

	err = row.Scan(
		&mentionsChannelID, &pointsChannelID, &onlineChannelID, &offlineChannelID,
		&mentionsEnabled, &mentionsAllChats, &mentionsStreamersJSON,
		&onlineEnabled, &onlineAllStreamers, &onlineStreamersJSON,
		&offlineEnabled, &offlineAllStreamers, &offlineStreamersJSON,
	)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to read old config: %w", err)
	}

	if err != sql.ErrNoRows {
		_, err = r.db.Exec(`
			UPDATE notification_config SET
				mentions_channel_id = ?,
				points_channel_id = ?,
				online_channel_id = ?,
				offline_channel_id = ?,
				mentions_enabled = ?,
				mentions_all_chats = ?,
				mentions_streamers = ?,
				online_enabled = ?,
				online_all_streamers = ?,
				online_streamers = ?,
				offline_enabled = ?,
				offline_all_streamers = ?,
				offline_streamers = ?
			WHERE id = 1
		`,
			mentionsChannelID, pointsChannelID, onlineChannelID, offlineChannelID,
			mentionsEnabled, mentionsAllChats, mentionsStreamersJSON,
			onlineEnabled, onlineAllStreamers, onlineStreamersJSON,
			offlineEnabled, offlineAllStreamers, offlineStreamersJSON,
		)
		if err != nil {
			return fmt.Errorf("failed to migrate config: %w", err)
		}
	}

	rows, err := oldDB.Query(`SELECT streamer, threshold, delete_on_trigger, triggered FROM point_rules`)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to read old point rules: %w", err)
	}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var streamer string
			var threshold int
			var deleteOnTrigger, triggered bool
			if err := rows.Scan(&streamer, &threshold, &deleteOnTrigger, &triggered); err != nil {
				continue
			}
			_, err = r.db.Exec(`
				INSERT INTO point_rules (streamer, threshold, delete_on_trigger, triggered)
				VALUES (?, ?, ?, ?)
			`, streamer, threshold, deleteOnTrigger, triggered)
			if err != nil {
				slog.Warn("Failed to migrate point rule", "streamer", streamer, "error", err)
			}
		}
	}

	if err := os.Remove(oldDBPath); err != nil {
		slog.Warn("Failed to delete old notifications.db", "error", err)
	} else {
		slog.Info("Deleted old notifications.db after successful migration")
	}

	return nil
}

func (r *Repository) Close() error {
	return nil
}

func (r *Repository) GetConfig() (*NotificationConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	row := r.db.QueryRow(`
		SELECT 
			mentions_channel_id, points_channel_id, online_channel_id, offline_channel_id,
			mentions_enabled, mentions_all_chats, mentions_streamers,
			online_enabled, online_all_streamers, online_streamers,
			offline_enabled, offline_all_streamers, offline_streamers
		FROM notification_config WHERE id = 1
	`)

	var cfg NotificationConfig
	var mentionsStreamersJSON, onlineStreamersJSON, offlineStreamersJSON string

	err := row.Scan(
		&cfg.MentionsChannelID, &cfg.PointsChannelID, &cfg.OnlineChannelID, &cfg.OfflineChannelID,
		&cfg.MentionsEnabled, &cfg.MentionsAllChats, &mentionsStreamersJSON,
		&cfg.OnlineEnabled, &cfg.OnlineAllStreamers, &onlineStreamersJSON,
		&cfg.OfflineEnabled, &cfg.OfflineAllStreamers, &offlineStreamersJSON,
	)
	if err != nil {
		return nil, err
	}

	_ = json.Unmarshal([]byte(mentionsStreamersJSON), &cfg.MentionsStreamers)
	_ = json.Unmarshal([]byte(onlineStreamersJSON), &cfg.OnlineStreamers)
	_ = json.Unmarshal([]byte(offlineStreamersJSON), &cfg.OfflineStreamers)

	if cfg.MentionsStreamers == nil {
		cfg.MentionsStreamers = []string{}
	}
	if cfg.OnlineStreamers == nil {
		cfg.OnlineStreamers = []string{}
	}
	if cfg.OfflineStreamers == nil {
		cfg.OfflineStreamers = []string{}
	}

	return &cfg, nil
}

func (r *Repository) SaveConfig(cfg *NotificationConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	mentionsStreamersJSON, _ := json.Marshal(cfg.MentionsStreamers)
	onlineStreamersJSON, _ := json.Marshal(cfg.OnlineStreamers)
	offlineStreamersJSON, _ := json.Marshal(cfg.OfflineStreamers)

	_, err := r.db.Exec(`
		UPDATE notification_config SET
			mentions_channel_id = ?,
			points_channel_id = ?,
			online_channel_id = ?,
			offline_channel_id = ?,
			mentions_enabled = ?,
			mentions_all_chats = ?,
			mentions_streamers = ?,
			online_enabled = ?,
			online_all_streamers = ?,
			online_streamers = ?,
			offline_enabled = ?,
			offline_all_streamers = ?,
			offline_streamers = ?
		WHERE id = 1
	`,
		cfg.MentionsChannelID, cfg.PointsChannelID, cfg.OnlineChannelID, cfg.OfflineChannelID,
		cfg.MentionsEnabled, cfg.MentionsAllChats, string(mentionsStreamersJSON),
		cfg.OnlineEnabled, cfg.OnlineAllStreamers, string(onlineStreamersJSON),
		cfg.OfflineEnabled, cfg.OfflineAllStreamers, string(offlineStreamersJSON),
	)

	return err
}

func (r *Repository) GetPointRules() ([]PointRule, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	rows, err := r.db.Query(`
		SELECT id, streamer, threshold, delete_on_trigger, triggered
		FROM point_rules ORDER BY streamer, threshold
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []PointRule
	for rows.Next() {
		var rule PointRule
		if err := rows.Scan(&rule.ID, &rule.Streamer, &rule.Threshold, &rule.DeleteOnTrigger, &rule.Triggered); err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}

	return rules, rows.Err()
}

func (r *Repository) AddPointRule(rule *PointRule) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	result, err := r.db.Exec(`
		INSERT INTO point_rules (streamer, threshold, delete_on_trigger, triggered)
		VALUES (?, ?, ?, 0)
	`, rule.Streamer, rule.Threshold, rule.DeleteOnTrigger)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	rule.ID = id

	return nil
}

func (r *Repository) UpdatePointRule(rule *PointRule) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, err := r.db.Exec(`
		UPDATE point_rules SET
			streamer = ?,
			threshold = ?,
			delete_on_trigger = ?,
			triggered = ?
		WHERE id = ?
	`, rule.Streamer, rule.Threshold, rule.DeleteOnTrigger, rule.Triggered, rule.ID)

	return err
}

func (r *Repository) DeletePointRule(id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, err := r.db.Exec(`DELETE FROM point_rules WHERE id = ?`, id)
	return err
}

func (r *Repository) MarkPointRuleTriggered(id int64, triggered bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, err := r.db.Exec(`UPDATE point_rules SET triggered = ? WHERE id = ?`, triggered, id)
	return err
}

func (r *Repository) ResetPointRuleIfBelow(streamer string, points int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, err := r.db.Exec(`
		UPDATE point_rules 
		SET triggered = 0 
		WHERE streamer = ? AND threshold > ? AND triggered = 1 AND delete_on_trigger = 0
	`, streamer, points)

	return err
}
