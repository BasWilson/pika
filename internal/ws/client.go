package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512 * 1024 // 512KB
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins in development
	},
}

// Client represents a WebSocket client connection
type Client struct {
	hub     *Hub
	conn    *websocket.Conn
	send    chan []byte
	format  ResponseFormat
	handler MessageHandler
}

// MessageHandler processes incoming messages
type MessageHandler func(client *Client, msg *Message)

// NewClient creates a new WebSocket client
func NewClient(hub *Hub, conn *websocket.Conn, handler MessageHandler) *Client {
	return &Client{
		hub:     hub,
		conn:    conn,
		send:    make(chan []byte, 256),
		format:  FormatHTMX,
		handler: handler,
	}
}

// ServeWs handles WebSocket requests from clients
func ServeWs(hub *Hub, handler MessageHandler, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	client := NewClient(hub, conn, handler)
	hub.Register(client)

	// Start read and write pumps
	go client.writePump()
	go client.readPump()

	// Send initial status
	status, _ := NewStatus("idle", true, "ready")
	data, _ := json.Marshal(status)
	client.Send(data)
}

// readPump pumps messages from the WebSocket connection to the hub
func (c *Client) readPump() {
	defer func() {
		c.hub.Unregister(c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, messageBytes, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket read error: %v", err)
			}
			break
		}

		var msg Message
		if err := json.Unmarshal(messageBytes, &msg); err != nil {
			log.Printf("Invalid message format: %v", err)
			continue
		}

		// Update client's preferred format if specified
		if msg.Format != "" {
			c.format = msg.Format
		}

		// Handle the message
		if c.handler != nil {
			c.handler(c, &msg)
		}
	}
}

// writePump pumps messages from the hub to the WebSocket connection
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Batch queued messages
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// Send sends a message to this client
func (c *Client) Send(message []byte) {
	select {
	case c.send <- message:
	default:
		log.Printf("Client send buffer full, dropping message")
	}
}

// SendMessage sends a Message struct to this client
func (c *Client) SendMessage(msg *Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	c.Send(data)
	return nil
}

// GetFormat returns the client's preferred response format
func (c *Client) GetFormat() ResponseFormat {
	return c.format
}
