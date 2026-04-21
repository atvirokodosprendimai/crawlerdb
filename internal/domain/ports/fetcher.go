package ports

import (
	"context"
	"io"
	"net/http"
)

// FetchResponse wraps an HTTP response for the domain layer.
type FetchResponse struct {
	StatusCode  int
	Headers     http.Header
	Body        io.ReadCloser
	ContentType string
	URL         string // Final URL after redirects
}

// Fetcher defines the interface for fetching web pages.
type Fetcher interface {
	Fetch(ctx context.Context, url string) (*FetchResponse, error)
}
