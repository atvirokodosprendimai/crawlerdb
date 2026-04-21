package entities_test

import (
	"testing"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
	"github.com/stretchr/testify/assert"
)

func TestNewPage(t *testing.T) {
	p := entities.NewPage("url1", "job1")
	assert.NotEmpty(t, p.ID)
	assert.Equal(t, "url1", p.URLID)
	assert.Equal(t, "job1", p.JobID)
	assert.False(t, p.CreatedAt.IsZero())
}

func TestCrawlResult(t *testing.T) {
	url := entities.NewCrawlURL("job1", "https://example.com", "https://example.com", "hash", 0, "")
	page := entities.NewPage(url.ID, "job1")

	result := entities.CrawlResult{
		URL:     url,
		Page:    page,
		Success: true,
		DiscoveredURLs: []entities.DiscoveredLink{
			{RawURL: "https://example.com/about", Normalized: "https://example.com/about", URLHash: "h1", IsExternal: false},
			{RawURL: "https://other.com", Normalized: "https://other.com", URLHash: "h2", IsExternal: true},
		},
	}

	assert.True(t, result.Success)
	assert.Len(t, result.DiscoveredURLs, 2)
	assert.False(t, result.DiscoveredURLs[0].IsExternal)
	assert.True(t, result.DiscoveredURLs[1].IsExternal)
}
