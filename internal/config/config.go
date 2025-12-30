package config

import (
	"os"
	"strconv"
)

type Config struct {
	// Server
	Port string
	Env  string

	// Database
	DatabaseURL string

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
	return &Config{
		Port:               getEnv("PORT", "8080"),
		Env:                getEnv("ENV", "development"),
		DatabaseURL:        getEnv("DATABASE_URL", "postgres://pika:pika@localhost:5432/pika?sslmode=disable"),
		RequestyAPIKey:     getEnv("REQUESTY_API_KEY", ""),
		RequestyBaseURL:    getEnv("REQUESTY_BASE_URL", "https://router.requesty.ai/v1"),
		RequestyModel:      getEnv("REQUESTY_MODEL", "google/gemini-2.0-flash-001"),
		GoogleClientID:     getEnv("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret: getEnv("GOOGLE_CLIENT_SECRET", ""),
		GoogleRedirectURL:  getEnv("GOOGLE_REDIRECT_URL", "http://localhost:8080/auth/google/callback"),
		MemoryContextLimit: getEnvInt("MEMORY_CONTEXT_LIMIT", 2000),
		MemoryTopK:         getEnvInt("MEMORY_TOP_K", 10),
		OllamaURL:          getEnv("OLLAMA_URL", "http://localhost:11434"),
		OllamaEmbedModel:   getEnv("OLLAMA_EMBED_MODEL", "nomic-embed-text"),
	}
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
