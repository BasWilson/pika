package calendar

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/baswilson/pika/internal/config"
	"github.com/google/uuid"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	gcalendar "google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

// Event represents a calendar event
type Event struct {
	ID          string    `json:"id"`
	GoogleID    string    `json:"google_id,omitempty"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	StartTime   time.Time `json:"start_time"`
	EndTime     time.Time `json:"end_time"`
	Location    string    `json:"location,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// Service handles Google Calendar operations
type Service struct {
	config         *oauth2.Config
	db             *sql.DB
	token          *oauth2.Token
	initialized    bool
	syncTicker     *time.Ticker
	stopSync       chan struct{}
	onReminder     func(event *Event, minutesBefore int)
	lastSyncTime   time.Time
	remindedEvents map[string]bool // Track which events we've reminded about
}

// NewService creates a new calendar service
func NewService(cfg *config.Config, db *sql.DB) *Service {
	oauthConfig := &oauth2.Config{
		ClientID:     cfg.GoogleClientID,
		ClientSecret: cfg.GoogleClientSecret,
		RedirectURL:  cfg.GoogleRedirectURL,
		Scopes: []string{
			gcalendar.CalendarEventsScope,
		},
		Endpoint: google.Endpoint,
	}

	s := &Service{
		config:         oauthConfig,
		db:             db,
		stopSync:       make(chan struct{}),
		remindedEvents: make(map[string]bool),
	}

	// Try to load existing token
	s.loadToken()

	return s
}

// SetReminderCallback sets the function to call when a reminder is triggered
func (s *Service) SetReminderCallback(cb func(event *Event, minutesBefore int)) {
	s.onReminder = cb
}

// StartBackgroundSync starts the background sync and reminder service
func (s *Service) StartBackgroundSync() {
	s.syncTicker = time.NewTicker(5 * time.Minute)

	go func() {
		// Initial sync
		s.syncFromGoogle()

		for {
			select {
			case <-s.syncTicker.C:
				s.syncFromGoogle()
				s.checkReminders()
			case <-s.stopSync:
				s.syncTicker.Stop()
				return
			}
		}
	}()

	// Also run reminder check more frequently (every minute)
	go func() {
		reminderTicker := time.NewTicker(1 * time.Minute)
		defer reminderTicker.Stop()

		for {
			select {
			case <-reminderTicker.C:
				s.checkReminders()
			case <-s.stopSync:
				return
			}
		}
	}()

	fmt.Println("Calendar background sync started (every 5 minutes)")
}

// StopBackgroundSync stops the background sync service
func (s *Service) StopBackgroundSync() {
	close(s.stopSync)
}

// syncFromGoogle fetches events from Google Calendar and caches them locally
func (s *Service) syncFromGoogle() {
	if !s.IsInitialized() {
		fmt.Println("Calendar sync skipped: not initialized")
		return
	}
	fmt.Println("Starting calendar sync from Google...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	srv, err := s.getClient(ctx)
	if err != nil {
		fmt.Printf("Calendar sync error: %v\n", err)
		return
	}

	// Fetch events for the next 7 days
	now := time.Now()
	timeMin := now.Format(time.RFC3339)
	timeMax := now.AddDate(0, 0, 7).Format(time.RFC3339)

	events, err := srv.Events.List("primary").
		ShowDeleted(false).
		SingleEvents(true).
		TimeMin(timeMin).
		TimeMax(timeMax).
		MaxResults(100).
		OrderBy("startTime").
		Do()

	if err != nil {
		fmt.Printf("Failed to fetch Google events: %v\n", err)
		return
	}

	// Sync each event to local database
	for _, item := range events.Items {
		s.upsertEventFromGoogle(ctx, item)
	}

	s.lastSyncTime = time.Now()
	fmt.Printf("Calendar synced: %d events cached\n", len(events.Items))
}

// upsertEventFromGoogle inserts or updates an event from Google Calendar
func (s *Service) upsertEventFromGoogle(ctx context.Context, item *gcalendar.Event) {
	startTime, _ := time.Parse(time.RFC3339, item.Start.DateTime)
	endTime, _ := time.Parse(time.RFC3339, item.End.DateTime)

	// Handle all-day events
	if item.Start.DateTime == "" && item.Start.Date != "" {
		startTime, _ = time.Parse("2006-01-02", item.Start.Date)
		endTime, _ = time.Parse("2006-01-02", item.End.Date)
	}

	// Convert to UTC for consistent storage and comparison
	startTimeUTC := startTime.UTC().Format("2006-01-02 15:04:05")
	endTimeUTC := endTime.UTC().Format("2006-01-02 15:04:05")

	query := `
		INSERT INTO calendar_events (id, google_event_id, title, description, start_time, end_time, location, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))
		ON CONFLICT (google_event_id) DO UPDATE SET
			title = excluded.title,
			description = excluded.description,
			start_time = excluded.start_time,
			end_time = excluded.end_time,
			location = excluded.location,
			updated_at = datetime('now')
	`

	_, err := s.db.ExecContext(ctx, query,
		uuid.New().String(),
		item.Id,
		item.Summary,
		item.Description,
		startTimeUTC,
		endTimeUTC,
		item.Location,
	)

	if err != nil {
		fmt.Printf("Failed to cache event %s: %v\n", item.Summary, err)
	}
}

// checkReminders checks for upcoming events and triggers reminders
func (s *Service) checkReminders() {
	if s.onReminder == nil {
		return
	}

	ctx := context.Background()
	now := time.Now().UTC()

	// Find events starting in the next 15 minutes
	query := `
		SELECT id, google_event_id, title, description, start_time, end_time, location
		FROM calendar_events
		WHERE start_time > ? AND start_time <= ?
		ORDER BY start_time ASC
	`

	// Check for 15-minute and 5-minute reminders
	reminderWindows := []struct {
		minutes int
		from    string
		to      string
	}{
		{15, now.Add(14 * time.Minute).Format("2006-01-02 15:04:05"), now.Add(16 * time.Minute).Format("2006-01-02 15:04:05")},
		{5, now.Add(4 * time.Minute).Format("2006-01-02 15:04:05"), now.Add(6 * time.Minute).Format("2006-01-02 15:04:05")},
	}

	for _, window := range reminderWindows {
		rows, err := s.db.QueryContext(ctx, query, window.from, window.to)
		if err != nil {
			continue
		}

		for rows.Next() {
			e := &Event{}
			var googleID, description, location sql.NullString
			var startTimeStr, endTimeStr string

			if err := rows.Scan(&e.ID, &googleID, &e.Title, &description, &startTimeStr, &endTimeStr, &location); err != nil {
				continue
			}

			e.StartTime = parseTimeString(startTimeStr)
			e.EndTime = parseTimeString(endTimeStr)

			if googleID.Valid {
				e.GoogleID = googleID.String
			}
			if description.Valid {
				e.Description = description.String
			}
			if location.Valid {
				e.Location = location.String
			}

			// Create unique key for this reminder
			reminderKey := fmt.Sprintf("%s-%d", e.ID, window.minutes)
			if !s.remindedEvents[reminderKey] {
				s.remindedEvents[reminderKey] = true
				s.onReminder(e, window.minutes)
			}
		}
		rows.Close()
	}

	// Clean up old reminded events - just limit size
	if len(s.remindedEvents) > 1000 {
		s.remindedEvents = make(map[string]bool)
	}
}

// GetAuthURL returns the OAuth authorization URL
func (s *Service) GetAuthURL() string {
	// Force consent to always get a refresh token
	return s.config.AuthCodeURL("state-token", oauth2.AccessTypeOffline, oauth2.ApprovalForce)
}

// ExchangeCode exchanges an authorization code for tokens
func (s *Service) ExchangeCode(ctx context.Context, code string) error {
	token, err := s.config.Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("failed to exchange code: %w", err)
	}

	s.token = token
	s.initialized = true

	// Save token to database
	if err := s.saveToken(ctx, token); err != nil {
		return err
	}

	// Trigger immediate sync now that we have valid credentials
	go s.syncFromGoogle()

	return nil
}

// IsInitialized returns whether the calendar service has valid credentials
func (s *Service) IsInitialized() bool {
	return s.initialized && s.token != nil
}

// ListEvents returns upcoming calendar events from local cache
func (s *Service) ListEvents(ctx context.Context) ([]*Event, error) {
	// Always use local cached events for speed
	// Background sync keeps them up to date
	return s.listLocalEvents(ctx)
}

// CreateEvent creates a new calendar event
func (s *Service) CreateEvent(ctx context.Context, title, description, startTime, endTime, location string) (*Event, error) {
	start, err := time.Parse(time.RFC3339, startTime)
	if err != nil {
		return nil, fmt.Errorf("invalid start time: %w", err)
	}

	end, err := time.Parse(time.RFC3339, endTime)
	if err != nil {
		return nil, fmt.Errorf("invalid end time: %w", err)
	}

	event := &Event{
		ID:          uuid.New().String(),
		Title:       title,
		Description: description,
		StartTime:   start,
		EndTime:     end,
		Location:    location,
		CreatedAt:   time.Now(),
	}

	// Save locally first
	if err := s.saveLocalEvent(ctx, event); err != nil {
		return nil, err
	}

	// If connected to Google, also create there
	if s.IsInitialized() {
		googleID, err := s.createGoogleEvent(ctx, event)
		if err != nil {
			// Log but don't fail - local event is saved
			fmt.Printf("Failed to create Google event: %v\n", err)
		} else {
			event.GoogleID = googleID
			s.updateEventGoogleID(ctx, event.ID, googleID)

			// Trigger a sync to get the latest events
			go s.syncFromGoogle()
		}
	}

	return event, nil
}

// getClient returns an HTTP client with auto-refreshing tokens
func (s *Service) getClient(ctx context.Context) (*gcalendar.Service, error) {
	tokenSource := s.config.TokenSource(ctx, s.token)

	// Get a potentially refreshed token
	newToken, err := tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	// If token was refreshed, save it
	if newToken.AccessToken != s.token.AccessToken {
		s.token = newToken
		if err := s.saveToken(ctx, newToken); err != nil {
			fmt.Printf("Warning: failed to save refreshed token: %v\n", err)
		}
	}

	client := oauth2.NewClient(ctx, tokenSource)
	return gcalendar.NewService(ctx, option.WithHTTPClient(client))
}

// createGoogleEvent creates an event in Google Calendar
func (s *Service) createGoogleEvent(ctx context.Context, event *Event) (string, error) {
	srv, err := s.getClient(ctx)
	if err != nil {
		return "", err
	}

	gEvent := &gcalendar.Event{
		Summary:     event.Title,
		Description: event.Description,
		Location:    event.Location,
		Start: &gcalendar.EventDateTime{
			DateTime: event.StartTime.Format(time.RFC3339),
		},
		End: &gcalendar.EventDateTime{
			DateTime: event.EndTime.Format(time.RFC3339),
		},
	}

	created, err := srv.Events.Insert("primary", gEvent).Do()
	if err != nil {
		return "", err
	}

	return created.Id, nil
}

// saveLocalEvent saves an event to the local database
func (s *Service) saveLocalEvent(ctx context.Context, event *Event) error {
	// Convert to UTC for consistent storage
	startTimeUTC := event.StartTime.UTC().Format("2006-01-02 15:04:05")
	endTimeUTC := event.EndTime.UTC().Format("2006-01-02 15:04:05")

	query := `
		INSERT INTO calendar_events (id, title, description, start_time, end_time, location, created_at)
		VALUES (?, ?, ?, ?, ?, ?, datetime('now'))
	`
	_, err := s.db.ExecContext(ctx, query,
		event.ID, event.Title, event.Description,
		startTimeUTC, endTimeUTC, event.Location)
	return err
}

// listLocalEvents returns events from local database
func (s *Service) listLocalEvents(ctx context.Context) ([]*Event, error) {
	query := `
		SELECT id, google_event_id, title, description, start_time, end_time, location, created_at
		FROM calendar_events
		WHERE start_time >= datetime('now')
		ORDER BY start_time ASC
		LIMIT 20
	`

	fmt.Printf("Querying local events...\n")
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		fmt.Printf("Error querying events: %v\n", err)
		return nil, err
	}
	defer rows.Close()

	var events []*Event
	for rows.Next() {
		e := &Event{}
		var googleID sql.NullString
		var description sql.NullString
		var location sql.NullString
		var startTimeStr, endTimeStr, createdAtStr string

		if err := rows.Scan(&e.ID, &googleID, &e.Title, &description, &startTimeStr, &endTimeStr, &location, &createdAtStr); err != nil {
			return nil, err
		}

		// Parse times - supports both old and new formats
		e.StartTime = parseTimeString(startTimeStr)
		e.EndTime = parseTimeString(endTimeStr)
		e.CreatedAt = parseTimeString(createdAtStr)

		if googleID.Valid {
			e.GoogleID = googleID.String
		}
		if description.Valid {
			e.Description = description.String
		}
		if location.Valid {
			e.Location = location.String
		}

		events = append(events, e)
	}

	fmt.Printf("listLocalEvents returning %d events\n", len(events))
	return events, nil
}

