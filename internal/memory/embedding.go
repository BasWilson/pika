package memory

import "context"

// EmbeddingGenerator generates vector embeddings for text content.
// This interface allows the memory store to request embeddings without
// depending directly on the AI service.
type EmbeddingGenerator interface {
	GenerateEmbedding(ctx context.Context, text string) ([]float32, error)
}
