package uid_test

import (
	"sync"
	"testing"

	"github.com/atvirokodosprendimai/crawlerdb/pkg/uid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewID(t *testing.T) {
	id := uid.NewID()
	assert.Len(t, id, 26) // ULID is 26 chars
	assert.NotEmpty(t, id)
}

func TestNewID_Unique(t *testing.T) {
	ids := make(map[string]struct{}, 1000)
	for range 1000 {
		id := uid.NewID()
		_, exists := ids[id]
		require.False(t, exists, "duplicate ID: %s", id)
		ids[id] = struct{}{}
	}
}

func TestNewID_ThreadSafe(t *testing.T) {
	var wg sync.WaitGroup
	ids := make(chan string, 100)

	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids <- uid.NewID()
		}()
	}

	wg.Wait()
	close(ids)

	seen := make(map[string]struct{})
	for id := range ids {
		assert.Len(t, id, 26)
		_, exists := seen[id]
		assert.False(t, exists)
		seen[id] = struct{}{}
	}
}
