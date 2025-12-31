package server

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/baswilson/pika/internal/ai"
	"github.com/baswilson/pika/internal/ws"
	"github.com/go-chi/chi/v5"
	"github.com/sashabaranov/go-openai"
)

// setupRoutes configures all HTTP routes
func (s *Server) setupRoutes() {
	r := s.router

	// Middleware
	r.Use(CORSMiddleware)
	r.Use(ResponseFormatMiddleware)
	r.Use(LoggingMiddleware)

	// Static files
	fileServer := http.FileServer(http.Dir("web/static"))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	// Main UI
	r.Get("/", s.handleIndex)

	// WebSocket
	r.Get("/ws", s.handleWebSocket)

	// API routes
	r.Route("/api", func(r chi.Router) {
		r.Get("/health", s.handleHealth)
		r.Get("/status", s.handleStatus)

		// Memory endpoints
		r.Get("/memories", s.handleListMemories)
		r.Post("/memories", s.handleCreateMemory)

		// Calendar endpoints
		r.Get("/calendar/events", s.handleListCalendarEvents)
		r.Post("/calendar/events", s.handleCreateCalendarEvent)
	})

	// OAuth routes
	r.Route("/auth", func(r chi.Router) {
		r.Get("/google", s.handleGoogleAuth)
		r.Get("/google/callback", s.handleGoogleCallback)
	})
}

// handleIndex serves the main UI
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	tmplPath := filepath.Join("web", "templates", "index.html")
	basePath := filepath.Join("web", "templates", "base.html")

	tmpl, err := template.ParseFiles(basePath, tmplPath)
	if err != nil {
		log.Printf("Template error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"Title": "PIKA - Voice Assistant",
	}

	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("Template execution error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// handleWebSocket handles WebSocket connections
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	ws.ServeWs(s.hub, s.handleMessage, w, r)
}

// handleMessage processes incoming WebSocket messages
func (s *Server) handleMessage(client *ws.Client, msg *ws.Message) {
	switch msg.Type {
	case ws.MessageTypeCommand:
		s.handleCommand(client, msg)
	case ws.MessageTypeStatus:
		// Handle status updates from client
		log.Printf("Status update from client: %s", msg.RequestID)
	default:
		log.Printf("Unknown message type: %s", msg.Type)
	}
}

// handleCommand processes voice commands
func (s *Server) handleCommand(client *ws.Client, msg *ws.Message) {
	cmd, err := msg.ParseCommand()
	if err != nil {
		log.Printf("Failed to parse command: %v", err)
		errMsg, _ := ws.NewError("PARSE_ERROR", "Failed to parse command", err.Error())
		client.SendMessage(errMsg)
		return
	}

	log.Printf("Received command: %s (wake_word: %v)", cmd.Text, cmd.WakeWord)

	// Process through AI
	go s.processCommand(client, cmd, msg.RequestID)
}

// processCommand sends command to AI and handles response
func (s *Server) processCommand(client *ws.Client, cmd *ws.CommandPayload, requestID string) {
	log.Printf("[FLOW] processCommand started for: %s", cmd.Text)

	// Send processing status
	status, _ := ws.NewStatus("processing", true, "busy")
	client.SendMessage(status)

	// Add user message to conversation history
	client.AddToHistory("user", cmd.Text)

	// Convert history to OpenAI format
	history := convertToOpenAIMessages(client.GetHistory())

	// Process through AI service with conversation history
	log.Printf("[FLOW] Calling AI service with %d messages in history...", len(history))
	aiStart := time.Now()
	response, actions, err := s.ai.ProcessCommandWithHistory(cmd.Text, history)
	log.Printf("[FLOW] AI service returned in %v", time.Since(aiStart))
	if err != nil {
		log.Printf("AI processing error: %v", err)
		errMsg, _ := ws.NewError("AI_ERROR", "Failed to process command", err.Error())
		client.SendMessage(errMsg)

		status, _ := ws.NewStatus("idle", true, "ready")
		client.SendMessage(status)
		return
	}

	log.Printf("[FLOW] Got %d actions, sending response immediately", len(actions))

	// Send response immediately (for TTS and display)
	if response != nil {
		log.Printf("[FLOW] Sending response to client: %s", response.Text[:min(50, len(response.Text))])
		respMsg, _ := ws.NewResponse(response.Text, response.Emotion)
		client.SendMessage(respMsg)
		log.Printf("[FLOW] Response sent to client")

		// Add assistant response to conversation history
		client.AddToHistory("assistant", response.Text)
	}

	// Reset status
	status, _ = ws.NewStatus("idle", true, "ready")
	client.SendMessage(status)
	log.Printf("[FLOW] Status reset to idle")

	// Execute actions in background (memory saves, calendar events, etc.)
	for _, action := range actions {
		log.Printf("[FLOW] Spawning goroutine for action: %s", action.Type)
		go s.executeActionAsync(client, action)
	}
	log.Printf("[FLOW] All action goroutines spawned, processCommand returning")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// convertToOpenAIMessages converts conversation history to OpenAI message format
func convertToOpenAIMessages(history []ws.ConversationMessage) []openai.ChatCompletionMessage {
	messages := make([]openai.ChatCompletionMessage, len(history))
	for i, msg := range history {
		role := openai.ChatMessageRoleUser
		if msg.Role == "assistant" {
			role = openai.ChatMessageRoleAssistant
		}
		messages[i] = openai.ChatCompletionMessage{
			Role:    role,
			Content: msg.Content,
		}
	}
	return messages
}

// Actions that return data the user wants to see/hear or trigger frontend behavior
var queryActions = map[string]bool{
	"GET_WEATHER":    true,
	"SEARCH_POKEMON": true,
	"STOP_LISTENING": true,
}

// executeActionAsync runs an action in the background and notifies the client
func (s *Server) executeActionAsync(client *ws.Client, action ai.Action) {
	log.Printf("[ACTION] Starting async execution: %s", action.Type)
	start := time.Now()

	result := s.actions.Execute(action)

	elapsed := time.Since(start)

	if !result.Success {
		log.Printf("[ACTION] %s failed after %v: %s", action.Type, elapsed, result.Error)
		actionMsg, _ := ws.NewMessage(ws.MessageTypeAction, result)
		client.SendMessage(actionMsg)
	} else {
		log.Printf("[ACTION] %s completed successfully in %v", action.Type, elapsed)

		// For query actions (weather, pokemon), send the result back to the client
		if queryActions[action.Type] {
			log.Printf("[ACTION] Sending query result to client for: %s", action.Type)
			actionMsg, _ := ws.NewMessage(ws.MessageTypeAction, result)
			client.SendMessage(actionMsg)
		}
	}
}

// handleHealth returns health check
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "healthy",
		"name":   "PIKA",
	})
}

