package reminder

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// Reminder represents a reminder in the system
type Reminder struct {
	ID             string    `json:"id"`
	Title          string    `json:"title"`
	Description    string    `json:"description"`
	RemindAt       time.Time `json:"remind_at"`
	Notified24h    bool      `json:"notified_24h"`
	Notified12h    bool      `json:"notified_12h"`
	Notified3h     bool      `json:"notified_3h"`
	Notified1h     bool      `json:"notified_1h"`
	Notified10m    bool      `json:"notified_10m"`
	NotifiedAtTime bool      `json:"notified_at_time"`
	Completed      bool      `json:"completed"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Store handles reminder persistence
type Store struct {
	db *sql.DB
}

// NewStore creates a new reminder store
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Create creates a new reminder
func (s *Store) Create(ctx context.Context, title, description string, remindAt time.Time) (*Reminder, error) {
	id := uuid.New().String()
	now := time.Now()

	query := `
		INSERT INTO reminders (id, title, description, remind_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query, id, title, description, remindAt.Format(time.RFC3339), now.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}

	return &Reminder{
		ID:          id,
		Title:       title,
		Description: description,
		RemindAt:    remindAt,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// Get retrieves a reminder by ID
func (s *Store) Get(ctx context.Context, id string) (*Reminder, error) {
	query := `
		SELECT id, title, description, remind_at, notified_24h, notified_12h, notified_3h, notified_1h, notified_10m, notified_at_time, completed, created_at, updated_at
		FROM reminders
		WHERE id = ?
	`

	r := &Reminder{}
	var remindAtStr, createdAtStr, updatedAtStr string
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&r.ID, &r.Title, &r.Description, &remindAtStr,
		&r.Notified24h, &r.Notified12h, &r.Notified3h, &r.Notified1h, &r.Notified10m, &r.NotifiedAtTime,
		&r.Completed, &createdAtStr, &updatedAtStr,
	)
	if err != nil {
		return nil, err
	}

	r.RemindAt = parseTime(remindAtStr)
	r.CreatedAt = parseTime(createdAtStr)
	r.UpdatedAt = parseTime(updatedAtStr)

	return r, nil
}

// List returns all reminders, optionally filtered by completion status
func (s *Store) List(ctx context.Context, includeCompleted bool) ([]*Reminder, error) {
	query := `
		SELECT id, title, description, remind_at, notified_24h, notified_12h, notified_3h, notified_1h, notified_10m, notified_at_time, completed, created_at, updated_at
		FROM reminders
	`
	if !includeCompleted {
		query += " WHERE completed = 0"
	}
	query += " ORDER BY remind_at ASC"

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reminders []*Reminder
	for rows.Next() {
		r := &Reminder{}
		var remindAtStr, createdAtStr, updatedAtStr string
		if err := rows.Scan(
			&r.ID, &r.Title, &r.Description, &remindAtStr,
			&r.Notified24h, &r.Notified12h, &r.Notified3h, &r.Notified1h, &r.Notified10m, &r.NotifiedAtTime,
			&r.Completed, &createdAtStr, &updatedAtStr,
		); err != nil {
			return nil, err
		}
		r.RemindAt = parseTime(remindAtStr)
		r.CreatedAt = parseTime(createdAtStr)
		r.UpdatedAt = parseTime(updatedAtStr)
		reminders = append(reminders, r)
	}

	return reminders, rows.Err()
}

// Update updates a reminder
func (s *Store) Update(ctx context.Context, id string, title, description *string, remindAt *time.Time) (*Reminder, error) {
	// First get the current reminder
	r, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Apply updates
	if title != nil {
		r.Title = *title
	}
	if description != nil {
		r.Description = *description
	}
	if remindAt != nil {
		r.RemindAt = *remindAt
		// Reset notification flags if time changed
		r.Notified24h = false
		r.Notified12h = false
		r.Notified3h = false
		r.Notified1h = false
		r.Notified10m = false
		r.NotifiedAtTime = false
	}
	r.UpdatedAt = time.Now()

	query := `
		UPDATE reminders
		SET title = ?, description = ?, remind_at = ?,
		    notified_24h = ?, notified_12h = ?, notified_3h = ?, notified_1h = ?, notified_10m = ?, notified_at_time = ?,
		    updated_at = ?
		WHERE id = ?
	`

	_, err = s.db.ExecContext(ctx, query,
		r.Title, r.Description, r.RemindAt.Format(time.RFC3339),
		r.Notified24h, r.Notified12h, r.Notified3h, r.Notified1h, r.Notified10m, r.NotifiedAtTime,
		r.UpdatedAt.Format(time.RFC3339), id,
	)
	if err != nil {
		return nil, err
	}

	return r, nil
}

// Delete removes a reminder
func (s *Store) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM reminders WHERE id = ?", id)
	return err
}

