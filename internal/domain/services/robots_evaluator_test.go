package services_test

import (
	"testing"
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/services"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/valueobj"
	"github.com/stretchr/testify/assert"
)

func TestRobotsEvaluator_IsAllowed(t *testing.T) {
	eval := services.NewRobotsEvaluator()

	policy := &valueobj.RobotsPolicy{
		Domain: "example.com",
		Rules: []valueobj.RobotsRule{
			{Path: "/private/", Allow: false},
			{Path: "/private/public", Allow: true},
			{Path: "/admin", Allow: false},
			{Path: "/", Allow: true},
		},
	}

	tests := []struct {
		name    string
		path    string
		allowed bool
	}{
		{"root allowed", "/", true},
		{"public page", "/about", true},
		{"private blocked", "/private/secret", false},
		{"private public allowed", "/private/public", true},
		{"admin blocked", "/admin", false},
		{"admin subpath blocked", "/admin/dashboard", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := eval.IsAllowed(policy, "https://example.com"+tt.path, "CrawlerDB/1.0")
			assert.Equal(t, tt.allowed, result)
		})
	}
}

func TestRobotsEvaluator_NilPolicy(t *testing.T) {
	eval := services.NewRobotsEvaluator()
	// Nil policy = no robots.txt = allow everything.
	assert.True(t, eval.IsAllowed(nil, "https://example.com/anything", "CrawlerDB/1.0"))
}

func TestRobotsEvaluator_EmptyRules(t *testing.T) {
	eval := services.NewRobotsEvaluator()
	policy := &valueobj.RobotsPolicy{Domain: "example.com"}
	assert.True(t, eval.IsAllowed(policy, "https://example.com/anything", "CrawlerDB/1.0"))
}

func TestRobotsEvaluator_GetCrawlDelay(t *testing.T) {
	eval := services.NewRobotsEvaluator()
	policy := &valueobj.RobotsPolicy{
		Domain:     "example.com",
		CrawlDelay: 5 * time.Second,
	}
	assert.Equal(t, 5*time.Second, eval.GetCrawlDelay(policy))
}

func TestRobotsEvaluator_GetCrawlDelay_Nil(t *testing.T) {
	eval := services.NewRobotsEvaluator()
	assert.Equal(t, time.Duration(0), eval.GetCrawlDelay(nil))
}

func TestRobotsEvaluator_LongestMatch(t *testing.T) {
	eval := services.NewRobotsEvaluator()

	// More specific path should win.
	policy := &valueobj.RobotsPolicy{
		Domain: "example.com",
		Rules: []valueobj.RobotsRule{
			{Path: "/", Allow: false},        // block everything
			{Path: "/open", Allow: true},      // but allow /open
		},
	}

	assert.False(t, eval.IsAllowed(policy, "https://example.com/secret", "Bot"))
	assert.True(t, eval.IsAllowed(policy, "https://example.com/open", "Bot"))
	assert.True(t, eval.IsAllowed(policy, "https://example.com/open/page", "Bot"))
}
