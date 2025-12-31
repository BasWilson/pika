package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// parseTimeString parses a time string from SQLite in various formats
func parseTimeString(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	// Try common SQLite/Go time formats
	formats := []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z",
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00",
	}
	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

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

	// Marshal tags to JSON for SQLite storage
	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return nil, err
	}

	query := `
		INSERT INTO memories (id, content, embedding, importance, tags, created_at, last_accessed, access_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0)
	`

	_, err = s.db.ExecContext(ctx, query, id, content, embedding, importance, string(tagsJSON), now, now)
	if err != nil {
		return nil, err
	}

	return &Memory{
		ID:           id,
		Content:      content,
		Importance:   importance,
		Tags:         tags,
		CreatedAt:    now,
		LastAccessed: now,
		AccessCount:  0,
	}, nil
}

// List returns the most recent memories
func (s *Store) List(ctx context.Context, limit int) ([]*Memory, error) {
	query := `
		SELECT id, content, importance, tags, created_at, last_accessed, access_count
		FROM memories
		ORDER BY created_at DESC
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memories []*Memory
	for rows.Next() {
		m := &Memory{}
		var tagsJSON string
		var createdAtStr, lastAccessedStr string
		if err := rows.Scan(&m.ID, &m.Content, &m.Importance, &tagsJSON, &createdAtStr, &lastAccessedStr, &m.AccessCount); err != nil {
			return nil, err
		}
		m.CreatedAt = parseTimeString(createdAtStr)
		m.LastAccessed = parseTimeString(lastAccessedStr)
		if err := json.Unmarshal([]byte(tagsJSON), &m.Tags); err != nil {
			m.Tags = []string{} // Default to empty if parse fails
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
		WHERE id = ?
	`

	m := &Memory{}
	var tagsJSON string
	var createdAtStr, lastAccessedStr string
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&m.ID, &m.Content, &m.Importance, &tagsJSON, &createdAtStr, &lastAccessedStr, &m.AccessCount,
	)
	if err != nil {
		return nil, err
	}
	m.CreatedAt = parseTimeString(createdAtStr)
	m.LastAccessed = parseTimeString(lastAccessedStr)
	if err := json.Unmarshal([]byte(tagsJSON), &m.Tags); err != nil {
		m.Tags = []string{}
	}

	// Update access count and last accessed
	go s.updateAccess(id)

	return m, nil
}

// SearchRelevant searches for relevant memories (simple keyword search)
func (s *Store) SearchRelevant(ctx context.Context, query string, limit int) ([]string, error) {
	// SQLite uses LIKE for case-insensitive search (case-insensitive by default for ASCII)
	sqlQuery := `
		SELECT content, tags
		FROM memories
		WHERE content LIKE '%' || ? || '%'
		ORDER BY importance DESC, last_accessed DESC
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, sqlQuery, query, limit*2) // Get more to filter by tags
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	queryLower := strings.ToLower(query)
	var contents []string
	for rows.Next() {
		var content, tagsJSON string
		if err := rows.Scan(&content, &tagsJSON); err != nil {
			return nil, err
		}

		// Check if query matches content or any tag
		contentMatches := strings.Contains(strings.ToLower(content), queryLower)

		var tags []string
		if err := json.Unmarshal([]byte(tagsJSON), &tags); err == nil {
			for _, tag := range tags {
				if strings.Contains(strings.ToLower(tag), queryLower) {
					contentMatches = true
					break
				}
			}
		}

		if contentMatches {
			contents = append(contents, content)
			if len(contents) >= limit {
				break
			}
		}
	}

	return contents, nil
}

// SearchByVector searches using vector similarity (in-memory cosine similarity)
func (s *Store) SearchByVector(ctx context.Context, embedding []float32, limit int) ([]*Memory, error) {
	// Fetch all memories with embeddings
	query := `
		SELECT id, content, importance, tags, created_at, last_accessed, access_count, embedding
		FROM memories
		WHERE embedding IS NOT NULL
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type memoryWithScore struct {
		memory     *Memory
		similarity float32
	}
	var results []memoryWithScore

	for rows.Next() {
		m := &Memory{}
		var tagsJSON string
		var embeddingBlob []byte
		var createdAtStr, lastAccessedStr string
		if err := rows.Scan(&m.ID, &m.Content, &m.Importance, &tagsJSON, &createdAtStr, &lastAccessedStr, &m.AccessCount, &embeddingBlob); err != nil {
			return nil, err
		}
		m.CreatedAt = parseTimeString(createdAtStr)
		m.LastAccessed = parseTimeString(lastAccessedStr)
		if err := json.Unmarshal([]byte(tagsJSON), &m.Tags); err != nil {
			m.Tags = []string{}
		}

		// Convert blob to vector and compute similarity
		memEmbedding := BlobToVector(embeddingBlob)
		if memEmbedding != nil {
			similarity := CosineSimilarity(embedding, memEmbedding)
			results = append(results, memoryWithScore{memory: m, similarity: similarity})
		}
	}

	// Sort by similarity descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].similarity > results[j].similarity
	})

	// Return top N
	var memories []*Memory
	for i := 0; i < len(results) && i < limit; i++ {
		memories = append(memories, results[i].memory)
	}

	return memories, nil
}

// Delete removes a memory
func (s *Store) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM memories WHERE id = ?", id)
	return err
}

// updateAccess updates the access statistics for a memory
func (s *Store) updateAccess(id string) {
	ctx := context.Background()
	_, _ = s.db.ExecContext(ctx, `
		UPDATE memories
		SET last_accessed = datetime('now'), access_count = access_count + 1
		WHERE id = ?
	`, id)
}

// GetTopImportant returns the most important memories
func (s *Store) GetTopImportant(ctx context.Context, limit int) ([]*Memory, error) {
	query := `
		SELECT id, content, importance, tags, created_at, last_accessed, access_count
		FROM memories
		ORDER BY importance DESC, access_count DESC
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memories []*Memory
	for rows.Next() {
		m := &Memory{}
		var tagsJSON string
		var createdAtStr, lastAccessedStr string
		if err := rows.Scan(&m.ID, &m.Content, &m.Importance, &tagsJSON, &createdAtStr, &lastAccessedStr, &m.AccessCount); err != nil {
			return nil, err
		}
		m.CreatedAt = parseTimeString(createdAtStr)
		m.LastAccessed = parseTimeString(lastAccessedStr)
		if err := json.Unmarshal([]byte(tagsJSON), &m.Tags); err != nil {
			m.Tags = []string{}
		}
		memories = append(memories, m)
	}

	return memories, nil
}

// UpdateEmbedding sets the embedding for an existing memory.
// Used for backfilling embeddings on memories that were created before embedding support.
func (s *Store) UpdateEmbedding(ctx context.Context, id string, embedding []float32) error {
	vec := Vector(embedding)
	_, err := s.db.ExecContext(ctx, "UPDATE memories SET embedding = ? WHERE id = ?", vec, id)
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
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memories []*Memory
	for rows.Next() {
		m := &Memory{}
		var tagsJSON string
		var createdAtStr, lastAccessedStr string
		if err := rows.Scan(&m.ID, &m.Content, &m.Importance, &tagsJSON, &createdAtStr, &lastAccessedStr, &m.AccessCount); err != nil {
			return nil, err
		}
		m.CreatedAt = parseTimeString(createdAtStr)
		m.LastAccessed = parseTimeString(lastAccessedStr)
		if err := json.Unmarshal([]byte(tagsJSON), &m.Tags); err != nil {
			m.Tags = []string{}
		}
		memories = append(memories, m)
	}

	return memories, nil
}
