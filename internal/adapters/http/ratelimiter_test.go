package fetcher_test

import (
	"context"
	"testing"
	"time"

	fetcher "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/http"
	"github.com/stretchr/testify/assert"
)

func TestRateLimiter_Wait(t *testing.T) {
	rl := fetcher.NewAdaptiveRateLimiter(100 * time.Millisecond)
	ctx := context.Background()

	start := time.Now()
	assert.NoError(t, rl.Wait(ctx, "example.com"))
	assert.NoError(t, rl.Wait(ctx, "example.com"))
	elapsed := time.Since(start)

	assert.GreaterOrEqual(t, elapsed, 90*time.Millisecond, "second request should wait")
}

func TestRateLimiter_DifferentDomains(t *testing.T) {
	rl := fetcher.NewAdaptiveRateLimiter(200 * time.Millisecond)
	ctx := context.Background()

	start := time.Now()
	assert.NoError(t, rl.Wait(ctx, "a.com"))
	assert.NoError(t, rl.Wait(ctx, "b.com")) // different domain, no wait
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 100*time.Millisecond, "different domains should not wait")
}

func TestRateLimiter_Adaptive429(t *testing.T) {
	rl := fetcher.NewAdaptiveRateLimiter(100 * time.Millisecond)

	rl.RecordResponse("slow.com", 429, 0)

	// After 429, interval should double.
	ctx := context.Background()
	start := time.Now()
	assert.NoError(t, rl.Wait(ctx, "slow.com"))
	assert.NoError(t, rl.Wait(ctx, "slow.com"))
	elapsed := time.Since(start)

	assert.GreaterOrEqual(t, elapsed, 180*time.Millisecond, "should wait longer after 429")
}

func TestRateLimiter_SetCrawlDelay(t *testing.T) {
	rl := fetcher.NewAdaptiveRateLimiter(100 * time.Millisecond)
	rl.SetCrawlDelay("polite.com", 300*time.Millisecond)

	ctx := context.Background()
	start := time.Now()
	assert.NoError(t, rl.Wait(ctx, "polite.com"))
	assert.NoError(t, rl.Wait(ctx, "polite.com"))
	elapsed := time.Since(start)

	assert.GreaterOrEqual(t, elapsed, 280*time.Millisecond, "should respect crawl-delay")
}

func TestRateLimiter_ContextCancel(t *testing.T) {
	rl := fetcher.NewAdaptiveRateLimiter(5 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	assert.NoError(t, rl.Wait(ctx, "cancel.com"))     // first is immediate
	assert.Error(t, rl.Wait(ctx, "cancel.com"))        // second should timeout
}
