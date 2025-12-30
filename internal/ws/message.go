package ws

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// MessageType represents the type of WebSocket message
type MessageType string

const (
	MessageTypeCommand  MessageType = "command"  // User voice command (client -> server)
	MessageTypeResponse MessageType = "response" // AI response (server -> client)
	MessageTypeStream   MessageType = "stream"   // Streaming AI response chunk (server -> client)
	MessageTypeAction   MessageType = "action"   // Action execution result (server -> client)
	MessageTypeStatus   MessageType = "status"   // System status updates (bidirectional)
	MessageTypeTrigger  MessageType = "trigger"  // PIKA-initiated interaction (server -> client)
	MessageTypeError    MessageType = "error"    // Error message (server -> client)
)

// ResponseFormat represents how the client wants responses
type ResponseFormat string

const (
	FormatHTMX ResponseFormat = "htmx" // Return HTML partials for HTMX
	FormatJSON ResponseFormat = "json" // Return raw JSON
	FormatPush ResponseFormat = "push" // Return push notification format
)

// Message is the base WebSocket message structure
type Message struct {
	Type      MessageType     `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	RequestID string          `json:"request_id"`
	Format    ResponseFormat  `json:"format,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

// CommandPayload is sent when user issues a voice command
type CommandPayload struct {
	Text       string `json:"text"`        // The transcribed text
	WakeWord   bool   `json:"wake_word"`   // Whether wake word was detected
	Confidence float64 `json:"confidence"` // Speech recognition confidence
}

// ResponsePayload is sent as AI response to user
type ResponsePayload struct {
	Text    string `json:"text"`
	Emotion string `json:"emotion,omitempty"` // helpful, curious, alert, etc.
	HTML    string `json:"html,omitempty"`    // Pre-rendered HTML for HTMX
}

// StreamPayload is sent for streaming AI responses
type StreamPayload struct {
	Chunk    string `json:"chunk"`              // The text chunk
	Done     bool   `json:"done"`               // Whether streaming is complete
	FullText string `json:"full_text,omitempty"` // Full text when done
}

// ActionPayload reports action execution results
type ActionPayload struct {
	ActionType string      `json:"action_type"`
	Success    bool        `json:"success"`
	Data       interface{} `json:"data,omitempty"`
	Error      string      `json:"error,omitempty"`
}

// StatusPayload for system status updates
type StatusPayload struct {
	Status    string `json:"status"`    // listening, processing, speaking, idle
	Connected bool   `json:"connected"` // WebSocket connection status
	AIStatus  string `json:"ai_status"` // ready, busy, error
}

// TriggerPayload for PIKA-initiated interactions
type TriggerPayload struct {
	TriggerType string      `json:"trigger_type"` // reminder, suggestion, alert
	Title       string      `json:"title"`
	Message     string      `json:"message"`
	Data        interface{} `json:"data,omitempty"`
	Priority    string      `json:"priority,omitempty"` // low, normal, high
}

// ErrorPayload for error messages
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

// NewMessage creates a new message with a generated request ID
func NewMessage(msgType MessageType, payload interface{}) (*Message, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	return &Message{
		Type:      msgType,
		Payload:   payloadBytes,
		RequestID: uuid.New().String(),
		Timestamp: time.Now(),
	}, nil
}

// NewResponse creates a response message
func NewResponse(text, emotion string) (*Message, error) {
	return NewMessage(MessageTypeResponse, ResponsePayload{
		Text:    text,
		Emotion: emotion,
	})
}

// NewStreamChunk creates a streaming chunk message
func NewStreamChunk(chunk string, done bool, fullText string) (*Message, error) {
	return NewMessage(MessageTypeStream, StreamPayload{
		Chunk:    chunk,
		Done:     done,
		FullText: fullText,
	})
}

// NewStatus creates a status message
func NewStatus(status string, connected bool, aiStatus string) (*Message, error) {
	return NewMessage(MessageTypeStatus, StatusPayload{
		Status:    status,
		Connected: connected,
		AIStatus:  aiStatus,
	})
}

// NewError creates an error message
func NewError(code, message, details string) (*Message, error) {
	return NewMessage(MessageTypeError, ErrorPayload{
		Code:    code,
		Message: message,
		Details: details,
	})
}

// NewTrigger creates a trigger message
func NewTrigger(triggerType, title, message string, data interface{}) (*Message, error) {
	return NewMessage(MessageTypeTrigger, TriggerPayload{
		TriggerType: triggerType,
		Title:       title,
		Message:     message,
		Data:        data,
	})
}

// ParseCommand extracts CommandPayload from a message
func (m *Message) ParseCommand() (*CommandPayload, error) {
	var cmd CommandPayload
	if err := json.Unmarshal(m.Payload, &cmd); err != nil {
		return nil, err
	}
	return &cmd, nil
}
