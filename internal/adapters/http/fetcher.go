package fetcher

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
)

// HTTPFetcher implements ports.Fetcher using net/http.
type HTTPFetcher struct {
	client    *http.Client
	userAgent string
}

// Option configures the HTTPFetcher.
type Option func(*HTTPFetcher)

// WithUserAgent sets the User-Agent header.
func WithUserAgent(ua string) Option {
	return func(f *HTTPFetcher) { f.userAgent = ua }
}

// WithTimeout sets the HTTP client timeout.
func WithTimeout(d time.Duration) Option {
	return func(f *HTTPFetcher) { f.client.Timeout = d }
}

// WithTransport sets a custom http.RoundTripper (e.g., for proxies).
func WithTransport(t http.RoundTripper) Option {
	return func(f *HTTPFetcher) { f.client.Transport = t }
}

// New creates a new HTTPFetcher.
func New(opts ...Option) *HTTPFetcher {
	f := &HTTPFetcher{
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects (10)")
				}
				return nil
			},
		},
		userAgent: "CrawlerDB/1.0",
	}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// Fetch retrieves a URL and returns the response.
func (f *HTTPFetcher) Fetch(ctx context.Context, url string) (*ports.FetchResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}

	return &ports.FetchResponse{
		StatusCode:  resp.StatusCode,
		Headers:     resp.Header,
		Body:        resp.Body,
		ContentType: resp.Header.Get("Content-Type"),
		URL:         resp.Request.URL.String(),
	}, nil
}
