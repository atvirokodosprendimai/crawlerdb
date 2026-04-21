package fetcher_test

import (
	"testing"

	fetcher "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProxyPool_RoundRobin(t *testing.T) {
	pool, err := fetcher.NewProxyPool(
		[]string{"http://p1:8080", "http://p2:8080", "http://p3:8080"},
		fetcher.RotationRoundRobin,
	)
	require.NoError(t, err)

	got := make([]string, 6)
	for i := range got {
		got[i] = pool.Next().String()
	}

	assert.Equal(t, "http://p1:8080", got[0])
	assert.Equal(t, "http://p2:8080", got[1])
	assert.Equal(t, "http://p3:8080", got[2])
	assert.Equal(t, "http://p1:8080", got[3]) // wraps around
}

func TestProxyPool_Random(t *testing.T) {
	pool, err := fetcher.NewProxyPool(
		[]string{"http://p1:8080", "http://p2:8080"},
		fetcher.RotationRandom,
	)
	require.NoError(t, err)

	// Just verify it returns non-nil proxies.
	for range 10 {
		assert.NotNil(t, pool.Next())
	}
}

func TestProxyPool_LeastUsed(t *testing.T) {
	pool, err := fetcher.NewProxyPool(
		[]string{"http://p1:8080", "http://p2:8080"},
		fetcher.RotationLeastUsed,
	)
	require.NoError(t, err)

	// First call picks p1 (both at 0, picks first).
	first := pool.Next().String()
	assert.Equal(t, "http://p1:8080", first)

	// Second call picks p2 (p1 used=1, p2 used=0).
	second := pool.Next().String()
	assert.Equal(t, "http://p2:8080", second)
}

func TestProxyPool_MarkDead(t *testing.T) {
	pool, err := fetcher.NewProxyPool(
		[]string{"http://p1:8080", "http://p2:8080"},
		fetcher.RotationRoundRobin,
	)
	require.NoError(t, err)

	pool.MarkDead("http://p1:8080")
	assert.Equal(t, 1, pool.AliveCount())

	// All calls should return p2.
	for range 3 {
		assert.Equal(t, "http://p2:8080", pool.Next().String())
	}
}

func TestProxyPool_MarkAlive(t *testing.T) {
	pool, err := fetcher.NewProxyPool(
		[]string{"http://p1:8080", "http://p2:8080"},
		fetcher.RotationRoundRobin,
	)
	require.NoError(t, err)

	pool.MarkDead("http://p1:8080")
	assert.Equal(t, 1, pool.AliveCount())

	pool.MarkAlive("http://p1:8080")
	assert.Equal(t, 2, pool.AliveCount())
}

func TestProxyPool_AllDead(t *testing.T) {
	pool, err := fetcher.NewProxyPool(
		[]string{"http://p1:8080"},
		fetcher.RotationRoundRobin,
	)
	require.NoError(t, err)

	pool.MarkDead("http://p1:8080")
	assert.Nil(t, pool.Next())
}

func TestProxyPool_EmptyList(t *testing.T) {
	_, err := fetcher.NewProxyPool(nil, fetcher.RotationRoundRobin)
	assert.Error(t, err)
}

func TestProxyPool_Transport(t *testing.T) {
	pool, err := fetcher.NewProxyPool(
		[]string{"http://p1:8080"},
		fetcher.RotationRoundRobin,
	)
	require.NoError(t, err)

	tr := pool.Transport()
	assert.NotNil(t, tr)
}
