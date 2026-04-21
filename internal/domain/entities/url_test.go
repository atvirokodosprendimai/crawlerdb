package entities_test

import (
	"testing"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCrawlURL(t *testing.T) {
	u := entities.NewCrawlURL("job1", "https://example.com/page", "https://example.com/page", "abc123", 0, "")
	assert.NotEmpty(t, u.ID)
	assert.Equal(t, "job1", u.JobID)
	assert.Equal(t, entities.URLStatusPending, u.Status)
	assert.Equal(t, 0, u.Depth)
}

func TestCrawlURL_Claim(t *testing.T) {
	u := entities.NewCrawlURL("job1", "https://example.com", "https://example.com", "abc", 0, "")
	err := u.Claim()
	require.NoError(t, err)
	assert.Equal(t, entities.URLStatusCrawling, u.Status)
}

func TestCrawlURL_Claim_OnlyFromPending(t *testing.T) {
	u := entities.NewCrawlURL("job1", "https://example.com", "https://example.com", "abc", 0, "")
	require.NoError(t, u.Claim())
	err := u.Claim()
	assert.ErrorIs(t, err, entities.ErrInvalidTransition)
}

func TestCrawlURL_MarkDone(t *testing.T) {
	u := entities.NewCrawlURL("job1", "https://example.com", "https://example.com", "abc", 0, "")
	require.NoError(t, u.Claim())
	err := u.MarkDone()
	require.NoError(t, err)
	assert.Equal(t, entities.URLStatusDone, u.Status)
}

func TestCrawlURL_MarkBlocked(t *testing.T) {
	u := entities.NewCrawlURL("job1", "https://example.com", "https://example.com", "abc", 0, "")
	require.NoError(t, u.Claim())
	err := u.MarkBlocked()
	require.NoError(t, err)
	assert.Equal(t, entities.URLStatusBlocked, u.Status)
}

func TestCrawlURL_MarkError(t *testing.T) {
	u := entities.NewCrawlURL("job1", "https://example.com", "https://example.com", "abc", 0, "")
	require.NoError(t, u.Claim())
	err := u.MarkError()
	require.NoError(t, err)
	assert.Equal(t, entities.URLStatusError, u.Status)
}

func TestCrawlURL_Retry(t *testing.T) {
	u := entities.NewCrawlURL("job1", "https://example.com", "https://example.com", "abc", 0, "")
	require.NoError(t, u.Claim())
	require.NoError(t, u.MarkError())

	err := u.Retry()
	require.NoError(t, err)
	assert.Equal(t, entities.URLStatusPending, u.Status)
	assert.Equal(t, 1, u.RetryCount)
}
