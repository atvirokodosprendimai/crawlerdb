package valueobj

import "time"

// AppConfig is the top-level application configuration.
type AppConfig struct {
	NATS     NATSConfig     `toml:"nats"`
	Database DatabaseConfig `toml:"database"`
	Crawler  CrawlerDefaults `toml:"crawler"`
	Server   ServerConfig   `toml:"server"`
}

// NATSConfig holds NATS connection settings.
type NATSConfig struct {
	URL            string   `toml:"url"`
	MaxReconnects  int      `toml:"max_reconnects"`
	ReconnectWait  Duration `toml:"reconnect_wait"`
	RequestTimeout Duration `toml:"request_timeout"`
}

// DatabaseConfig holds SQLite/GORM settings.
type DatabaseConfig struct {
	Path string `toml:"path"`
}

// CrawlerDefaults holds default crawler settings.
type CrawlerDefaults struct {
	UserAgent          string   `toml:"user_agent"`
	MaxDepth           int      `toml:"max_depth"`
	PoolSize           int      `toml:"pool_size"`
	RequestTimeout     Duration `toml:"request_timeout"`
	DefaultRateLimit   Duration `toml:"default_rate_limit"`
	MaxRetries         int      `toml:"max_retries"`
	RobotsTTL          Duration `toml:"robots_ttl"`
	HeartbeatInterval  Duration `toml:"heartbeat_interval"`
	HeartbeatTTL       Duration `toml:"heartbeat_ttl"`
	DomainConcurrency  int      `toml:"domain_concurrency"`
	DataDir            string   `toml:"data_dir"`
}

// ServerConfig holds GUI HTTP server settings.
type ServerConfig struct {
	Addr string `toml:"addr"`
}

// DefaultAppConfig returns a configuration with sensible defaults.
func DefaultAppConfig() AppConfig {
	return AppConfig{
		NATS: NATSConfig{
			URL:            "nats://localhost:4222",
			MaxReconnects:  60,
			ReconnectWait:  Duration{2 * time.Second},
			RequestTimeout: Duration{10 * time.Second},
		},
		Database: DatabaseConfig{
			Path: "crawlerdb.sqlite",
		},
		Crawler: CrawlerDefaults{
			UserAgent:         "CrawlerDB/1.0",
			MaxDepth:          10,
			PoolSize:          10,
			RequestTimeout:    Duration{30 * time.Second},
			DefaultRateLimit:  Duration{time.Second},
			MaxRetries:        3,
			RobotsTTL:         Duration{24 * time.Hour},
			HeartbeatInterval: Duration{5 * time.Second},
			HeartbeatTTL:      Duration{15 * time.Second},
			DomainConcurrency: 2,
			DataDir:           ".crawlerdb",
		},
		Server: ServerConfig{
			Addr: ":8080",
		},
	}
}
