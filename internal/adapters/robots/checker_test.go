package robots_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/robots"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockFetcher implements ports.Fetcher for testing.
type mockFetcher struct {
	responses map[string]*ports.FetchResponse
	errors    map[string]error
}

func (m *mockFetcher) Fetch(_ context.Context, url string) (*ports.FetchResponse, error) {
	if err, ok := m.errors[url]; ok {
		return nil, err
	}
	if resp, ok := m.responses[url]; ok {
		return resp, nil
	}
	return &ports.FetchResponse{
		StatusCode: 404,
		Body:       io.NopCloser(strings.NewReader("")),
	}, nil
}

func TestChecker_IsAllowed(t *testing.T) {
	robotsTxt := `
User-agent: *
Disallow: /private/
Allow: /private/public
`
	fetcher := &mockFetcher{
		responses: map[string]*ports.FetchResponse{
			"https://example.com/robots.txt": {
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(robotsTxt)),
				Headers:    http.Header{},
			},
		},
	}

	checker := robots.NewChecker(fetcher, "CrawlerDB/1.0", time.Hour)
	ctx := context.Background()

	allowed, err := checker.IsAllowed(ctx, "https://example.com/about", "CrawlerDB/1.0")
	require.NoError(t, err)
	assert.True(t, allowed)

	allowed, err = checker.IsAllowed(ctx, "https://example.com/private/secret", "CrawlerDB/1.0")
	require.NoError(t, err)
	assert.False(t, allowed)

	allowed, err = checker.IsAllowed(ctx, "https://example.com/private/public", "CrawlerDB/1.0")
	require.NoError(t, err)
	assert.True(t, allowed)
}

func TestChecker_NoRobotsTxt(t *testing.T) {
	fetcher := &mockFetcher{
		errors: map[string]error{
			"https://norobots.com/robots.txt": assert.AnError,
			"http://norobots.com/robots.txt":  assert.AnError,
		},
	}

	checker := robots.NewChecker(fetcher, "CrawlerDB/1.0", time.Hour)
	ctx := context.Background()

	allowed, err := checker.IsAllowed(ctx, "https://norobots.com/anything", "CrawlerDB/1.0")
	require.NoError(t, err)
	assert.True(t, allowed, "no robots.txt should allow everything")
}

func TestChecker_CachesPolicy(t *testing.T) {
	callCount := 0
	robotsTxt := "User-agent: *\nDisallow: /blocked\n"
	fetcher := &mockFetcher{
		responses: map[string]*ports.FetchResponse{
			"https://cached.com/robots.txt": {
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(robotsTxt)),
				Headers:    http.Header{},
			},
		},
	}
	// Wrap to count calls.
	origFetch := fetcher.Fetch
	wrappedFetcher := &countingFetcher{inner: fetcher, count: &callCount, origFetch: origFetch}

	checker := robots.NewChecker(wrappedFetcher, "CrawlerDB/1.0", time.Hour)
	ctx := context.Background()

	// First call fetches.
	_, _ = checker.IsAllowed(ctx, "https://cached.com/page1", "CrawlerDB/1.0")
	// Second call should use cache.
	_, _ = checker.IsAllowed(ctx, "https://cached.com/page2", "CrawlerDB/1.0")

	assert.Equal(t, 1, callCount, "robots.txt should be cached")
}

type countingFetcher struct {
	inner     *mockFetcher
	count     *int
	origFetch func(context.Context, string) (*ports.FetchResponse, error)
}

func (f *countingFetcher) Fetch(ctx context.Context, url string) (*ports.FetchResponse, error) {
	*f.count++
	// Re-create the body since it may have been consumed.
	if resp, ok := f.inner.responses[url]; ok {
		robotsTxt := "User-agent: *\nDisallow: /blocked\n"
		return &ports.FetchResponse{
			StatusCode: resp.StatusCode,
			Body:       io.NopCloser(strings.NewReader(robotsTxt)),
			Headers:    resp.Headers,
		}, nil
	}
	return f.inner.Fetch(ctx, url)
}
