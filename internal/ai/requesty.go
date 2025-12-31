package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/baswilson/pika/internal/config"
	"github.com/baswilson/pika/internal/memory"
	"github.com/sashabaranov/go-openai"
)

// stripMarkdownCodeFences removes markdown code fences from AI responses.
// Some models wrap JSON in ```json ... ``` blocks.
func stripMarkdownCodeFences(content string) string {
	content = strings.TrimSpace(content)

	// Match ```json or ``` at start and ``` at end
	re := regexp.MustCompile("^```(?:json)?\\s*\\n?")
	content = re.ReplaceAllString(content, "")

	// Remove trailing ```
	content = strings.TrimSuffix(strings.TrimSpace(content), "```")

	return strings.TrimSpace(content)
}

// CalendarProvider interface for fetching calendar events
type CalendarProvider interface {
	ListEvents(ctx context.Context) ([]*CalendarEvent, error)
	IsInitialized() bool
}

// CalendarEvent represents a calendar event for AI context
type CalendarEvent struct {
	Title     string
	StartTime time.Time
	EndTime   time.Time
	Location  string
}

// AIResponse represents the structured response from the AI
type AIResponse struct {
	Actions  []Action `json:"actions"`
	Response struct {
		Text    string `json:"text"`
		Emotion string `json:"emotion"`
	} `json:"response"`
}

// Action represents an action to be executed
type Action struct {
	Type string                 `json:"type"`
	Data map[string]interface{} `json:"data"`
}

// ResponsePayload for returning to WebSocket
type ResponsePayload struct {
	Text    string `json:"text"`
	Emotion string `json:"emotion"`
}

// Service handles AI interactions via requesty.ai
type Service struct {
	client      *openai.Client
	embedClient *openai.Client // Local Ollama client for embeddings
	embedModel  string
	model       string
	memory      *memory.Store
	calendar    CalendarProvider
}

// GenerateEmbedding creates a vector embedding for the given text using local Ollama.
// Returns a 768-dimensional vector suitable for pgvector storage.
func (s *Service) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	req := openai.EmbeddingRequest{
		Model: openai.EmbeddingModel(s.embedModel),
		Input: []string{text},
	}

	resp, err := s.embedClient.CreateEmbeddings(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}

	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("no embedding returned from model")
	}

	return resp.Data[0].Embedding, nil
}

// NewService creates a new AI service
func NewService(cfg *config.Config, memoryStore *memory.Store) *Service {
	clientConfig := openai.DefaultConfig(cfg.RequestyAPIKey)
	clientConfig.BaseURL = cfg.RequestyBaseURL

	// Local Ollama client for fast embeddings
	embedConfig := openai.DefaultConfig("ollama") // Key not required for Ollama
	embedConfig.BaseURL = cfg.OllamaURL + "/v1"

	return &Service{
		client:      openai.NewClientWithConfig(clientConfig),
		embedClient: openai.NewClientWithConfig(embedConfig),
		embedModel:  cfg.OllamaEmbedModel,
		model:       cfg.RequestyModel,
		memory:      memoryStore,
	}
}

// SetCalendar sets the calendar provider (called after calendar service is created)
func (s *Service) SetCalendar(cal CalendarProvider) {
	s.calendar = cal
}

