package ports

import (
	"context"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/valueobj"
)

// DetectionResult holds the outcome of anti-bot analysis.
type DetectionResult struct {
	Detected  bool   `json:"detected"`
	EventType string `json:"event_type"` // captcha, challenge, block, rate_limit
	Provider  string `json:"provider"`   // recaptcha, hcaptcha, cloudflare, etc.
	Details   string `json:"details"`
}

// AntiBotDetector analyzes HTTP responses for bot detection signals.
type AntiBotDetector interface {
	Analyze(response *FetchResponse, body []byte) DetectionResult
}

// CaptchaSolver solves detected captcha challenges.
type CaptchaSolver interface {
	Solve(ctx context.Context, event valueobj.AntiBotEvent) (string, error)
}
