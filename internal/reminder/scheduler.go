package reminder

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// NotificationTier represents a notification time before the reminder
type NotificationTier struct {
	Name     string
	Duration time.Duration
	Field    string
}

// Standard notification tiers
var NotificationTiers = []NotificationTier{
	{"24h", 24 * time.Hour, "notified_24h"},
	{"12h", 12 * time.Hour, "notified_12h"},
	{"3h", 3 * time.Hour, "notified_3h"},
	{"1h", 1 * time.Hour, "notified_1h"},
	{"10m", 10 * time.Minute, "notified_10m"},
	{"at_time", 0, "notified_at_time"},
}

// ReminderCallback is called when a reminder notification should be sent
type ReminderCallback func(reminder *Reminder, tier string, timeUntil time.Duration)

// Scheduler checks for upcoming reminders and triggers notifications
type Scheduler struct {
	store    *Store
	callback ReminderCallback
	ticker   *time.Ticker
	stop     chan struct{}
	mu       sync.RWMutex
	running  bool
}

// NewScheduler creates a new reminder scheduler
func NewScheduler(store *Store) *Scheduler {
	return &Scheduler{
		store: store,
		stop:  make(chan struct{}),
	}
}

// SetCallback sets the notification callback
func (s *Scheduler) SetCallback(cb ReminderCallback) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callback = cb
}

// Start begins the scheduler, checking every minute
func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.ticker = time.NewTicker(1 * time.Minute)
	s.mu.Unlock()

	log.Println("Reminder scheduler started")

	// Run immediately on start
	go s.checkReminders()

	go func() {
		for {
			select {
			case <-s.ticker.C:
				s.checkReminders()
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
	log.Println("Reminder scheduler stopped")
}

// checkReminders checks all pending reminders and sends notifications
func (s *Scheduler) checkReminders() {
	s.mu.RLock()
	callback := s.callback
	s.mu.RUnlock()

	if callback == nil {
		return
	}

	ctx := context.Background()
	reminders, err := s.store.GetPendingReminders(ctx)
	if err != nil {
		log.Printf("Error fetching pending reminders: %v", err)
		return
	}

	now := time.Now()

	for _, r := range reminders {
		timeUntil := r.RemindAt.Sub(now)

		// Check each notification tier
		for _, tier := range NotificationTiers {
			if s.shouldNotify(r, tier, timeUntil) {
				log.Printf("Sending %s reminder for: %s", tier.Name, r.Title)

				// Send notification
				callback(r, tier.Name, timeUntil)

				// Mark as notified
				if err := s.store.MarkNotified(ctx, r.ID, tier.Name); err != nil {
					log.Printf("Error marking reminder as notified: %v", err)
				}
			}
		}
	}
}

// shouldNotify determines if a notification should be sent for a tier
func (s *Scheduler) shouldNotify(r *Reminder, tier NotificationTier, timeUntil time.Duration) bool {
	// Already notified for this tier?
	switch tier.Name {
	case "24h":
		if r.Notified24h {
			return false
		}
	case "12h":
		if r.Notified12h {
			return false
		}
	case "3h":
		if r.Notified3h {
			return false
		}
	case "1h":
		if r.Notified1h {
			return false
		}
	case "10m":
		if r.Notified10m {
			return false
		}
	case "at_time":
		if r.NotifiedAtTime {
			return false
		}
	}

	// For "at_time", trigger when the time has arrived or passed (within 1 minute window)
	if tier.Name == "at_time" {
		return timeUntil <= 1*time.Minute
	}

	// For other tiers, trigger when we're within the tier's window
	// but haven't passed it by too much (give a 2-minute grace window)
	return timeUntil <= tier.Duration && timeUntil > tier.Duration-2*time.Minute
}

// FormatTimeUntil formats the time until a reminder in a human-readable way
func FormatTimeUntil(d time.Duration) string {
	if d < 0 {
		return "now"
	}

	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60

	if hours >= 24 {
		days := hours / 24
		hours = hours % 24
		if hours > 0 {
			return fmt.Sprintf("%d days and %d hours", days, hours)
		}
		return fmt.Sprintf("%d days", days)
	}

	if hours > 0 {
		if minutes > 0 {
			return fmt.Sprintf("%d hours and %d minutes", hours, minutes)
		}
		return fmt.Sprintf("%d hours", hours)
	}

	if minutes > 0 {
		return fmt.Sprintf("%d minutes", minutes)
	}

	return "less than a minute"
}
