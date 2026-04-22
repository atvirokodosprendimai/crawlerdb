package entities

import (
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/pkg/uid"
)

// Page stores the content fetched from a URL.
type Page struct {
	ID             string            `json:"id"`
	URLID          string            `json:"url_id"`
	JobID          string            `json:"job_id"`
	HTTPStatus     int               `json:"http_status"`
	ContentType    string            `json:"content_type"`
	ContentPath    string            `json:"content_path,omitempty"`
	ContentSize    int64             `json:"content_size,omitempty"`
	TransferObject string            `json:"transfer_object,omitempty"`
	Headers        map[string]string `json:"headers"`
	Title          string            `json:"title"`
	MetaTags       map[string]string `json:"meta_tags"`
	HTMLBody       string            `json:"html_body,omitempty"`
	TextContent    string            `json:"text_content,omitempty"`
	StructuredData []any             `json:"structured_data,omitempty"`
	Links          []DiscoveredLink  `json:"links"`
	RawContent     []byte            `json:"-"`
	FetchDuration  time.Duration     `json:"fetch_duration"`
	FetchedAt      time.Time         `json:"fetched_at"`
	CreatedAt      time.Time         `json:"created_at"`
}

// DiscoveredLink represents a link found on a page.
type DiscoveredLink struct {
	RawURL     string `json:"raw_url"`
	Normalized string `json:"normalized"`
	URLHash    string `json:"url_hash"`
	IsExternal bool   `json:"is_external"`
	Rel        string `json:"rel,omitempty"` // e.g. nofollow
	Anchor     string `json:"anchor,omitempty"`
}

// NewPage creates a new page record.
func NewPage(urlID, jobID string) *Page {
	now := time.Now().UTC()
	return &Page{
		ID:        uid.NewID(),
		URLID:     urlID,
		JobID:     jobID,
		CreatedAt: now,
	}
}

// CrawlResult wraps the outcome of crawling a single URL.
type CrawlResult struct {
	URL            *CrawlURL         `json:"url"`
	Page           *Page             `json:"page"`
	DiscoveredURLs []DiscoveredLink  `json:"discovered_urls"`
	Error          string            `json:"error,omitempty"`
	Success        bool              `json:"success"`
	AntiBotEvent   *AntiBotDetection `json:"antibot_event,omitempty"`
}

// AntiBotDetection records an anti-bot signal found during a crawl.
type AntiBotDetection struct {
	Detected  bool   `json:"detected"`
	EventType string `json:"event_type"`
	Provider  string `json:"provider"`
	Details   string `json:"details"`
}
