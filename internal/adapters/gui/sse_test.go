package gui_test

import (
	"bufio"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/gui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSSEBroker_ConnectAndBroadcast(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	broker := gui.NewSSEBroker(logger)

	srv := httptest.NewServer(broker)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", srv.URL, nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	// Wait for client to register.
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, broker.ClientCount())

	// Broadcast event.
	broker.Broadcast("job_update", map[string]string{"id": "j1", "status": "running"})

	// Read SSE data.
	scanner := bufio.NewScanner(resp.Body)
	var foundData bool
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			assert.Contains(t, line, "job_update")
			assert.Contains(t, line, "j1")
			foundData = true
			break
		}
	}
	assert.True(t, foundData)
}

func TestSSEBroker_ClientCount(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	broker := gui.NewSSEBroker(logger)
	assert.Equal(t, 0, broker.ClientCount())
}
