package config

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strconv"

	_ "github.com/mattn/go-sqlite3"
)

type Config struct {
	// Server
	Port string
	Env  string

	// Desktop paths
	DataDir      string
	DatabasePath string

	// AI (requesty.ai)
	RequestyAPIKey  string
	RequestyBaseURL string
	RequestyModel   string

	// Google Calendar
	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURL  string

	// Memory
	MemoryContextLimit int
	MemoryTopK         int

	// Ollama (local embeddings)
	OllamaURL        string
	OllamaEmbedModel string
}

func Load() *Config {
	dataDir := getAppDataDir()
	port := "8080"
	dbPath := filepath.Join(dataDir, "pika.db")

	// Try to load config from database first
	dbConfig := loadFromDatabase(dbPath)

	return &Config{
		Port:               port,
		Env:                getEnvOrDB("ENV", "development", dbConfig),
		DataDir:            dataDir,
		DatabasePath:       dbPath,
		RequestyAPIKey:     getEnvOrDB("REQUESTY_API_KEY", "", dbConfig),
		RequestyBaseURL:    getEnvOrDB("REQUESTY_BASE_URL", "https://router.requesty.ai/v1", dbConfig),
		RequestyModel:      getEnvOrDB("REQUESTY_MODEL", "google/gemini-2.0-flash-001", dbConfig),
		GoogleClientID:     getEnvOrDB("GOOGLE_CLIENT_ID", "", dbConfig),
		GoogleClientSecret: getEnvOrDB("GOOGLE_CLIENT_SECRET", "", dbConfig),
		GoogleRedirectURL:  getEnvOrDB("GOOGLE_REDIRECT_URL", "http://localhost:"+port+"/auth/google/callback", dbConfig),
		MemoryContextLimit: getEnvIntOrDB("MEMORY_CONTEXT_LIMIT", 2000, dbConfig),
		MemoryTopK:         getEnvIntOrDB("MEMORY_TOP_K", 10, dbConfig),
		OllamaURL:          getEnvOrDB("OLLAMA_URL", "http://localhost:11434", dbConfig),
		OllamaEmbedModel:   getEnvOrDB("OLLAMA_EMBED_MODEL", "nomic-embed-text", dbConfig),
	}
}

// loadFromDatabase loads config values from SQLite
func loadFromDatabase(dbPath string) map[string]string {
	config := make(map[string]string)

	// Check if database file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return config
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return config
	}
	defer db.Close()

	rows, err := db.QueryContext(context.Background(), "SELECT key, value FROM app_config")
	if err != nil {
		return config
	}
	defer rows.Close()

	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err == nil {
			config[key] = value
		}
	}
	return config
}

// getEnvOrDB checks env first, then database, then falls back to default
func getEnvOrDB(key, fallback string, dbConfig map[string]string) string {
	// Environment variable takes precedence
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	// Then check database (lowercase key)
	dbKey := toLowerKey(key)
	if value, ok := dbConfig[dbKey]; ok && value != "" {
		return value
	}
	return fallback
}

// getEnvIntOrDB checks env first, then database, then falls back to default
func getEnvIntOrDB(key string, fallback int, dbConfig map[string]string) int {
	if value, ok := os.LookupEnv(key); ok {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	dbKey := toLowerKey(key)
	if value, ok := dbConfig[dbKey]; ok {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return fallback
}

// toLowerKey converts ENV_STYLE to env_style for database keys
func toLowerKey(key string) string {
	result := ""
	for i, c := range key {
		if c >= 'A' && c <= 'Z' {
			result += string(c + 32) // lowercase
		} else {
			_ = i
			result += string(c)
		}
	}
	return result
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return fallback
}

func (c *Config) IsDevelopment() bool {
	return c.Env == "development"
}

// getAppDataDir returns the platform-specific application data directory
func getAppDataDir() string {
	// Allow override via environment variable
	if dir := os.Getenv("PIKA_DATA_DIR"); dir != "" {
		return dir
	}

	home, err := os.UserHomeDir()
	if err != nil {
		// Fallback to current directory
		return "."
	}

	// macOS: ~/Library/Application Support/PIKA
	appDir := filepath.Join(home, "Library", "Application Support", "PIKA")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return "."
	}

	return appDir
}
