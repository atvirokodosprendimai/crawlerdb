package worker_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadOrCreate_NewIdentity(t *testing.T) {
	dir := t.TempDir()
	id, err := worker.LoadOrCreate(dir)
	require.NoError(t, err)
	assert.NotEmpty(t, id.ID())

	// File exists.
	data, err := os.ReadFile(filepath.Join(dir, "worker.id"))
	require.NoError(t, err)
	assert.Contains(t, string(data), id.ID())
}

func TestLoadOrCreate_ExistingIdentity(t *testing.T) {
	dir := t.TempDir()

	// Create first.
	id1, err := worker.LoadOrCreate(dir)
	require.NoError(t, err)

	// Load again — same ID.
	id2, err := worker.LoadOrCreate(dir)
	require.NoError(t, err)
	assert.Equal(t, id1.ID(), id2.ID())
}

func TestLoadOrCreate_CorruptFile(t *testing.T) {
	dir := t.TempDir()
	// Write empty file.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "worker.id"), []byte("   \n"), 0o644))

	// Should generate new ID.
	id, err := worker.LoadOrCreate(dir)
	require.NoError(t, err)
	assert.NotEmpty(t, id.ID())
}