// updateEventGoogleID updates the Google event ID for a local event
func (s *Service) updateEventGoogleID(ctx context.Context, id, googleID string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE calendar_events SET google_event_id = ?, updated_at = datetime('now') WHERE id = ?",
		googleID, id)
	return err
}

// UpdateEvent updates an existing calendar event
func (s *Service) UpdateEvent(ctx context.Context, eventID string, title, description, startTime, endTime, location *string) (*Event, error) {
	// First, get the existing event
	event, err := s.GetEventByID(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("event not found: %w", err)
	}

	// Update fields if provided
	if title != nil {
		event.Title = *title
	}
	if description != nil {
		event.Description = *description
	}
	if startTime != nil {
		start, err := time.Parse(time.RFC3339, *startTime)
		if err != nil {
			return nil, fmt.Errorf("invalid start time: %w", err)
		}
		event.StartTime = start
	}
	if endTime != nil {
		end, err := time.Parse(time.RFC3339, *endTime)
		if err != nil {
			return nil, fmt.Errorf("invalid end time: %w", err)
		}
		event.EndTime = end
	}
	if location != nil {
		event.Location = *location
	}

	// Update locally - store times in UTC
	startTimeUTC := event.StartTime.UTC().Format("2006-01-02 15:04:05")
	endTimeUTC := event.EndTime.UTC().Format("2006-01-02 15:04:05")

	query := `
		UPDATE calendar_events
		SET title = ?, description = ?, start_time = ?, end_time = ?, location = ?, updated_at = datetime('now')
		WHERE id = ?
	`
	_, err = s.db.ExecContext(ctx, query,
		event.Title, event.Description, startTimeUTC, endTimeUTC, event.Location, eventID)
	if err != nil {
		return nil, fmt.Errorf("failed to update local event: %w", err)
	}

	// If connected to Google and event has Google ID, update there too
	if s.IsInitialized() && event.GoogleID != "" {
		if err := s.updateGoogleEvent(ctx, event); err != nil {
			fmt.Printf("Failed to update Google event: %v\n", err)
		} else {
			go s.syncFromGoogle()
		}
	}

	return event, nil
}

