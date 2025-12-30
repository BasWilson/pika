package memory

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// Memory represents a stored memory
type Memory struct {
	ID           string    `json:"id"`
	Content      string    `json:"content"`
	Importance   float64   `json:"importance"`
	Tags         []string  `json:"tags"`
	CreatedAt    time.Time `json:"created_at"`
	LastAccessed time.Time `json:"last_accessed"`
	AccessCount  int       `json:"access_count"`
}

// Store handles memory persistence
type Store struct {
	db       *sql.DB
	embedder EmbeddingGenerator
}

// NewStore creates a new memory store
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// SetEmbedder sets the embedding generator for the store.
// When set, embeddings will be generated automatically when creating memories.
func (s *Store) SetEmbedder(e EmbeddingGenerator) {
	s.embedder = e
}

// Create stores a new memory with optional embedding generation
func (s *Store) Create(ctx context.Context, content string, importance float64, tags []string) (*Memory, error) {
	id := uuid.New().String()
	now := time.Now()

	// Generate embedding if embedder is available
	var embedding Vector
	if s.embedder != nil {
		emb, err := s.embedder.GenerateEmbedding(ctx, content)
		if err != nil {
			log.Printf("Warning: failed to generate embedding for memory: %v", err)
			// Continue without embedding - memory is still valuable
		} else {
			embedding = Vector(emb)
		}
	}

	query := `
		INSERT INTO memories (id, content, embedding, importance, tags, created_at, last_accessed, access_count)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 0)
		RETURNING id, content, importance, tags, created_at, last_accessed, access_count
	`

	memory := &Memory{}
	err := s.db.QueryRowContext(ctx, query, id, content, embedding, importance, pq.Array(tags), now, now).Scan(
		&memory.ID,
		&memory.Content,
		&memory.Importance,
		pq.Array(&memory.Tags),
		&memory.CreatedAt,
		&memory.LastAccessed,
		&memory.AccessCount,
	)
	if err != nil {
		return nil, err
	}

	return memory, nil
}

// List returns the most recent memories
func (s *Store) List(ctx context.Context, limit int) ([]*Memory, error) {
	query := `
		SELECT id, content, importance, tags, created_at, last_accessed, access_count
		FROM memories
		ORDER BY created_at DESC
		LIMIT $1
	`

	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memories []*Memory
	for rows.Next() {
		m := &Memory{}
		if err := rows.Scan(&m.ID, &m.Content, &m.Importance, pq.Array(&m.Tags), &m.CreatedAt, &m.LastAccessed, &m.AccessCount); err != nil {
			return nil, err
		}
		memories = append(memories, m)
	}

	return memories, nil
}

// Get retrieves a memory by ID
func (s *Store) Get(ctx context.Context, id string) (*Memory, error) {
	query := `
		SELECT id, content, importance, tags, created_at, last_accessed, access_count
		FROM memories
		WHERE id = $1
	`

	m := &Memory{}
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&m.ID, &m.Content, &m.Importance, pq.Array(&m.Tags), &m.CreatedAt, &m.LastAccessed, &m.AccessCount,
	)
	if err != nil {
		return nil, err
	}

	// Update access count and last accessed
	go s.updateAccess(id)

	return m, nil
}

// SearchRelevant searches for relevant memories (simple keyword search)
// In production, this would use vector similarity search
func (s *Store) SearchRelevant(ctx context.Context, query string, limit int) ([]string, error) {
	// Simple search - in production use pgvector similarity search
	sqlQuery := `
		SELECT content
		FROM memories
		WHERE content ILIKE '%' || $1 || '%'
		   OR EXISTS (SELECT 1 FROM unnest(tags) AS tag WHERE tag ILIKE '%' || $1 || '%')
		ORDER BY importance DESC, last_accessed DESC
		LIMIT $2
	`

	rows, err := s.db.QueryContext(ctx, sqlQuery, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var contents []string
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			return nil, err
		}
		contents = append(contents, content)
	}

	return contents, nil
}

// SearchByVector searches using vector similarity (requires embedding)
func (s *Store) SearchByVector(ctx context.Context, embedding []float32, limit int) ([]*Memory, error) {
	query := `
		SELECT id, content, importance, tags, created_at, last_accessed, access_count
		FROM memories
		WHERE embedding IS NOT NULL
		ORDER BY embedding <=> $1
		LIMIT $2
	`

	// Convert to Vector type for proper pgvector formatting
	vec := Vector(embedding)
	rows, err := s.db.QueryContext(ctx, query, vec, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memories []*Memory
	for rows.Next() {
		m := &Memory{}
		if err := rows.Scan(&m.ID, &m.Content, &m.Importance, pq.Array(&m.Tags), &m.CreatedAt, &m.LastAccessed, &m.AccessCount); err != nil {
			return nil, err
		}
		memories = append(memories, m)
	}

	return memories, nil
}

// Delete removes a memory
func (s *Store) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM memories WHERE id = $1", id)
	return err
}

// updateAccess updates the access statistics for a memory
func (s *Store) updateAccess(id string) {
	ctx := context.Background()
	_, _ = s.db.ExecContext(ctx, `
		UPDATE memories
		SET last_accessed = NOW(), access_count = access_count + 1
		WHERE id = $1
	`, id)
}

// GetTopImportant returns the most important memories
func (s *Store) GetTopImportant(ctx context.Context, limit int) ([]*Memory, error) {
	query := `
		SELECT id, content, importance, tags, created_at, last_accessed, access_count
		FROM memories
		ORDER BY importance DESC, access_count DESC
		LIMIT $1
	`

	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memories []*Memory
	for rows.Next() {
		m := &Memory{}
		if err := rows.Scan(&m.ID, &m.Content, &m.Importance, pq.Array(&m.Tags), &m.CreatedAt, &m.LastAccessed, &m.AccessCount); err != nil {
			return nil, err
		}
		memories = append(memories, m)
	}

	return memories, nil
}

// UpdateEmbedding sets the embedding for an existing memory.
// Used for backfilling embeddings on memories that were created before embedding support.
func (s *Store) UpdateEmbedding(ctx context.Context, id string, embedding []float32) error {
	vec := Vector(embedding)
	_, err := s.db.ExecContext(ctx, "UPDATE memories SET embedding = $1 WHERE id = $2", vec, id)
	return err
}

// GetWithoutEmbedding returns memories that don't have embeddings yet.
// Used for backfilling embeddings.
func (s *Store) GetWithoutEmbedding(ctx context.Context, limit int) ([]*Memory, error) {
	query := `
		SELECT id, content, importance, tags, created_at, last_accessed, access_count
		FROM memories
		WHERE embedding IS NULL
		ORDER BY importance DESC, created_at DESC
		LIMIT $1
	`

	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memories []*Memory
	for rows.Next() {
		m := &Memory{}
		if err := rows.Scan(&m.ID, &m.Content, &m.Importance, pq.Array(&m.Tags), &m.CreatedAt, &m.LastAccessed, &m.AccessCount); err != nil {
			return nil, err
		}
		memories = append(memories, m)
	}

	return memories, nil
}