// ProcessCommand sends a command to the AI and returns the response
func (s *Service) ProcessCommand(text string) (*ResponsePayload, []Action, error) {
	// Add timeout for AI requests
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log.Printf("Processing command: %s", text)
	log.Printf("Using model: %s", s.model)

	// Get relevant memories using vector similarity search
	var memories []string

	// Generate embedding for the query to enable semantic search
	queryEmbedding, err := s.GenerateEmbedding(ctx, text)
	if err != nil {
		log.Printf("Failed to generate query embedding, falling back to keyword search: %v", err)
		// Fall back to keyword search if embedding fails
		memories, _ = s.memory.SearchRelevant(ctx, text, 5)
	} else {
		// Use vector similarity search for semantic matching
		vectorResults, err := s.memory.SearchByVector(ctx, queryEmbedding, 5)
		if err != nil {
			log.Printf("Vector search failed, falling back to keyword search: %v", err)
			memories, _ = s.memory.SearchRelevant(ctx, text, 5)
		} else {
			for _, m := range vectorResults {
				memories = append(memories, m.Content)
			}
			log.Printf("Vector search returned %d results", len(memories))
		}
	}

	// Also get top important memories (ensures personal info is always included)
	topMemories, err := s.memory.GetTopImportant(ctx, 5)
	if err != nil {
		log.Printf("Failed to fetch top memories: %v", err)
	} else {
		// Add important memories that weren't already found by search
		seen := make(map[string]bool)
		for _, m := range memories {
			seen[m] = true
		}
		for _, m := range topMemories {
			if !seen[m.Content] {
				memories = append(memories, m.Content)
				seen[m.Content] = true
			}
		}
	}

	log.Printf("Memory context: %d memories loaded", len(memories))

	// Get upcoming calendar events
	var calendarEvents []string
	log.Printf("Calendar check: calendar=%v, initialized=%v", s.calendar != nil, s.calendar != nil && s.calendar.IsInitialized())
	if s.calendar != nil && s.calendar.IsInitialized() {
		events, err := s.calendar.ListEvents(ctx)
		if err != nil {
			log.Printf("Failed to fetch calendar events: %v", err)
		} else {
			for _, e := range events {
				if len(calendarEvents) >= 10 {
					break
				}
				eventStr := fmt.Sprintf("%s: %s", e.StartTime.Format("Mon Jan 2 3:04 PM"), e.Title)
				if e.Location != "" {
					eventStr += " at " + e.Location
				}
				calendarEvents = append(calendarEvents, eventStr)
			}
			log.Printf("Calendar context: %d events loaded", len(calendarEvents))
		}
	}

	// Build system prompt with context
	currentTime := time.Now().Format("Monday, January 2, 2006 3:04 PM MST")
	systemPrompt := BuildPromptWithContext(memories, calendarEvents, currentTime)

	// Create chat completion request
	// Note: Some models via OpenAI-compatible APIs don't support ResponseFormat
	req := openai.ChatCompletionRequest{
		Model: s.model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: systemPrompt,
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: text,
			},
		},
	}

	log.Printf("Sending request to AI...")
	resp, err := s.client.CreateChatCompletion(ctx, req)
	if err != nil {
		log.Printf("AI request error: %v", err)
		return nil, nil, fmt.Errorf("AI request failed: %w", err)
	}
	log.Printf("AI response received")

	if len(resp.Choices) == 0 {
		return nil, nil, fmt.Errorf("no response from AI")
	}

	// Parse the JSON response
	var aiResp AIResponse
	rawContent := resp.Choices[0].Message.Content
	log.Printf("Raw AI response: %s", rawContent)

	// Strip markdown code fences if present (some models wrap JSON in ```json ... ```)
	cleanContent := stripMarkdownCodeFences(rawContent)

	if err := json.Unmarshal([]byte(cleanContent), &aiResp); err != nil {
		// If JSON parsing fails, return the raw text as response
		log.Printf("Failed to parse AI JSON response: %v", err)
		return &ResponsePayload{
			Text:    rawContent,
			Emotion: "helpful",
		}, nil, nil
	}

	// Deduplicate actions by type+content
	seenActions := make(map[string]bool)
	var uniqueActions []Action
	for _, action := range aiResp.Actions {
		key := action.Type
		if content, ok := action.Data["content"].(string); ok {
			key += ":" + content
		}
		if title, ok := action.Data["title"].(string); ok {
			key += ":" + title
		}
		if !seenActions[key] {
			seenActions[key] = true
			uniqueActions = append(uniqueActions, action)
		}
	}

	log.Printf("Actions: %d total, %d unique", len(aiResp.Actions), len(uniqueActions))

	return &ResponsePayload{
		Text:    aiResp.Response.Text,
		Emotion: aiResp.Response.Emotion,
	}, uniqueActions, nil
}

// StreamCallback is called for each chunk of streamed response
type StreamCallback func(chunk string, done bool)

