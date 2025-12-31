package memory

import (
	"database/sql/driver"
	"encoding/binary"
	"fmt"
	"math"
)

// Vector represents a vector embedding for SQLite storage.
// Stored as binary BLOB (little-endian float32 array) for efficiency.
type Vector []float32

// Value implements driver.Valuer for database insertion.
// Converts the vector to a binary blob.
func (v Vector) Value() (driver.Value, error) {
	if v == nil || len(v) == 0 {
		return nil, nil
	}

	buf := make([]byte, len(v)*4)
	for i, f := range v {
		bits := math.Float32bits(f)
		binary.LittleEndian.PutUint32(buf[i*4:], bits)
	}
	return buf, nil
}

// Scan implements sql.Scanner for reading vectors from the database.
func (v *Vector) Scan(src interface{}) error {
	if src == nil {
		*v = nil
		return nil
	}

	var buf []byte
	switch s := src.(type) {
	case []byte:
		buf = s
	default:
		return fmt.Errorf("unsupported type for SQLiteVector: %T", src)
	}

	if len(buf) == 0 {
		*v = nil
		return nil
	}

	if len(buf)%4 != 0 {
		return fmt.Errorf("invalid vector blob size: %d (must be multiple of 4)", len(buf))
	}

	result := make(Vector, len(buf)/4)
	for i := range result {
		bits := binary.LittleEndian.Uint32(buf[i*4:])
		result[i] = math.Float32frombits(bits)
	}

	*v = result
	return nil
}

// ToBlob converts a float32 slice to binary blob for SQLite storage.
func VectorToBlob(v []float32) []byte {
	if v == nil || len(v) == 0 {
		return nil
	}
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		bits := math.Float32bits(f)
		binary.LittleEndian.PutUint32(buf[i*4:], bits)
	}
	return buf
}

// BlobToVector converts a binary blob back to float32 slice.
func BlobToVector(buf []byte) []float32 {
	if len(buf) == 0 || len(buf)%4 != 0 {
		return nil
	}
	result := make([]float32, len(buf)/4)
	for i := range result {
		bits := binary.LittleEndian.Uint32(buf[i*4:])
		result[i] = math.Float32frombits(bits)
	}
	return result
}

// CosineSimilarity computes the cosine similarity between two vectors.
// Returns a value between -1 and 1, where 1 means identical direction.
func CosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dotProduct, normA, normB float32
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}

// EuclideanDistance computes the Euclidean distance between two vectors.
func EuclideanDistance(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return float32(math.MaxFloat32)
	}

	var sum float32
	for i := range a {
		diff := a[i] - b[i]
		sum += diff * diff
	}

	return float32(math.Sqrt(float64(sum)))
}

// VectorMatch represents a vector search result with similarity score.
type VectorMatch struct {
	ID         string
	Similarity float32
}

// BySimiliarity implements sort.Interface for sorting by similarity (descending).
type BySimilarity []VectorMatch

func (a BySimilarity) Len() int           { return len(a) }
func (a BySimilarity) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a BySimilarity) Less(i, j int) bool { return a[i].Similarity > a[j].Similarity }
