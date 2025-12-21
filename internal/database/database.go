package database

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"

	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
	mu sync.RWMutex
}

type Module interface {
	Name() string
	Migrations() []Migration
}

type Migration struct {
	Version     int
	Description string
	SQL         string
}

var (
	instance *DB
	once     sync.Once
)

func Open(basePath string) (*DB, error) {
	var initErr error
	once.Do(func() {
		if err := os.MkdirAll(basePath, 0755); err != nil {
			initErr = fmt.Errorf("failed to create database directory: %w", err)
			return
		}

		dbPath := filepath.Join(basePath, "miner.db")
		sqlDB, err := sql.Open("sqlite", dbPath)
		if err != nil {
			initErr = fmt.Errorf("failed to open database: %w", err)
			return
		}

		sqlDB.SetMaxOpenConns(1)

		instance = &DB{DB: sqlDB}
	})

	if initErr != nil {
		return nil, initErr
	}

	return instance, nil
}

func (db *DB) RegisterModule(module Module) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	moduleName := module.Name()
	currentVersion, err := db.getModuleVersion(moduleName)
	if err != nil {
		return fmt.Errorf("failed to get module version for %s: %w", moduleName, err)
	}

	migrations := module.Migrations()
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	for _, m := range migrations {
		if m.Version <= currentVersion {
			continue
		}

		slog.Debug("Applying migration",
			"module", moduleName,
			"version", m.Version,
			"description", m.Description,
		)

		if _, err := db.Exec(m.SQL); err != nil {
			return fmt.Errorf("failed to apply migration %s v%d (%s): %w",
				moduleName, m.Version, m.Description, err)
		}

		if err := db.setModuleVersion(moduleName, m.Version); err != nil {
			return fmt.Errorf("failed to update module version for %s: %w", moduleName, err)
		}
	}

	return nil
}

func (db *DB) getModuleVersion(moduleName string) (int, error) {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_versions (
			module TEXT PRIMARY KEY,
			version INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL
		)
	`)
	if err != nil {
		return 0, err
	}

	var version int
	err = db.QueryRow("SELECT version FROM schema_versions WHERE module = ?", moduleName).Scan(&version)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return version, err
}

func (db *DB) setModuleVersion(moduleName string, version int) error {
	_, err := db.Exec(`
		INSERT INTO schema_versions (module, version, updated_at) 
		VALUES (?, ?, strftime('%s', 'now'))
		ON CONFLICT(module) DO UPDATE SET version = excluded.version, updated_at = excluded.updated_at
	`, moduleName, version)
	return err
}

func (db *DB) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.DB.Close()
}

func (db *DB) RLock() {
	db.mu.RLock()
}

func (db *DB) RUnlock() {
	db.mu.RUnlock()
}

func (db *DB) Lock() {
	db.mu.Lock()
}

func (db *DB) Unlock() {
	db.mu.Unlock()
}