// ProcessCommandStream sends a command and streams the response
func (s *Service) ProcessCommandStream(text string, onChunk StreamCallback) (*ResponsePayload, []Action, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	log.Printf("Processing command (streaming): %s", text)

	// Get relevant memories
	memories, err := s.memory.SearchRelevant(ctx, text, 5)
	if err != nil {
		log.Printf("Failed to fetch memories: %v", err)
		memories = []string{}
	}

	currentTime := time.Now().Format("Monday, January 2, 2006 3:04 PM MST")
	systemPrompt := BuildPromptWithContext(memories, nil, currentTime)

	req := openai.ChatCompletionRequest{
		Model: s.model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
			{Role: openai.ChatMessageRoleUser, Content: text},
		},
		Stream: true,
	}

	stream, err := s.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		log.Printf("AI stream request error: %v", err)
		return nil, nil, fmt.Errorf("AI request failed: %w", err)
	}
	defer stream.Close()

	var fullResponse string

	for {
		response, err := stream.Recv()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			log.Printf("Stream error: %v", err)
			break
		}

		if len(response.Choices) > 0 {
			chunk := response.Choices[0].Delta.Content
			if chunk != "" {
				fullResponse += chunk
				onChunk(chunk, false)
			}
		}
	}

	// Signal completion
	onChunk("", true)

	log.Printf("Stream complete. Total length: %d", len(fullResponse))

	// Try to parse as JSON for actions
	var aiResp AIResponse
	if err := json.Unmarshal([]byte(fullResponse), &aiResp); err != nil {
		// Not JSON, return as plain text
		return &ResponsePayload{
			Text:    fullResponse,
			Emotion: "helpful",
		}, nil, nil
	}

	return &ResponsePayload{
		Text:    aiResp.Response.Text,
		Emotion: aiResp.Response.Emotion,
	}, aiResp.Actions, nil
}

// ProcessCommandWithHistory processes with conversation history
func (s *Service) ProcessCommandWithHistory(text string, history []openai.ChatCompletionMessage) (*ResponsePayload, []Action, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get relevant memories using vector similarity search
	var memories []string

	// Generate embedding for the query to enable semantic search
	queryEmbedding, err := s.GenerateEmbedding(ctx, text)
	if err != nil {
		log.Printf("Failed to generate query embedding, falling back to keyword search: %v", err)
		memories, _ = s.memory.SearchRelevant(ctx, text, 5)
	} else {
		// Use vector similarity search for semantic matching
		vectorResults, err := s.memory.SearchByVector(ctx, queryEmbedding, 5)
		if err != nil {
			log.Printf("Vector search failed, falling back to keyword search: %v", err)
			memories, _ = s.memory.SearchRelevant(ctx, text, 5)
		} else {
			for _, m := range vectorResults {
				memories = append(memories, m.Content)
			}
			log.Printf("Vector search returned %d results", len(memories))
		}
	}

	// Also get top important memories (ensures personal info is always included)
	topMemories, err := s.memory.GetTopImportant(ctx, 5)
	if err != nil {
		log.Printf("Failed to fetch top memories: %v", err)
	} else {
		seen := make(map[string]bool)
		for _, m := range memories {
			seen[m] = true
		}
		for _, m := range topMemories {
			if !seen[m.Content] {
				memories = append(memories, m.Content)
				seen[m.Content] = true
			}
		}
	}

	log.Printf("Memory context: %d memories loaded", len(memories))

	// Get upcoming calendar events
	var calendarEvents []string
	if s.calendar != nil && s.calendar.IsInitialized() {
		events, err := s.calendar.ListEvents(ctx)
		if err != nil {
			log.Printf("Failed to fetch calendar events: %v", err)
		} else {
			for _, e := range events {
				if len(calendarEvents) >= 10 {
					break
				}
				eventStr := fmt.Sprintf("%s: %s", e.StartTime.Local().Format("Mon Jan 2 3:04 PM"), e.Title)
				if e.Location != "" {
					eventStr += " at " + e.Location
				}
				calendarEvents = append(calendarEvents, eventStr)
			}
			log.Printf("Calendar context: %d events loaded", len(calendarEvents))
		}
	}

	// Build system prompt
	currentTime := time.Now().Format("Monday, January 2, 2006 3:04 PM MST")
	systemPrompt := BuildPromptWithContext(memories, calendarEvents, currentTime)

	// Build messages with history
	messages := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: systemPrompt,
		},
	}
	messages = append(messages, history...)
	messages = append(messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: text,
	})

	req := openai.ChatCompletionRequest{
		Model:    s.model,
		Messages: messages,
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		},
	}

	resp, err := s.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return nil, nil, fmt.Errorf("AI request failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, nil, fmt.Errorf("no response from AI")
	}

	var aiResp AIResponse
	if err := json.Unmarshal([]byte(resp.Choices[0].Message.Content), &aiResp); err != nil {
		return &ResponsePayload{
			Text:    resp.Choices[0].Message.Content,
			Emotion: "helpful",
		}, nil, nil
	}

	return &ResponsePayload{
		Text:    aiResp.Response.Text,
		Emotion: aiResp.Response.Emotion,
	}, aiResp.Actions, nil
}
