package valueobj

import "time"

// RobotsRule represents a single Allow or Disallow directive.
type RobotsRule struct {
	Path  string `json:"path"`
	Allow bool   `json:"allow"` // true = Allow, false = Disallow
}

// RobotsPolicy holds parsed robots.txt rules for a specific domain.
type RobotsPolicy struct {
	Domain     string       `json:"domain"`
	Rules      []RobotsRule `json:"rules"`
	CrawlDelay time.Duration `json:"crawl_delay"`
	Sitemaps   []string     `json:"sitemaps"`
	FetchedAt  time.Time    `json:"fetched_at"`
	ExpiresAt  time.Time    `json:"expires_at"`
}
