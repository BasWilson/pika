package nudge

import (
	"log"
	"math/rand"
	"sync"
	"time"
)

// NudgeCallback is called when a nudge should be sent
type NudgeCallback func(message string, emotion string)

// Scheduler manages idle detection and nudge delivery
type Scheduler struct {
	callback NudgeCallback
	ticker   *time.Ticker
	stop     chan struct{}
	mu       sync.RWMutex
	running  bool

	// Activity tracking
	lastActivityTime time.Time
	lastNudgeTime    time.Time

	// Configuration
	idleThreshold  time.Duration // 10 minutes
	cooldownPeriod time.Duration // 30 minutes
	checkInterval  time.Duration // 1 minute
}

// NewScheduler creates a new nudge scheduler
func NewScheduler() *Scheduler {
	return &Scheduler{
		stop:             make(chan struct{}),
		lastActivityTime: time.Now(),
		lastNudgeTime:    time.Time{}, // Zero value - no nudge sent yet
		idleThreshold:    10 * time.Minute,
		cooldownPeriod:   30 * time.Minute,
		checkInterval:    1 * time.Minute,
	}
}

// SetCallback sets the nudge delivery callback
func (s *Scheduler) SetCallback(cb NudgeCallback) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callback = cb
}

// RecordActivity updates the last activity timestamp
func (s *Scheduler) RecordActivity() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastActivityTime = time.Now()
}

// Start begins the idle checking loop
func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.ticker = time.NewTicker(s.checkInterval)
	s.mu.Unlock()

	log.Println("Nudge scheduler started")

	go func() {
		for {
			select {
			case <-s.ticker.C:
				s.checkAndNudge()
			case <-s.stop:
				return
			}
		}
	}()
}

// Stop stops the scheduler
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	s.running = false
	if s.ticker != nil {
		s.ticker.Stop()
	}
	close(s.stop)
	log.Println("Nudge scheduler stopped")
}

// checkAndNudge checks if conditions are met to send a nudge
func (s *Scheduler) checkAndNudge() {
	s.mu.RLock()
	callback := s.callback
	lastActivity := s.lastActivityTime
	lastNudge := s.lastNudgeTime
	s.mu.RUnlock()

	if callback == nil {
		return
	}

	now := time.Now()
	idleDuration := now.Sub(lastActivity)

	// Check idle threshold (10 minutes)
	if idleDuration < s.idleThreshold {
		return
	}

	// Check cooldown (30 minutes since last nudge)
	if !lastNudge.IsZero() && now.Sub(lastNudge) < s.cooldownPeriod {
		return
	}

	// Generate and send nudge
	message, emotion := s.generateNudgeMessage()

	s.mu.Lock()
	s.lastNudgeTime = now
	s.mu.Unlock()

	log.Printf("Sending idle nudge: %s", message)
	callback(message, emotion)
}

// nudgeMessage holds a message and its associated emotion
type nudgeMessage struct {
	text    string
	emotion string
}

// generateNudgeMessage creates a context-aware nudge message
func (s *Scheduler) generateNudgeMessage() (string, string) {
	hour := time.Now().Hour()
	timeOfDay := getTimeOfDay(hour)

	// Get messages for the current time of day
	messages := getTimeAwareMessages(timeOfDay)

	// Random selection
	idx := rand.Intn(len(messages))
	return messages[idx].text, messages[idx].emotion
}

func getTimeAwareMessages(timeOfDay string) []nudgeMessage {
	// Generic messages that work any time
	messages := []nudgeMessage{
		{"Hey! Just checking in. Need any help with anything?", "curious"},
		{"I'm still here if you need me!", "helpful"},
		{"Anything I can help you with?", "helpful"},
		{"I'm getting a bit lonely over here! Got any questions for me?", "playful"},
	}

	// Add time-specific messages
	switch timeOfDay {
	case "morning":
		messages = append(messages,
			nudgeMessage{"Good morning! Ready to tackle the day?", "helpful"},
			nudgeMessage{"Morning! Want me to check your calendar for today?", "helpful"},
			nudgeMessage{"Rise and shine! Anything I can help you get started with?", "playful"},
		)
	case "afternoon":
		messages = append(messages,
			nudgeMessage{"How's your afternoon going? Need a hand with anything?", "curious"},
			nudgeMessage{"Afternoon check-in! Everything going smoothly?", "helpful"},
			nudgeMessage{"Taking a break? Let me know if you need anything!", "playful"},
		)
	case "evening":
		messages = append(messages,
			nudgeMessage{"Evening! Winding down or still going strong?", "curious"},
			nudgeMessage{"Anything I can help you wrap up today?", "helpful"},
			nudgeMessage{"Getting late! Don't forget to take a break if you need one.", "thoughtful"},
		)
	case "night":
		messages = append(messages,
			nudgeMessage{"Burning the midnight oil? I'm here if you need me!", "helpful"},
			nudgeMessage{"Late night session! Want me to set a reminder for anything?", "helpful"},
			nudgeMessage{"Still awake? Don't forget to rest!", "thoughtful"},
		)
	}

	return messages
}

func getTimeOfDay(hour int) string {
	switch {
	case hour >= 5 && hour < 12:
		return "morning"
	case hour >= 12 && hour < 17:
		return "afternoon"
	case hour >= 17 && hour < 21:
		return "evening"
	default:
		return "night"
	}
}