// DeleteEvent deletes a calendar event
func (s *Service) DeleteEvent(ctx context.Context, eventID string) error {
	// First, get the existing event to check for Google ID
	event, err := s.GetEventByID(ctx, eventID)
	if err != nil {
		return fmt.Errorf("event not found: %w", err)
	}

	// Delete locally
	_, err = s.db.ExecContext(ctx, "DELETE FROM calendar_events WHERE id = ?", eventID)
	if err != nil {
		return fmt.Errorf("failed to delete local event: %w", err)
	}

	// If connected to Google and event has Google ID, delete there too
	if s.IsInitialized() && event.GoogleID != "" {
		if err := s.deleteGoogleEvent(ctx, event.GoogleID); err != nil {
			fmt.Printf("Failed to delete Google event: %v\n", err)
		}
	}

	return nil
}

// GetEventByID retrieves an event by its ID
func (s *Service) GetEventByID(ctx context.Context, eventID string) (*Event, error) {
	query := `
		SELECT id, google_event_id, title, description, start_time, end_time, location, created_at
		FROM calendar_events
		WHERE id = ?
	`

	e := &Event{}
	var googleID, description, location sql.NullString
	var startTimeStr, endTimeStr, createdAtStr string

	err := s.db.QueryRowContext(ctx, query, eventID).Scan(
		&e.ID, &googleID, &e.Title, &description, &startTimeStr, &endTimeStr, &location, &createdAtStr)
	if err != nil {
		return nil, err
	}

	// Parse times - try both formats for backwards compatibility
	e.StartTime = parseTimeString(startTimeStr)
	e.EndTime = parseTimeString(endTimeStr)
	e.CreatedAt = parseTimeString(createdAtStr)

	if googleID.Valid {
		e.GoogleID = googleID.String
	}
	if description.Valid {
		e.Description = description.String
	}
	if location.Valid {
		e.Location = location.String
	}

	return e, nil
}