// handleStatus returns system status
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":             "ready",
		"connections":        s.hub.ClientCount(),
		"ai_status":          "ready",
		"calendar_connected": s.calendar.IsInitialized(),
	})
}

// handleListMemories returns stored memories
func (s *Server) handleListMemories(w http.ResponseWriter, r *http.Request) {
	memories, err := s.memory.List(r.Context(), 50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(memories)
}

// handleCreateMemory creates a new memory
func (s *Server) handleCreateMemory(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content    string   `json:"content"`
		Importance float64  `json:"importance"`
		Tags       []string `json:"tags"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	memory, err := s.memory.Create(r.Context(), req.Content, req.Importance, req.Tags)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(memory)
}

// handleListCalendarEvents returns calendar events
func (s *Server) handleListCalendarEvents(w http.ResponseWriter, r *http.Request) {
	events, err := s.calendar.ListEvents(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

// handleCreateCalendarEvent creates a calendar event
func (s *Server) handleCreateCalendarEvent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		StartTime   string `json:"start_time"`
		EndTime     string `json:"end_time"`
		Location    string `json:"location"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	event, err := s.calendar.CreateEvent(r.Context(), req.Title, req.Description, req.StartTime, req.EndTime, req.Location)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(event)
}

// handleGoogleAuth initiates Google OAuth flow
func (s *Server) handleGoogleAuth(w http.ResponseWriter, r *http.Request) {
	url := s.calendar.GetAuthURL()
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// handleGoogleCallback handles OAuth callback
func (s *Server) handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Missing authorization code", http.StatusBadRequest)
		return
	}

	if err := s.calendar.ExchangeCode(r.Context(), code); err != nil {
		log.Printf("OAuth exchange error: %v", err)
		http.Error(w, "Failed to exchange authorization code", http.StatusInternalServerError)
		return
	}

	// Redirect back to main page
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}
