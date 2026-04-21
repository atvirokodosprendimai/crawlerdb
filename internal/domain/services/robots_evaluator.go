package services

import (
	"net/url"
	"strings"
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/valueobj"
)

// RobotsEvaluator evaluates URLs against robots.txt policies.
type RobotsEvaluator struct{}

// NewRobotsEvaluator creates a new evaluator.
func NewRobotsEvaluator() *RobotsEvaluator {
	return &RobotsEvaluator{}
}

// IsAllowed checks whether a URL is permitted by the given policy.
// Uses longest-match precedence: the most specific matching rule wins.
// A nil policy means no robots.txt was found, which allows all URLs.
func (e *RobotsEvaluator) IsAllowed(policy *valueobj.RobotsPolicy, rawURL, userAgent string) bool {
	if policy == nil || len(policy.Rules) == 0 {
		return true
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	path := parsed.Path
	if path == "" {
		path = "/"
	}

	// Find the longest matching rule (most specific).
	bestLen := -1
	allowed := true

	for _, rule := range policy.Rules {
		if strings.HasPrefix(path, rule.Path) && len(rule.Path) > bestLen {
			bestLen = len(rule.Path)
			allowed = rule.Allow
		}
	}

	return allowed
}

// GetCrawlDelay returns the crawl delay from the policy, or zero if none.
func (e *RobotsEvaluator) GetCrawlDelay(policy *valueobj.RobotsPolicy) time.Duration {
	if policy == nil {
		return 0
	}
	return policy.CrawlDelay
}
