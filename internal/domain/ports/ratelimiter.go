package ports

import (
	"context"
	"time"
)

// RateLimiter controls per-domain request rates.
type RateLimiter interface {
	// Wait blocks until the domain's rate limit allows a request.
	Wait(ctx context.Context, domain string) error

	// RecordResponse updates rate limiting based on server response signals.
	RecordResponse(domain string, statusCode int, latency time.Duration)

	// SetCrawlDelay overrides the rate for a domain based on robots.txt.
	SetCrawlDelay(domain string, delay time.Duration)
}
