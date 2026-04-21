package entities

import (
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/pkg/uid"
)

// URLStatus represents the crawl state of a URL.
type URLStatus string

const (
	URLStatusPending  URLStatus = "pending"
	URLStatusCrawling URLStatus = "crawling"
	URLStatusDone     URLStatus = "done"
	URLStatusBlocked  URLStatus = "blocked"
	URLStatusError    URLStatus = "error"
)

// CrawlURL represents a URL discovered during crawling.
type CrawlURL struct {
	ID         string    `json:"id"`
	JobID      string    `json:"job_id"`
	RawURL     string    `json:"raw_url"`
	Normalized string    `json:"normalized"`
	URLHash    string    `json:"url_hash"`
	Depth      int       `json:"depth"`
	Status     URLStatus `json:"status"`
	RetryCount int       `json:"retry_count"`
	LastError  string    `json:"last_error,omitempty"`
	RevisitAt  time.Time `json:"revisit_at,omitzero"`
	FoundOn    string    `json:"found_on,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// NewCrawlURL creates a new URL in pending state.
func NewCrawlURL(jobID, rawURL, normalized, urlHash string, depth int, foundOn string) *CrawlURL {
	now := time.Now().UTC()
	return &CrawlURL{
		ID:         uid.NewID(),
		JobID:      jobID,
		RawURL:     rawURL,
		Normalized: normalized,
		URLHash:    urlHash,
		Depth:      depth,
		Status:     URLStatusPending,
		FoundOn:    foundOn,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

// Claim transitions URL from pending to crawling (worker picks it up).
func (u *CrawlURL) Claim() error {
	if u.Status != URLStatusPending {
		return ErrInvalidTransition
	}
	u.Status = URLStatusCrawling
	u.UpdatedAt = time.Now().UTC()
	return nil
}

// MarkDone transitions URL to done after successful crawl.
func (u *CrawlURL) MarkDone() error {
	if u.Status != URLStatusCrawling {
		return ErrInvalidTransition
	}
	u.Status = URLStatusDone
	u.UpdatedAt = time.Now().UTC()
	return nil
}

// MarkBlocked transitions URL to blocked (anti-bot detected).
func (u *CrawlURL) MarkBlocked() error {
	if u.Status != URLStatusCrawling {
		return ErrInvalidTransition
	}
	u.Status = URLStatusBlocked
	u.UpdatedAt = time.Now().UTC()
	return nil
}

// MarkError transitions URL to error state.
func (u *CrawlURL) MarkError() error {
	if u.Status != URLStatusCrawling {
		return ErrInvalidTransition
	}
	u.Status = URLStatusError
	u.UpdatedAt = time.Now().UTC()
	return nil
}

// Retry resets URL back to pending with incremented retry count.
func (u *CrawlURL) Retry() error {
	if u.Status != URLStatusError && u.Status != URLStatusBlocked {
		return ErrInvalidTransition
	}
	u.Status = URLStatusPending
	u.RetryCount++
	u.UpdatedAt = time.Now().UTC()
	return nil
}
