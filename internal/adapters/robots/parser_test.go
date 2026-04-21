package robots_test

import (
	"testing"
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/robots"
	"github.com/stretchr/testify/assert"
)

func TestParse_BasicRules(t *testing.T) {
	raw := `
User-agent: *
Disallow: /private/
Disallow: /admin
Allow: /admin/public
Crawl-delay: 2

Sitemap: https://example.com/sitemap.xml
`
	policy := robots.Parse("example.com", raw, "CrawlerDB/1.0", 24*time.Hour)

	assert.Equal(t, "example.com", policy.Domain)
	assert.Len(t, policy.Rules, 3)
	assert.Equal(t, 2*time.Second, policy.CrawlDelay)
	assert.Equal(t, []string{"https://example.com/sitemap.xml"}, policy.Sitemaps)
}

func TestParse_AgentSpecific(t *testing.T) {
	raw := `
User-agent: Googlebot
Disallow: /google-only/

User-agent: *
Disallow: /secret/
`
	policy := robots.Parse("example.com", raw, "CrawlerDB/1.0", 24*time.Hour)

	// Should only get wildcard rules since CrawlerDB doesn't match Googlebot.
	assert.Len(t, policy.Rules, 1)
	assert.Equal(t, "/secret/", policy.Rules[0].Path)
}

func TestParse_MatchingAgent(t *testing.T) {
	raw := `
User-agent: crawlerdb
Disallow: /specific/

User-agent: *
Disallow: /general/
`
	policy := robots.Parse("example.com", raw, "CrawlerDB/1.0", 24*time.Hour)

	// Should get crawlerdb-specific rules.
	assert.Len(t, policy.Rules, 1)
	assert.Equal(t, "/specific/", policy.Rules[0].Path)
}

func TestParse_Empty(t *testing.T) {
	policy := robots.Parse("example.com", "", "CrawlerDB/1.0", 24*time.Hour)
	assert.Empty(t, policy.Rules)
	assert.Equal(t, time.Duration(0), policy.CrawlDelay)
}

func TestParse_MultipleSitemaps(t *testing.T) {
	raw := `
User-agent: *
Disallow:

Sitemap: https://example.com/sitemap1.xml
Sitemap: https://example.com/sitemap2.xml
`
	policy := robots.Parse("example.com", raw, "CrawlerDB/1.0", 24*time.Hour)
	assert.Len(t, policy.Sitemaps, 2)
}

func TestParse_Comments(t *testing.T) {
	raw := `
# This is a comment
User-agent: *  # all bots
Disallow: /private/  # keep out
`
	policy := robots.Parse("example.com", raw, "CrawlerDB/1.0", 24*time.Hour)
	assert.Len(t, policy.Rules, 1)
	assert.Equal(t, "/private/", policy.Rules[0].Path)
}
