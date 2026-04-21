package valueobj

import "fmt"

// CrawlScope defines how far the crawler follows links.
type CrawlScope string

const (
	ScopeSameDomain       CrawlScope = "same_domain"
	ScopeIncludeSubdomain CrawlScope = "include_subdomains"
	ScopeFollowExternals  CrawlScope = "follow_externals"
)

// ExtractionLevel defines what content to extract from pages.
type ExtractionLevel string

const (
	ExtractionMinimal  ExtractionLevel = "minimal"
	ExtractionStandard ExtractionLevel = "standard"
	ExtractionFull     ExtractionLevel = "full"
)

// AntiBotMode defines how to respond to bot detection.
type AntiBotMode string

const (
	AntiBotSkip   AntiBotMode = "skip"
	AntiBotRotate AntiBotMode = "rotate"
	AntiBotSolve  AntiBotMode = "solve"
)

// CrawlConfig defines per-job crawl parameters.
type CrawlConfig struct {
	Scope          CrawlScope      `json:"scope"`
	MaxDepth       int             `json:"max_depth"`
	ExternalDepth  int             `json:"external_depth"` // Only used when Scope == ScopeFollowExternals
	Extraction     ExtractionLevel `json:"extraction"`
	RateLimit      Duration        `json:"rate_limit"`
	MaxConcurrency int             `json:"max_concurrency"`
	AntiBotMode    AntiBotMode     `json:"antibot_mode"`
	UserAgent      string          `json:"user_agent"`
	ProxyList      []string        `json:"proxy_list,omitempty"`
	RevisitTTL     Duration        `json:"revisit_ttl"`
}

// Validate checks the crawl config for errors.
func (c CrawlConfig) Validate() error {
	switch c.Scope {
	case ScopeSameDomain, ScopeIncludeSubdomain, ScopeFollowExternals:
	default:
		return fmt.Errorf("invalid scope: %q", c.Scope)
	}

	switch c.Extraction {
	case ExtractionMinimal, ExtractionStandard, ExtractionFull:
	default:
		return fmt.Errorf("invalid extraction level: %q", c.Extraction)
	}

	switch c.AntiBotMode {
	case AntiBotSkip, AntiBotRotate, AntiBotSolve, "":
	default:
		return fmt.Errorf("invalid antibot mode: %q", c.AntiBotMode)
	}

	if c.MaxDepth < 0 {
		return fmt.Errorf("max_depth must be >= 0, got %d", c.MaxDepth)
	}

	return nil
}
