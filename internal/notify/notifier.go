package notify

import (
	"context"
)

// Notification represents a notification to send
type Notification struct {
	Type     string      `json:"type"`     // reminder, suggestion, alert
	Title    string      `json:"title"`
	Message  string      `json:"message"`
	Data     interface{} `json:"data,omitempty"`
	Priority string      `json:"priority,omitempty"` // low, normal, high
}

// Notifier is the interface for sending notifications
type Notifier interface {
	// Send sends a notification
	Send(ctx context.Context, notification *Notification) error

	// SendToUser sends to a specific user/client
	SendToUser(ctx context.Context, userID string, notification *Notification) error

	// Broadcast sends to all connected clients
	Broadcast(ctx context.Context, notification *Notification) error

	// Type returns the notifier type (websocket, push, etc.)
	Type() string
}

// Manager manages multiple notifiers
type Manager struct {
	notifiers []Notifier
}

// NewManager creates a new notification manager
func NewManager() *Manager {
	return &Manager{
		notifiers: make([]Notifier, 0),
	}
}

// Register adds a notifier
func (m *Manager) Register(n Notifier) {
	m.notifiers = append(m.notifiers, n)
}

// Broadcast sends notification through all registered notifiers
func (m *Manager) Broadcast(ctx context.Context, notification *Notification) error {
	var lastErr error
	for _, n := range m.notifiers {
		if err := n.Broadcast(ctx, notification); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// Send sends through the first available notifier
func (m *Manager) Send(ctx context.Context, notification *Notification) error {
	for _, n := range m.notifiers {
		if err := n.Send(ctx, notification); err == nil {
			return nil
		}
	}
	return nil
}

// SendViaType sends through a specific notifier type
func (m *Manager) SendViaType(ctx context.Context, notifierType string, notification *Notification) error {
	for _, n := range m.notifiers {
		if n.Type() == notifierType {
			return n.Send(ctx, notification)
		}
	}
	return nil
}
