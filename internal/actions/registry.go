package actions

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/baswilson/pika/internal/ai"
	"github.com/baswilson/pika/internal/calendar"
	"github.com/baswilson/pika/internal/memory"
)

// ActionType represents the type of action
type ActionType string

const (
	ActionSaveToCalendar ActionType = "SAVE_TO_CALENDAR"
	ActionSaveMemory     ActionType = "SAVE_MEMORY"
	ActionNoAction       ActionType = "NO_ACTION"
)

// ActionResult represents the result of executing an action
type ActionResult struct {
	ActionType string      `json:"action_type"`
	Success    bool        `json:"success"`
	Data       interface{} `json:"data,omitempty"`
	Error      string      `json:"error,omitempty"`
}

// ActionHandler is a function that handles a specific action
type ActionHandler func(ctx context.Context, data map[string]interface{}) *ActionResult

// Registry manages action handlers
type Registry struct {
	handlers map[ActionType]ActionHandler
	memory   *memory.Store
	calendar *calendar.Service
}

// NewRegistry creates a new action registry
func NewRegistry(memoryStore *memory.Store, calendarService *calendar.Service) *Registry {
	r := &Registry{
		handlers: make(map[ActionType]ActionHandler),
		memory:   memoryStore,
		calendar: calendarService,
	}

	// Register built-in handlers
	r.Register(ActionSaveToCalendar, r.handleSaveToCalendar)
	r.Register(ActionSaveMemory, r.handleSaveMemory)

	return r
}

// Register adds an action handler
func (r *Registry) Register(actionType ActionType, handler ActionHandler) {
	r.handlers[actionType] = handler
}

// Execute runs an action and returns the result
func (r *Registry) Execute(action ai.Action) *ActionResult {
	ctx := context.Background()
	actionType := ActionType(action.Type)

	handler, ok := r.handlers[actionType]
	if !ok {
		return &ActionResult{
			ActionType: action.Type,
			Success:    false,
			Error:      fmt.Sprintf("unknown action type: %s", action.Type),
		}
	}

	log.Printf("Executing action: %s", action.Type)
	result := handler(ctx, action.Data)
	result.ActionType = action.Type

	return result
}

// handleSaveToCalendar creates a calendar event
func (r *Registry) handleSaveToCalendar(ctx context.Context, data map[string]interface{}) *ActionResult {
	title, _ := data["title"].(string)
	description, _ := data["description"].(string)
	startTime, _ := data["start_time"].(string)
	endTime, _ := data["end_time"].(string)
	location, _ := data["location"].(string)

	if title == "" || startTime == "" {
		return &ActionResult{
			Success: false,
			Error:   "title and start_time are required",
		}
	}

	// If end time not provided, default to 1 hour after start
	if endTime == "" {
		if t, err := time.Parse(time.RFC3339, startTime); err == nil {
			endTime = t.Add(time.Hour).Format(time.RFC3339)
		}
	}

	event, err := r.calendar.CreateEvent(ctx, title, description, startTime, endTime, location)
	if err != nil {
		return &ActionResult{
			Success: false,
			Error:   err.Error(),
		}
	}

	return &ActionResult{
		Success: true,
		Data:    event,
	}
}

// handleSaveMemory saves a memory
func (r *Registry) handleSaveMemory(ctx context.Context, data map[string]interface{}) *ActionResult {
	content, _ := data["content"].(string)
	importance, _ := data["importance"].(float64)
	tagsInterface, _ := data["tags"].([]interface{})

	if content == "" {
		return &ActionResult{
			Success: false,
			Error:   "content is required",
		}
	}

	// Default importance
	if importance == 0 {
		importance = 0.5
	}

	// Convert tags
	var tags []string
	for _, t := range tagsInterface {
		if s, ok := t.(string); ok {
			tags = append(tags, s)
		}
	}

	mem, err := r.memory.Create(ctx, content, importance, tags)
	if err != nil {
		return &ActionResult{
			Success: false,
			Error:   err.Error(),
		}
	}

	return &ActionResult{
		Success: true,
		Data:    mem,
	}
}
