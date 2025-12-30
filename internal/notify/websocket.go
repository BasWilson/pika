package notify

import (
	"context"
	"encoding/json"

	"github.com/baswilson/pika/internal/ws"
)

// WebSocketNotifier sends notifications via WebSocket
type WebSocketNotifier struct {
	hub *ws.Hub
}

// NewWebSocketNotifier creates a WebSocket notifier
func NewWebSocketNotifier(hub *ws.Hub) *WebSocketNotifier {
	return &WebSocketNotifier{
		hub: hub,
	}
}

// Send sends a notification (broadcasts since we don't have user tracking)
func (n *WebSocketNotifier) Send(ctx context.Context, notification *Notification) error {
	return n.Broadcast(ctx, notification)
}

// SendToUser sends to a specific user (not implemented for basic WebSocket)
func (n *WebSocketNotifier) SendToUser(ctx context.Context, userID string, notification *Notification) error {
	// For now, broadcast to all
	// In future, implement user session tracking
	return n.Broadcast(ctx, notification)
}

// Broadcast sends to all connected WebSocket clients
func (n *WebSocketNotifier) Broadcast(ctx context.Context, notification *Notification) error {
	msg, err := ws.NewTrigger(
		notification.Type,
		notification.Title,
		notification.Message,
		notification.Data,
	)
	if err != nil {
		return err
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	n.hub.Broadcast(data)
	return nil
}

// Type returns the notifier type
func (n *WebSocketNotifier) Type() string {
	return "websocket"
}
