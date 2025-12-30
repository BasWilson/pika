package main

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/baswilson/pika/internal/ai"
	"github.com/baswilson/pika/internal/config"
	"github.com/baswilson/pika/internal/memory"
	"github.com/joho/godotenv"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	log.Println("PIKA Memory Embedding Backfill Tool")
	log.Println("====================================")

	// Load environment
	_ = godotenv.Load()
	cfg := config.Load()

	// Connect to database
	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}
	log.Println("Connected to database")

	// Create services
	memoryStore := memory.NewStore(db)
	aiService := ai.NewService(cfg, memoryStore)

	ctx := context.Background()
	batchSize := 50
	processed := 0
	failed := 0
	delayBetweenRequests := 100 * time.Millisecond

	log.Printf("Starting backfill (batch size: %d, delay: %v)", batchSize, delayBetweenRequests)

	for {
		// Get memories without embeddings
		memories, err := memoryStore.GetWithoutEmbedding(ctx, batchSize)
		if err != nil {
			log.Fatalf("Failed to get memories without embeddings: %v", err)
		}

		if len(memories) == 0 {
			log.Println("No more memories to process")
			break
		}

		log.Printf("Processing batch of %d memories...", len(memories))

		for _, m := range memories {
			// Generate embedding
			embedding, err := aiService.GenerateEmbedding(ctx, m.Content)
			if err != nil {
				log.Printf("  FAILED [%s]: %v", m.ID[:8], err)
				failed++
				continue
			}

			// Update memory with embedding
			if err := memoryStore.UpdateEmbedding(ctx, m.ID, embedding); err != nil {
				log.Printf("  FAILED [%s]: failed to update: %v", m.ID[:8], err)
				failed++
				continue
			}

			processed++
			log.Printf("  OK [%s]: \"%s...\" (%d dims)", m.ID[:8], truncate(m.Content, 40), len(embedding))

			// Rate limiting delay
			time.Sleep(delayBetweenRequests)
		}
	}

	log.Println("====================================")
	log.Printf("Backfill complete!")
	log.Printf("  Processed: %d", processed)
	log.Printf("  Failed:    %d", failed)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
