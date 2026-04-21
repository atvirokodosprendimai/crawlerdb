package fetcher

import (
	"context"
	"sync"
	"time"
)

// AdaptiveRateLimiter implements ports.RateLimiter with per-domain adaptive throttling.
type AdaptiveRateLimiter struct {
	mu       sync.Mutex
	domains  map[string]*domainLimiter
	defaults time.Duration
}

type domainLimiter struct {
	interval time.Duration
	lastReq  time.Time
}

// NewAdaptiveRateLimiter creates a new rate limiter with the given default interval.
func NewAdaptiveRateLimiter(defaultInterval time.Duration) *AdaptiveRateLimiter {
	return &AdaptiveRateLimiter{
		domains:  make(map[string]*domainLimiter),
		defaults: defaultInterval,
	}
}

func (r *AdaptiveRateLimiter) getOrCreate(domain string) *domainLimiter {
	r.mu.Lock()
	defer r.mu.Unlock()
	if dl, ok := r.domains[domain]; ok {
		return dl
	}
	dl := &domainLimiter{interval: r.defaults}
	r.domains[domain] = dl
	return dl
}

// Wait blocks until the domain's rate limit permits a request.
func (r *AdaptiveRateLimiter) Wait(ctx context.Context, domain string) error {
	dl := r.getOrCreate(domain)

	r.mu.Lock()
	elapsed := time.Since(dl.lastReq)
	if elapsed < dl.interval {
		wait := dl.interval - elapsed
		r.mu.Unlock()

		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}

		r.mu.Lock()
	}
	dl.lastReq = time.Now()
	r.mu.Unlock()
	return nil
}

// RecordResponse adjusts rate limiting based on server response signals.
func (r *AdaptiveRateLimiter) RecordResponse(domain string, statusCode int, latency time.Duration) {
	dl := r.getOrCreate(domain)
	r.mu.Lock()
	defer r.mu.Unlock()

	switch {
	case statusCode == 429:
		// Rate limited — double interval, max 30s.
		dl.interval = min(dl.interval*2, 30*time.Second)
	case statusCode >= 500:
		// Server error — increase slightly.
		dl.interval = min(dl.interval*3/2, 30*time.Second)
	case latency > 5*time.Second:
		// Slow response — increase slightly.
		dl.interval = min(dl.interval*3/2, 30*time.Second)
	case statusCode == 200 && latency < time.Second && dl.interval > r.defaults:
		// Fast response — decrease toward default.
		dl.interval = max(dl.interval*2/3, r.defaults)
	}
}

// SetCrawlDelay overrides the interval for a domain (from robots.txt).
func (r *AdaptiveRateLimiter) SetCrawlDelay(domain string, delay time.Duration) {
	dl := r.getOrCreate(domain)
	r.mu.Lock()
	defer r.mu.Unlock()
	if delay > dl.interval {
		dl.interval = delay
	}
}
