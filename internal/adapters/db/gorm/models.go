package store

import (
	"time"
)

// JobModel is the GORM model for the jobs table.
type JobModel struct {
	ID         string    `gorm:"primaryKey;column:id"`
	SeedURL    string    `gorm:"column:seed_url;not null"`
	Config     string    `gorm:"column:config;not null"`
	Status     string    `gorm:"column:status;not null;default:pending"`
	Stats      string    `gorm:"column:stats;default:'{}'"`
	Error      string    `gorm:"column:error;default:''"`
	CreatedAt  time.Time `gorm:"column:created_at;not null"`
	UpdatedAt  time.Time `gorm:"column:updated_at;not null"`
	StartedAt  *time.Time `gorm:"column:started_at"`
	FinishedAt *time.Time `gorm:"column:finished_at"`
}

func (JobModel) TableName() string { return "jobs" }

// URLModel is the GORM model for the urls table.
type URLModel struct {
	ID         string     `gorm:"primaryKey;column:id"`
	JobID      string     `gorm:"column:job_id;not null"`
	RawURL     string     `gorm:"column:raw_url;not null"`
	Normalized string     `gorm:"column:normalized;not null"`
	URLHash    string     `gorm:"column:url_hash;not null"`
	Depth      int        `gorm:"column:depth;not null;default:0"`
	Status     string     `gorm:"column:status;not null;default:pending"`
	RetryCount int        `gorm:"column:retry_count;not null;default:0"`
	RevisitAt  *time.Time `gorm:"column:revisit_at"`
	FoundOn    string     `gorm:"column:found_on;default:''"`
	CreatedAt  time.Time  `gorm:"column:created_at;not null"`
	UpdatedAt  time.Time  `gorm:"column:updated_at;not null"`
}

func (URLModel) TableName() string { return "urls" }

// PageModel is the GORM model for the pages table.
type PageModel struct {
	ID             string    `gorm:"primaryKey;column:id"`
	URLID          string    `gorm:"column:url_id;not null"`
	JobID          string    `gorm:"column:job_id;not null"`
	HTTPStatus     int       `gorm:"column:http_status"`
	ContentType    string    `gorm:"column:content_type;default:''"`
	Headers        string    `gorm:"column:headers;default:'{}'"`
	Title          string    `gorm:"column:title;default:''"`
	MetaTags       string    `gorm:"column:meta_tags;default:'{}'"`
	HTMLBody       string    `gorm:"column:html_body;default:''"`
	TextContent    string    `gorm:"column:text_content;default:''"`
	StructuredData string    `gorm:"column:structured_data;default:'[]'"`
	Links          string    `gorm:"column:links;default:'[]'"`
	FetchDuration  int64     `gorm:"column:fetch_duration;default:0"`
	FetchedAt      time.Time `gorm:"column:fetched_at;not null"`
	CreatedAt      time.Time `gorm:"column:created_at;not null"`
}

func (PageModel) TableName() string { return "pages" }

// RobotsCacheModel is the GORM model for the robots_cache table.
type RobotsCacheModel struct {
	Domain    string    `gorm:"primaryKey;column:domain"`
	Content   string    `gorm:"column:content;not null"`
	Parsed    string    `gorm:"column:parsed;not null"`
	FetchedAt time.Time `gorm:"column:fetched_at;not null"`
	ExpiresAt time.Time `gorm:"column:expires_at;not null"`
}

func (RobotsCacheModel) TableName() string { return "robots_cache" }

// AntiBotEventModel is the GORM model for the antibot_events table.
type AntiBotEventModel struct {
	ID        string    `gorm:"primaryKey;column:id"`
	URLID     string    `gorm:"column:url_id;not null"`
	JobID     string    `gorm:"column:job_id;not null"`
	EventType string    `gorm:"column:event_type;not null"`
	Provider  string    `gorm:"column:provider;default:''"`
	Strategy  string    `gorm:"column:strategy;not null"`
	Resolved  bool      `gorm:"column:resolved;not null;default:false"`
	Details   string    `gorm:"column:details;default:'{}'"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
}

func (AntiBotEventModel) TableName() string { return "antibot_events" }
