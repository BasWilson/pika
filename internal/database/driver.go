package database

import (
	"context"
	"database/sql"
)

// Driver represents a database driver that can be used by the application.
// This abstraction allows swapping between PostgreSQL and SQLite implementations.
type Driver interface {
	// DB returns the underlying *sql.DB connection
	DB() *sql.DB

	// Close closes the database connection
	Close() error

	// Initialize sets up the database schema if needed
	Initialize(ctx context.Context) error

	// Type returns the database type (e.g., "postgres", "sqlite")
	Type() string
}

// Config holds database configuration
type Config struct {
	// Type is the database type: "postgres" or "sqlite"
	Type string

	// For PostgreSQL
	PostgresURL string

	// For SQLite
	SQLitePath string
}
