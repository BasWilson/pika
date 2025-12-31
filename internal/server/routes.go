package server

import (
	"encoding/json"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os/exec"
	"strings"
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

	// Static files from embedded filesystem
	staticFS, err := fs.Sub(s.webFS, "web/static")
	if err != nil {
		log.Printf("Warning: could not create static sub-filesystem: %v", err)
	} else {
		fileServer := http.FileServer(http.FS(staticFS))
		r.Handle("/static/*", http.StripPrefix("/static/", fileServer))
	}

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

		// Reminder endpoints
		r.Get("/reminders", s.handleListReminders)
		r.Post("/reminders", s.handleCreateReminder)
		r.Get("/reminders/{id}", s.handleGetReminder)
		r.Put("/reminders/{id}", s.handleUpdateReminder)
		r.Delete("/reminders/{id}", s.handleDeleteReminder)
		r.Post("/reminders/{id}/complete", s.handleCompleteReminder)

		// Game endpoints
		r.Post("/game/move", s.handleGameMove)
	})

	// OAuth routes
	r.Route("/auth", func(r chi.Router) {
		r.Get("/google", s.handleGoogleAuth)
		r.Get("/google/callback", s.handleGoogleCallback)
	})

	// Utility routes
	r.Post("/open-url", s.handleOpenURL)
	r.Post("/api/reset", s.handleReset)
}

// handleIndex serves the main UI
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	// Parse templates from embedded filesystem
	tmpl, err := template.ParseFS(s.webFS, "web/templates/base.html", "web/templates/index.html")
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
	// Record user activity for idle nudge tracking
	if s.nudgeScheduler != nil {
		s.nudgeScheduler.RecordActivity()
	}

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
	"LIST_REMINDERS": true,
	"START_GAME":     true,
	"GAME_MOVE":      true,
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

	// Broadcast success message to all connected clients (triggers TTS)
	responseMsg, _ := ws.NewResponse("Google Calendar is now connected! I can help you manage your schedule.", "helpful")
	s.hub.BroadcastMessage(responseMsg)

	// Also send an action to dismiss the connect popup
	actionMsg, _ := ws.NewMessage(ws.MessageTypeAction, ws.ActionPayload{
		ActionType: "GOOGLE_CONNECTED",
		Success:    true,
	})
	s.hub.BroadcastMessage(actionMsg)

	// Show success page that closes itself
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`<!DOCTYPE html>
<html>
<head>
	<title>PIKA - Connected</title>
	<style>
		body {
			background: #0a0a0f;
			color: #fbbf24;
			font-family: -apple-system, BlinkMacSystemFont, sans-serif;
			display: flex;
			justify-content: center;
			align-items: center;
			height: 100vh;
			margin: 0;
			text-align: center;
		}
		.container { max-width: 400px; }
		h1 { font-size: 2rem; margin-bottom: 10px; }
		p { color: #888; margin-bottom: 20px; }
		.success { font-size: 3rem; margin-bottom: 20px; }
	</style>
</head>
<body>
	<div class="container">
		<div class="success">✓</div>
		<h1>Google Calendar Connected!</h1>
		<p>You can close this tab and return to PIKA.</p>
	</div>
	<script>
		// Try to close this tab after a short delay
		setTimeout(function() {
			window.close();
		}, 2000);
	</script>
</body>
</html>`))
}

// handleOpenURL opens a URL in the system browser
func (s *Server) handleOpenURL(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	url := strings.TrimSpace(string(body))
	if url == "" {
		http.Error(w, "No URL provided", http.StatusBadRequest)
		return
	}
	// Open URL in system browser using macOS 'open' command
	exec.Command("open", url).Start()
	w.WriteHeader(http.StatusOK)
}

// handleReset clears the app configuration to trigger setup wizard on next launch
func (s *Server) handleReset(w http.ResponseWriter, r *http.Request) {
	// Delete all config entries from app_config table
	_, err := s.db.Exec("DELETE FROM app_config")
	if err != nil {
		log.Printf("Failed to reset config: %v", err)
		http.Error(w, "Failed to reset configuration", http.StatusInternalServerError)
		return
	}

	log.Println("App configuration reset - setup wizard will show on next launch")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "reset",
		"message": "Configuration cleared. Please restart the app.",
	})
}

// handleListReminders returns all reminders
func (s *Server) handleListReminders(w http.ResponseWriter, r *http.Request) {
	includeCompleted := r.URL.Query().Get("include_completed") == "true"

	reminders, err := s.reminder.List(r.Context(), includeCompleted)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(reminders)
}

// handleCreateReminder creates a new reminder
func (s *Server) handleCreateReminder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		RemindAt    string `json:"remind_at"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Title == "" || req.RemindAt == "" {
		http.Error(w, "title and remind_at are required", http.StatusBadRequest)
		return
	}

	remindAt, err := time.Parse(time.RFC3339, req.RemindAt)
	if err != nil {
		http.Error(w, "invalid remind_at format, use RFC3339", http.StatusBadRequest)
		return
	}

	reminder, err := s.reminder.Create(r.Context(), req.Title, req.Description, remindAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(reminder)
}

// handleGetReminder returns a specific reminder
func (s *Server) handleGetReminder(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	reminder, err := s.reminder.Get(r.Context(), id)
	if err != nil {
		http.Error(w, "reminder not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(reminder)
}

// handleUpdateReminder updates a reminder
func (s *Server) handleUpdateReminder(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		Title       *string `json:"title"`
		Description *string `json:"description"`
		RemindAt    *string `json:"remind_at"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var remindAt *time.Time
	if req.RemindAt != nil {
		t, err := time.Parse(time.RFC3339, *req.RemindAt)
		if err != nil {
			http.Error(w, "invalid remind_at format, use RFC3339", http.StatusBadRequest)
			return
		}
		remindAt = &t
	}

	reminder, err := s.reminder.Update(r.Context(), id, req.Title, req.Description, remindAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(reminder)
}

// handleDeleteReminder deletes a reminder
func (s *Server) handleDeleteReminder(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := s.reminder.Delete(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleCompleteReminder marks a reminder as completed
func (s *Server) handleCompleteReminder(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := s.reminder.MarkCompleted(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "completed"})
}

// handleGameMove processes a game move directly (bypasses AI)
func (s *Server) handleGameMove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Move      string `json:"move"`
		GameState struct {
			GameType      string `json:"game_type"`
			CurrentNumber int    `json:"current_number"`
			TargetNumber  int    `json:"target_number"`
			Streak        int    `json:"streak"`
			BestStreak    int    `json:"best_streak"`
		} `json:"game_state"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Build action data
	actionData := map[string]interface{}{
		"move":           req.Move,
		"current_number": float64(req.GameState.CurrentNumber),
		"target_number":  float64(req.GameState.TargetNumber),
		"streak":         float64(req.GameState.Streak),
		"best_streak":    float64(req.GameState.BestStreak),
	}

	// Execute via registry
	result := s.actions.Execute(ai.Action{
		Type: "GAME_MOVE",
		Data: actionData,
	})

	w.Header().Set("Content-Type", "application/json")
	if !result.Success {
		w.WriteHeader(http.StatusBadRequest)
	}
	json.NewEncoder(w).Encode(result)
}
