package robots

import (
	"bufio"
	"strings"
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/valueobj"
)

// Parse parses raw robots.txt content into a RobotsPolicy.
// It extracts rules for the given userAgent (or * as fallback).
func Parse(domain, raw, userAgent string, ttl time.Duration) *valueobj.RobotsPolicy {
	now := time.Now().UTC()
	policy := &valueobj.RobotsPolicy{
		Domain:    domain,
		FetchedAt: now,
		ExpiresAt: now.Add(ttl),
	}

	scanner := bufio.NewScanner(strings.NewReader(raw))
	var (
		currentAgents []string
		specificRules []valueobj.RobotsRule
		wildcardRules []valueobj.RobotsRule
		specificDelay time.Duration
		wildcardDelay time.Duration
		matchedAgent  bool
	)

	ua := strings.ToLower(userAgent)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Strip comments.
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(strings.ToLower(parts[0]))
		value := strings.TrimSpace(parts[1])

		switch key {
		case "user-agent":
			if len(currentAgents) > 0 && (len(specificRules) > 0 || len(wildcardRules) > 0) {
				// New user-agent block; reset.
				currentAgents = nil
			}
			currentAgents = append(currentAgents, strings.ToLower(value))

		case "disallow":
			if value == "" {
				continue
			}
			rule := valueobj.RobotsRule{Path: value, Allow: false}
			if agentMatches(currentAgents, ua) {
				specificRules = append(specificRules, rule)
				matchedAgent = true
			} else if agentMatchesWildcard(currentAgents) {
				wildcardRules = append(wildcardRules, rule)
			}

		case "allow":
			if value == "" {
				continue
			}
			rule := valueobj.RobotsRule{Path: value, Allow: true}
			if agentMatches(currentAgents, ua) {
				specificRules = append(specificRules, rule)
				matchedAgent = true
			} else if agentMatchesWildcard(currentAgents) {
				wildcardRules = append(wildcardRules, rule)
			}

		case "crawl-delay":
			d, err := time.ParseDuration(value + "s")
			if err != nil {
				continue
			}
			if agentMatches(currentAgents, ua) {
				specificDelay = d
			} else if agentMatchesWildcard(currentAgents) {
				wildcardDelay = d
			}

		case "sitemap":
			policy.Sitemaps = append(policy.Sitemaps, value)
		}
	}

	// Prefer agent-specific rules; fall back to wildcard.
	if matchedAgent {
		policy.Rules = specificRules
		policy.CrawlDelay = specificDelay
	} else {
		policy.Rules = wildcardRules
		policy.CrawlDelay = wildcardDelay
	}

	return policy
}

func agentMatches(agents []string, ua string) bool {
	for _, a := range agents {
		if a != "*" && strings.Contains(ua, a) {
			return true
		}
	}
	return false
}

func agentMatchesWildcard(agents []string) bool {
	for _, a := range agents {
		if a == "*" {
			return true
		}
	}
	return false
}