// parseTimeString parses a time string in various formats
func parseTimeString(s string) time.Time {
	// Try UTC format first (new format)
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return t.UTC()
	}
	// Try RFC3339 with timezone (old format from Go's time.Time)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	// Try other common formats
	for _, format := range []string{"2006-01-02T15:04:05Z07:00", "2006-01-02 15:04:05-07:00"} {
		if t, err := time.Parse(format, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// FindEventByTitle searches for events by title (partial match)
func (s *Service) FindEventByTitle(ctx context.Context, titleSearch string) ([]*Event, error) {
	query := `
		SELECT id, google_event_id, title, description, start_time, end_time, location, created_at
		FROM calendar_events
		WHERE title LIKE ?
		ORDER BY start_time ASC
		LIMIT 10
	`

	rows, err := s.db.QueryContext(ctx, query, "%"+titleSearch+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*Event
	for rows.Next() {
		e := &Event{}
		var googleID, description, location sql.NullString
		var startTimeStr, endTimeStr, createdAtStr string

		if err := rows.Scan(&e.ID, &googleID, &e.Title, &description, &startTimeStr, &endTimeStr, &location, &createdAtStr); err != nil {
			return nil, err
		}

		e.StartTime = parseTimeString(startTimeStr)
		e.EndTime = parseTimeString(endTimeStr)
		e.CreatedAt = parseTimeString(createdAtStr)

		if googleID.Valid {
			e.GoogleID = googleID.String
		}
		if description.Valid {
			e.Description = description.String
		}
		if location.Valid {
			e.Location = location.String
		}

		events = append(events, e)
	}

	return events, nil
}

// updateGoogleEvent updates an event in Google Calendar
func (s *Service) updateGoogleEvent(ctx context.Context, event *Event) error {
	srv, err := s.getClient(ctx)
	if err != nil {
		return err
	}

	gEvent := &gcalendar.Event{
		Summary:     event.Title,
		Description: event.Description,
		Location:    event.Location,
		Start: &gcalendar.EventDateTime{
			DateTime: event.StartTime.Format(time.RFC3339),
		},
		End: &gcalendar.EventDateTime{
			DateTime: event.EndTime.Format(time.RFC3339),
		},
	}

	_, err = srv.Events.Update("primary", event.GoogleID, gEvent).Do()
	return err
}

// deleteGoogleEvent deletes an event from Google Calendar
func (s *Service) deleteGoogleEvent(ctx context.Context, googleID string) error {
	srv, err := s.getClient(ctx)
	if err != nil {
		return err
	}

	return srv.Events.Delete("primary", googleID).Do()
}

// loadToken loads the OAuth token from the database
func (s *Service) loadToken() {
	ctx := context.Background()
	query := `
		SELECT access_token, refresh_token, token_type, expiry
		FROM oauth_tokens
		WHERE provider = 'google'
		ORDER BY created_at DESC
		LIMIT 1
	`

	var accessToken, refreshToken, tokenType, expiryStr string

	err := s.db.QueryRowContext(ctx, query).Scan(&accessToken, &refreshToken, &tokenType, &expiryStr)
	if err != nil {
		if err != sql.ErrNoRows {
			fmt.Printf("Failed to load OAuth token: %v\n", err)
		}
		return
	}

	// Parse expiry time - try multiple formats
	var expiry time.Time
	for _, format := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02T15:04:05Z"} {
		if parsed, err := time.Parse(format, expiryStr); err == nil {
			expiry = parsed
			break
		}
	}

	s.token = &oauth2.Token{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    tokenType,
		Expiry:       expiry,
	}
	s.initialized = true
	fmt.Println("Google Calendar token loaded from database")
}

// saveToken saves the OAuth token to the database
func (s *Service) saveToken(ctx context.Context, token *oauth2.Token) error {
	// Delete existing tokens
	_, _ = s.db.ExecContext(ctx, "DELETE FROM oauth_tokens WHERE provider = 'google'")

	query := `
		INSERT INTO oauth_tokens (id, provider, access_token, refresh_token, token_type, expiry)
		VALUES (?, 'google', ?, ?, ?, ?)
	`
	_, err := s.db.ExecContext(ctx, query,
		uuid.New().String(), token.AccessToken, token.RefreshToken, token.TokenType, token.Expiry)
	return err
}
