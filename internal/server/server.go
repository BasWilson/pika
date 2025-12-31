package server

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log"

	"github.com/baswilson/pika/internal/actions"
	"github.com/baswilson/pika/internal/ai"
	"github.com/baswilson/pika/internal/calendar"
	"github.com/baswilson/pika/internal/config"
	"github.com/baswilson/pika/internal/database"
	"github.com/baswilson/pika/internal/memory"
	"github.com/baswilson/pika/internal/ws"
	"github.com/go-chi/chi/v5"
)

// Server is the main application server
type Server struct {
	config   *config.Config
	router   *chi.Mux
	hub      *ws.Hub
	db       *sql.DB
	dbDriver *database.SQLiteDriver
	ai       *ai.Service
	memory   *memory.Store
	calendar *calendar.Service
	actions  *actions.Registry
	webFS    fs.FS
}

// New creates a new Server instance
func New(cfg *config.Config, webFS fs.FS) (*Server, error) {
	// Connect to SQLite database
	driver, err := database.NewSQLiteDriver(cfg.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Initialize database schema
	if err := driver.Initialize(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	db := driver.DB()
	log.Printf("Connected to SQLite database: %s", cfg.DatabasePath)

	// Create services
	memoryStore := memory.NewStore(db)
	aiService := ai.NewService(cfg, memoryStore)
	calendarService := calendar.NewService(cfg, db)
	actionsRegistry := actions.NewRegistry(memoryStore, calendarService)

	// Wire up embedding generator for semantic memory search
	memoryStore.SetEmbedder(aiService)

	// Connect calendar to AI service for context
	aiService.SetCalendar(&calendarAdapter{calendarService})

	// Create WebSocket hub
	hub := ws.NewHub()
	go hub.Run()

	// Set up calendar reminders to broadcast to all clients
	calendarService.SetReminderCallback(func(event *calendar.Event, minutesBefore int) {
		var message string
		if minutesBefore == 5 {
			message = fmt.Sprintf("Heads up! '%s' starts in 5 minutes.", event.Title)
		} else {
			message = fmt.Sprintf("Reminder: '%s' starts in %d minutes.", event.Title, minutesBefore)
		}
		if event.Location != "" {
			message += fmt.Sprintf(" Location: %s", event.Location)
		}

		msg, err := ws.NewTrigger("reminder", "Upcoming Event", message, map[string]interface{}{
			"event_id":   event.ID,
			"title":      event.Title,
			"start_time": event.StartTime,
			"location":   event.Location,
		})
		if err == nil {
			hub.BroadcastMessage(msg)
			log.Printf("Reminder sent: %s", message)
		}
	})

	// Start calendar background sync
	calendarService.StartBackgroundSync()

	// Create server
	s := &Server{
		config:   cfg,
		router:   chi.NewRouter(),
		hub:      hub,
		db:       db,
		dbDriver: driver,
		ai:       aiService,
		memory:   memoryStore,
		calendar: calendarService,
		actions:  actionsRegistry,
		webFS:    webFS,
	}

	// Setup routes
	s.setupRoutes()

	return s, nil
}

// Router returns the HTTP router
func (s *Server) Router() *chi.Mux {
	return s.router
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	// Stop calendar sync
	s.calendar.StopBackgroundSync()

	if s.dbDriver != nil {
		if err := s.dbDriver.Close(); err != nil {
			return fmt.Errorf("failed to close database: %w", err)
		}
	}
	return nil
}

// Hub returns the WebSocket hub
func (s *Server) Hub() *ws.Hub {
	return s.hub
}

// BroadcastTrigger sends a trigger to all connected clients
func (s *Server) BroadcastTrigger(triggerType, title, message string, data interface{}) error {
	msg, err := ws.NewTrigger(triggerType, title, message, data)
	if err != nil {
		return err
	}
	return s.hub.BroadcastMessage(msg)
}

// calendarAdapter adapts calendar.Service to ai.CalendarProvider interface
type calendarAdapter struct {
	svc *calendar.Service
}

func (a *calendarAdapter) ListEvents(ctx context.Context) ([]*ai.CalendarEvent, error) {
	events, err := a.svc.ListEvents(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]*ai.CalendarEvent, len(events))
	for i, e := range events {
		result[i] = &ai.CalendarEvent{
			Title:     e.Title,
			StartTime: e.StartTime,
			EndTime:   e.EndTime,
			Location:  e.Location,
		}
	}
	return result, nil
}

func (a *calendarAdapter) IsInitialized() bool {
	return a.svc.IsInitialized()
}
