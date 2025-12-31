package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

// SQLiteDriver implements Driver for SQLite database
type SQLiteDriver struct {
	db   *sql.DB
	path string
}

// NewSQLiteDriver creates a new SQLite database driver
func NewSQLiteDriver(dbPath string) (*SQLiteDriver, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Open SQLite database with CGO (required for sqlite-vec)
	// Enable foreign keys and WAL mode for better performance
	dsn := fmt.Sprintf("%s?_foreign_keys=on&_journal_mode=WAL", dbPath)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to sqlite database: %w", err)
	}

	return &SQLiteDriver{
		db:   db,
		path: dbPath,
	}, nil
}

// DB returns the underlying *sql.DB connection
func (d *SQLiteDriver) DB() *sql.DB {
	return d.db
}

// Close closes the database connection
func (d *SQLiteDriver) Close() error {
	return d.db.Close()
}

// Type returns the database type
func (d *SQLiteDriver) Type() string {
	return "sqlite"
}

// Initialize sets up the database schema
func (d *SQLiteDriver) Initialize(ctx context.Context) error {
	schema := `
	-- Memories table
	CREATE TABLE IF NOT EXISTS memories (
		id TEXT PRIMARY KEY,
		content TEXT NOT NULL,
		embedding BLOB,
		importance REAL DEFAULT 0.5,
		tags TEXT DEFAULT '[]',
		created_at TEXT DEFAULT (datetime('now')),
		last_accessed TEXT DEFAULT (datetime('now')),
		access_count INTEGER DEFAULT 0
	);

	-- Index for importance-based queries
	CREATE INDEX IF NOT EXISTS idx_memories_importance ON memories(importance DESC);
	CREATE INDEX IF NOT EXISTS idx_memories_created_at ON memories(created_at DESC);

	-- OAuth tokens table
	CREATE TABLE IF NOT EXISTS oauth_tokens (
		id TEXT PRIMARY KEY,
		provider TEXT NOT NULL,
		access_token TEXT NOT NULL,
		refresh_token TEXT,
		token_type TEXT,
		expiry TEXT,
		created_at TEXT DEFAULT (datetime('now')),
		updated_at TEXT DEFAULT (datetime('now'))
	);

	CREATE INDEX IF NOT EXISTS idx_oauth_tokens_provider ON oauth_tokens(provider);

	-- Calendar events table
	CREATE TABLE IF NOT EXISTS calendar_events (
		id TEXT PRIMARY KEY,
		google_event_id TEXT UNIQUE,
		title TEXT NOT NULL,
		description TEXT,
		start_time TEXT NOT NULL,
		end_time TEXT NOT NULL,
		location TEXT,
		created_at TEXT DEFAULT (datetime('now')),
		updated_at TEXT DEFAULT (datetime('now'))
	);

	CREATE INDEX IF NOT EXISTS idx_calendar_events_start ON calendar_events(start_time);
	CREATE INDEX IF NOT EXISTS idx_calendar_events_google_id ON calendar_events(google_event_id);

	-- Conversations table
	CREATE TABLE IF NOT EXISTS conversations (
		id TEXT PRIMARY KEY,
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		created_at TEXT DEFAULT (datetime('now'))
	);

	CREATE INDEX IF NOT EXISTS idx_conversations_created_at ON conversations(created_at DESC);

	-- Triggers table
	CREATE TABLE IF NOT EXISTS triggers (
		id TEXT PRIMARY KEY,
		trigger_type TEXT NOT NULL,
		schedule_cron TEXT,
		next_run TEXT,
		payload TEXT,
		enabled INTEGER DEFAULT 1,
		created_at TEXT DEFAULT (datetime('now')),
		updated_at TEXT DEFAULT (datetime('now'))
	);

	CREATE INDEX IF NOT EXISTS idx_triggers_next_run ON triggers(next_run);
	CREATE INDEX IF NOT EXISTS idx_triggers_enabled ON triggers(enabled);

	-- App configuration table
	CREATE TABLE IF NOT EXISTS app_config (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at TEXT DEFAULT (datetime('now'))
	);
	`

	_, err := d.db.ExecContext(ctx, schema)
	if err != nil {
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	return nil
}

// GetAppDataDir returns the application data directory for macOS
func GetAppDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	appDir := filepath.Join(home, "Library", "Application Support", "PIKA")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create app data directory: %w", err)
	}

	return appDir, nil
}

// GetDefaultDatabasePath returns the default SQLite database path
func GetDefaultDatabasePath() (string, error) {
	appDir, err := GetAppDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(appDir, "pika.db"), nil
}

// SetConfig stores a configuration value in the database
func (d *SQLiteDriver) SetConfig(ctx context.Context, key, value string) error {
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO app_config (key, value, updated_at)
		VALUES (?, ?, datetime('now'))
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')
	`, key, value)
	return err
}

// GetConfig retrieves a configuration value from the database
func (d *SQLiteDriver) GetConfig(ctx context.Context, key string) (string, error) {
	var value string
	err := d.db.QueryRowContext(ctx, "SELECT value FROM app_config WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// GetAllConfig retrieves all configuration values as a map
func (d *SQLiteDriver) GetAllConfig(ctx context.Context) (map[string]string, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT key, value FROM app_config")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	config := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		config[key] = value
	}
	return config, rows.Err()
}

// HasConfig checks if any config has been saved
func (d *SQLiteDriver) HasConfig(ctx context.Context) (bool, error) {
	var count int
	err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM app_config WHERE key = 'requesty_api_key'").Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
