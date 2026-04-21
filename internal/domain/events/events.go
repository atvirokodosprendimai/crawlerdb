package events

import (
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
)

// Event is the base type for all domain events.
type Event struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
}

// JobCreated is emitted when a new crawl job is created.
type JobCreated struct {
	Event
	JobID   string `json:"job_id"`
	SeedURL string `json:"seed_url"`
}

// JobUpdated is emitted when a job's status changes.
type JobUpdated struct {
	Event
	JobID  string             `json:"job_id"`
	Status entities.JobStatus `json:"status"`
}

// URLDiscovered is emitted when new URLs are found on a page.
type URLDiscovered struct {
	Event
	JobID    string   `json:"job_id"`
	SourceURL string  `json:"source_url"`
	URLs     []string `json:"urls"`
}

// URLBlocked is emitted when a URL is blocked by anti-bot measures.
type URLBlocked struct {
	Event
	JobID    string `json:"job_id"`
	URL      string `json:"url"`
	Provider string `json:"provider"`
	Reason   string `json:"reason"`
}

// CaptchaDetected is emitted when a captcha is detected.
type CaptchaDetected struct {
	Event
	JobID    string `json:"job_id"`
	URL      string `json:"url"`
	Provider string `json:"provider"`
	Type     string `json:"captcha_type"`
}

// CaptchaSolved is emitted when a captcha is solved.
type CaptchaSolved struct {
	Event
	JobID    string `json:"job_id"`
	URL      string `json:"url"`
	Provider string `json:"provider"`
	Success  bool   `json:"success"`
}

// MetricsUpdated is emitted periodically with crawl metrics.
type MetricsUpdated struct {
	Event
	JobID        string  `json:"job_id"`
	PagesPerSec  float64 `json:"pages_per_sec"`
	QueueDepth   int     `json:"queue_depth"`
	PagesCrawled int     `json:"pages_crawled"`
	Errors       int     `json:"errors"`
}

// NewEvent creates a base event with the given type.
func NewEvent(eventType string) Event {
	return Event{
		Type:      eventType,
		Timestamp: time.Now().UTC(),
	}
}
