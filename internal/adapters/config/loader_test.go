package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefault(t *testing.T) {
	cfg := config.LoadDefault()
	assert.Equal(t, "nats://localhost:4222", cfg.NATS.URL)
	assert.Equal(t, "crawlerdb.sqlite", cfg.Database.Path)
	assert.Equal(t, 10, cfg.Crawler.PoolSize)
	assert.Equal(t, "data", cfg.Crawler.ContentDir)
	assert.Equal(t, ":8081", cfg.Server.Addr)
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
[nats]
url = "nats://custom:4222"

[database]
path = "/tmp/test.sqlite"

[crawler]
user_agent = "TestBot/1.0"
max_depth = 5
pool_size = 20
request_timeout = "15s"
default_rate_limit = "500ms"
max_retries = 5
robots_ttl = "12h"

[server]
addr = ":9090"
`
	err := os.WriteFile(path, []byte(content), 0o644)
	require.NoError(t, err)

	cfg, err := config.LoadFromFile(path)
	require.NoError(t, err)

	assert.Equal(t, "nats://custom:4222", cfg.NATS.URL)
	assert.Equal(t, "/tmp/test.sqlite", cfg.Database.Path)
	assert.Equal(t, "TestBot/1.0", cfg.Crawler.UserAgent)
	assert.Equal(t, 5, cfg.Crawler.MaxDepth)
	assert.Equal(t, 20, cfg.Crawler.PoolSize)
	assert.Equal(t, ":9090", cfg.Server.Addr)
}

func TestLoadFromFile_NotFound(t *testing.T) {
	_, err := config.LoadFromFile("/nonexistent/path.toml")
	assert.Error(t, err)
}

func TestGenerateDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "default.toml")

	err := config.GenerateDefault(path)
	require.NoError(t, err)

	cfg, err := config.LoadFromFile(path)
	require.NoError(t, err)

	assert.Equal(t, "nats://localhost:4222", cfg.NATS.URL)
	assert.Equal(t, 10, cfg.Crawler.PoolSize)
}