// MarkCompleted marks a reminder as completed
func (s *Store) MarkCompleted(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE reminders SET completed = 1, updated_at = ? WHERE id = ?",
		time.Now().Format(time.RFC3339), id,
	)
	return err
}

// FindByTitle finds reminders matching a title (case-insensitive partial match)
func (s *Store) FindByTitle(ctx context.Context, title string) ([]*Reminder, error) {
	query := `
		SELECT id, title, description, remind_at, notified_24h, notified_12h, notified_3h, notified_1h, notified_10m, notified_at_time, completed, created_at, updated_at
		FROM reminders
		WHERE LOWER(title) LIKE '%' || LOWER(?) || '%' AND completed = 0
		ORDER BY remind_at ASC
	`

	rows, err := s.db.QueryContext(ctx, query, title)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reminders []*Reminder
	for rows.Next() {
		r := &Reminder{}
		var remindAtStr, createdAtStr, updatedAtStr string
		if err := rows.Scan(
			&r.ID, &r.Title, &r.Description, &remindAtStr,
			&r.Notified24h, &r.Notified12h, &r.Notified3h, &r.Notified1h, &r.Notified10m, &r.NotifiedAtTime,
			&r.Completed, &createdAtStr, &updatedAtStr,
		); err != nil {
			return nil, err
		}
		r.RemindAt = parseTime(remindAtStr)
		r.CreatedAt = parseTime(createdAtStr)
		r.UpdatedAt = parseTime(updatedAtStr)
		reminders = append(reminders, r)
	}

	return reminders, rows.Err()
}

// GetPendingReminders returns reminders that need notification checks
func (s *Store) GetPendingReminders(ctx context.Context) ([]*Reminder, error) {
	// Get all non-completed reminders where we haven't sent all notifications yet
	query := `
		SELECT id, title, description, remind_at, notified_24h, notified_12h, notified_3h, notified_1h, notified_10m, notified_at_time, completed, created_at, updated_at
		FROM reminders
		WHERE completed = 0 AND notified_at_time = 0
		ORDER BY remind_at ASC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reminders []*Reminder
	for rows.Next() {
		r := &Reminder{}
		var remindAtStr, createdAtStr, updatedAtStr string
		if err := rows.Scan(
			&r.ID, &r.Title, &r.Description, &remindAtStr,
			&r.Notified24h, &r.Notified12h, &r.Notified3h, &r.Notified1h, &r.Notified10m, &r.NotifiedAtTime,
			&r.Completed, &createdAtStr, &updatedAtStr,
		); err != nil {
			return nil, err
		}
		r.RemindAt = parseTime(remindAtStr)
		r.CreatedAt = parseTime(createdAtStr)
		r.UpdatedAt = parseTime(updatedAtStr)
		reminders = append(reminders, r)
	}

	return reminders, rows.Err()
}

// MarkNotified marks a specific notification tier as sent
func (s *Store) MarkNotified(ctx context.Context, id string, tier string) error {
	var column string
	switch tier {
	case "24h":
		column = "notified_24h"
	case "12h":
		column = "notified_12h"
	case "3h":
		column = "notified_3h"
	case "1h":
		column = "notified_1h"
	case "10m":
		column = "notified_10m"
	case "at_time":
		column = "notified_at_time"
	default:
		return nil
	}

	query := "UPDATE reminders SET " + column + " = 1, updated_at = ? WHERE id = ?"
	_, err := s.db.ExecContext(ctx, query, time.Now().Format(time.RFC3339), id)
	return err
}

// parseTime parses a time string from SQLite
func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z",
	}
	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
