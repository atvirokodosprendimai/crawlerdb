package fetcher_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	fetcher "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetch_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "TestBot/1.0", r.Header.Get("User-Agent"))
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body>hello</body></html>"))
	}))
	defer server.Close()

	f := fetcher.New(fetcher.WithUserAgent("TestBot/1.0"))
	resp, err := f.Fetch(context.Background(), server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
	assert.Contains(t, resp.ContentType, "text/html")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "hello")
}

func TestFetch_404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	f := fetcher.New()
	resp, err := f.Fetch(context.Background(), server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 404, resp.StatusCode)
}

func TestFetch_Redirect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/final", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("final page"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	f := fetcher.New()
	resp, err := f.Fetch(context.Background(), server.URL+"/redirect")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
	assert.Contains(t, resp.URL, "/final")
}

func TestFetch_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer server.Close()

	f := fetcher.New(fetcher.WithTimeout(100 * time.Millisecond))
	_, err := f.Fetch(context.Background(), server.URL)
	assert.Error(t, err)
}

func TestFetch_InvalidURL(t *testing.T) {
	f := fetcher.New()
	_, err := f.Fetch(context.Background(), "http://[::1]:namedport/")
	assert.Error(t, err)
}
