package memory

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"
)

// Vector represents a pgvector-compatible vector embedding
type Vector []float32

// Value implements driver.Valuer for database insertion
// Converts the vector to pgvector's string format: [1.0,2.0,3.0,...]
func (v Vector) Value() (driver.Value, error) {
	if v == nil {
		return nil, nil
	}
	if len(v) == 0 {
		return nil, nil
	}

	strs := make([]string, len(v))
	for i, f := range v {
		strs[i] = strconv.FormatFloat(float64(f), 'f', -1, 32)
	}
	return "[" + strings.Join(strs, ",") + "]", nil
}

// Scan implements sql.Scanner for reading vectors from the database
func (v *Vector) Scan(src interface{}) error {
	if src == nil {
		*v = nil
		return nil
	}

	var str string
	switch s := src.(type) {
	case string:
		str = s
	case []byte:
		str = string(s)
	default:
		return fmt.Errorf("unsupported type for Vector: %T", src)
	}

	// Parse pgvector format: [1.0,2.0,3.0,...]
	str = strings.TrimPrefix(str, "[")
	str = strings.TrimSuffix(str, "]")
	if str == "" {
		*v = nil
		return nil
	}

	parts := strings.Split(str, ",")
	result := make(Vector, len(parts))
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return fmt.Errorf("failed to parse vector element %d: %w", i, err)
		}
		result[i] = float32(f)
	}

	*v = result
	return nil
}
