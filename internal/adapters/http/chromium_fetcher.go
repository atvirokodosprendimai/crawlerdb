package fetcher

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
)

type ChromiumFetcher struct {
	client    *http.Client
	endpoint  string
	userAgent string
}

func NewChromium(endpoint, userAgent string, timeout time.Duration) *ChromiumFetcher {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &ChromiumFetcher{
		client:    &http.Client{Timeout: timeout},
		endpoint:  strings.TrimRight(endpoint, "/"),
		userAgent: userAgent,
	}
}

func (f *ChromiumFetcher) Fetch(ctx context.Context, rawURL string) (*ports.FetchResponse, error) {
	if strings.TrimSpace(f.endpoint) == "" {
		return nil, fmt.Errorf("chromium endpoint is empty")
	}
	payload := map[string]any{"url": rawURL}
	if strings.TrimSpace(f.userAgent) != "" {
		payload["userAgent"] = f.userAgent
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal chromium payload: %w", err)
	}
	endpoint := f.endpoint + "/content"
	if _, err := url.Parse(endpoint); err != nil {
		return nil, fmt.Errorf("invalid chromium endpoint: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create chromium request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chromium fetch %s: %w", rawURL, err)
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("chromium fetch %s: status %d: %s", rawURL, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "text/html; charset=utf-8"
	}
	return &ports.FetchResponse{
		StatusCode:  resp.StatusCode,
		Headers:     resp.Header,
		Body:        resp.Body,
		ContentType: contentType,
		URL:         rawURL,
	}, nil
}
