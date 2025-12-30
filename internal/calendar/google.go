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
	config        *oauth2.Config
	db            *sql.DB
	token         *oauth2.Token
	initialized   bool
	syncTicker    *time.Ticker
	stopSync      chan struct{}
	onReminder    func(event *Event, minutesBefore int)
	lastSyncTime  time.Time
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
		return
	}

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

	query := `
		INSERT INTO calendar_events (id, google_event_id, title, description, start_time, end_time, location, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
		ON CONFLICT (google_event_id) DO UPDATE SET
			title = EXCLUDED.title,
			description = EXCLUDED.description,
			start_time = EXCLUDED.start_time,
			end_time = EXCLUDED.end_time,
			location = EXCLUDED.location,
			updated_at = NOW()
	`

	_, err := s.db.ExecContext(ctx, query,
		uuid.New().String(),
		item.Id,
		item.Summary,
		item.Description,
		startTime,
		endTime,
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
	now := time.Now()

	// Find events starting in the next 15 minutes
	query := `
		SELECT id, google_event_id, title, description, start_time, end_time, location
		FROM calendar_events
		WHERE start_time > $1 AND start_time <= $2
		ORDER BY start_time ASC
	`

	// Check for 15-minute and 5-minute reminders
	reminderWindows := []struct {
		minutes int
		from    time.Time
		to      time.Time
	}{
		{15, now.Add(14 * time.Minute), now.Add(16 * time.Minute)},
		{5, now.Add(4 * time.Minute), now.Add(6 * time.Minute)},
	}

	for _, window := range reminderWindows {
		rows, err := s.db.QueryContext(ctx, query, window.from, window.to)
		if err != nil {
			continue
		}

		for rows.Next() {
			e := &Event{}
			var googleID, description, location sql.NullString

			if err := rows.Scan(&e.ID, &googleID, &e.Title, &description, &e.StartTime, &e.EndTime, &location); err != nil {
				continue
			}

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
	return s.saveToken(ctx, token)
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
	query := `
		INSERT INTO calendar_events (id, title, description, start_time, end_time, location, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := s.db.ExecContext(ctx, query,
		event.ID, event.Title, event.Description,
		event.StartTime, event.EndTime, event.Location, event.CreatedAt)
	return err
}

// listLocalEvents returns events from local database
func (s *Service) listLocalEvents(ctx context.Context) ([]*Event, error) {
	query := `
		SELECT id, google_event_id, title, description, start_time, end_time, location, created_at
		FROM calendar_events
		WHERE start_time >= NOW()
		ORDER BY start_time ASC
		LIMIT 20
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*Event
	for rows.Next() {
		e := &Event{}
		var googleID sql.NullString
		var description sql.NullString
		var location sql.NullString

		if err := rows.Scan(&e.ID, &googleID, &e.Title, &description, &e.StartTime, &e.EndTime, &location, &e.CreatedAt); err != nil {
			return nil, err
		}

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

// updateEventGoogleID updates the Google event ID for a local event
func (s *Service) updateEventGoogleID(ctx context.Context, id, googleID string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE calendar_events SET google_event_id = $1, updated_at = NOW() WHERE id = $2",
		googleID, id)
	return err
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

	var accessToken, refreshToken, tokenType string
	var expiry time.Time

	err := s.db.QueryRowContext(ctx, query).Scan(&accessToken, &refreshToken, &tokenType, &expiry)
	if err != nil {
		return
	}

	s.token = &oauth2.Token{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    tokenType,
		Expiry:       expiry,
	}
	s.initialized = true
}

// saveToken saves the OAuth token to the database
func (s *Service) saveToken(ctx context.Context, token *oauth2.Token) error {
	// Delete existing tokens
	_, _ = s.db.ExecContext(ctx, "DELETE FROM oauth_tokens WHERE provider = 'google'")

	query := `
		INSERT INTO oauth_tokens (provider, access_token, refresh_token, token_type, expiry)
		VALUES ('google', $1, $2, $3, $4)
	`
	_, err := s.db.ExecContext(ctx, query,
		token.AccessToken, token.RefreshToken, token.TokenType, token.Expiry)
	return err
}
