package robots

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"sync"
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/services"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/valueobj"
)

// Checker implements ports.RobotsChecker.
type Checker struct {
	fetcher   ports.Fetcher
	evaluator *services.RobotsEvaluator
	userAgent string
	ttl       time.Duration

	mu    sync.RWMutex
	cache map[string]*valueobj.RobotsPolicy
}

// NewChecker creates a new robots.txt checker.
func NewChecker(fetcher ports.Fetcher, userAgent string, ttl time.Duration) *Checker {
	return &Checker{
		fetcher:   fetcher,
		evaluator: services.NewRobotsEvaluator(),
		userAgent: userAgent,
		ttl:       ttl,
		cache:     make(map[string]*valueobj.RobotsPolicy),
	}
}

func (c *Checker) IsAllowed(ctx context.Context, rawURL, userAgent string) (bool, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false, fmt.Errorf("parse URL: %w", err)
	}
	domain := parsed.Hostname()

	policy, err := c.GetPolicy(ctx, domain)
	if err != nil {
		// If we can't fetch robots.txt, allow by default.
		return true, nil
	}

	return c.evaluator.IsAllowed(policy, rawURL, userAgent), nil
}

func (c *Checker) GetPolicy(ctx context.Context, domain string) (*valueobj.RobotsPolicy, error) {
	// Check cache first.
	c.mu.RLock()
	if policy, ok := c.cache[domain]; ok {
		if time.Now().Before(policy.ExpiresAt) {
			c.mu.RUnlock()
			return policy, nil
		}
	}
	c.mu.RUnlock()

	// Fetch robots.txt.
	robotsURL := fmt.Sprintf("https://%s/robots.txt", domain)
	resp, err := c.fetcher.Fetch(ctx, robotsURL)
	if err != nil {
		// Try HTTP as fallback.
		robotsURL = fmt.Sprintf("http://%s/robots.txt", domain)
		resp, err = c.fetcher.Fetch(ctx, robotsURL)
		if err != nil {
			// No robots.txt found — allow everything.
			policy := &valueobj.RobotsPolicy{
				Domain:    domain,
				FetchedAt: time.Now().UTC(),
				ExpiresAt: time.Now().UTC().Add(c.ttl),
			}
			c.mu.Lock()
			c.cache[domain] = policy
			c.mu.Unlock()
			return policy, nil
		}
	}
	defer resp.Body.Close()

	// If not 200, treat as no robots.txt.
	if resp.StatusCode != 200 {
		policy := &valueobj.RobotsPolicy{
			Domain:    domain,
			FetchedAt: time.Now().UTC(),
			ExpiresAt: time.Now().UTC().Add(c.ttl),
		}
		c.mu.Lock()
		c.cache[domain] = policy
		c.mu.Unlock()
		return policy, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read robots.txt: %w", err)
	}

	policy := Parse(domain, string(body), c.userAgent, c.ttl)

	c.mu.Lock()
	c.cache[domain] = policy
	c.mu.Unlock()

	return policy, nil
}
