package entities_test

import (
	"testing"
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
	"github.com/stretchr/testify/assert"
)

func TestNewWorker(t *testing.T) {
	w := entities.NewWorker("host-1", 10)
	assert.NotEmpty(t, w.ID)
	assert.Equal(t, "host-1", w.Hostname)
	assert.Equal(t, entities.WorkerStatusOnline, w.Status)
	assert.Equal(t, 10, w.PoolSize)
}

func TestRecoverWorker(t *testing.T) {
	w := entities.RecoverWorker("my-id", "host-1", 5)
	assert.Equal(t, "my-id", w.ID)
	assert.Equal(t, entities.WorkerStatusOnline, w.Status)
}

func TestWorker_Heartbeat(t *testing.T) {
	w := entities.NewWorker("host-1", 10)
	old := w.LastHeartbeat
	time.Sleep(1 * time.Millisecond)
	w.Heartbeat()
	assert.True(t, w.LastHeartbeat.After(old))
}

func TestWorker_IsAlive(t *testing.T) {
	w := entities.NewWorker("host-1", 10)
	assert.True(t, w.IsAlive(15*time.Second))

	w.LastHeartbeat = time.Now().Add(-20 * time.Second)
	assert.False(t, w.IsAlive(15*time.Second))
}

func TestWorker_MarkOffline(t *testing.T) {
	w := entities.NewWorker("host-1", 10)
	w.MarkOffline()
	assert.Equal(t, entities.WorkerStatusOffline, w.Status)
}

func TestDomainAssignment_Lifecycle(t *testing.T) {
	da := entities.NewDomainAssignment("w1", "j1", "example.com", 3)
	assert.NotEmpty(t, da.ID)
	assert.Equal(t, "example.com", da.Domain)
	assert.Equal(t, 3, da.Concurrency)
	assert.True(t, da.IsActive())

	da.Release()
	assert.False(t, da.IsActive())
	assert.False(t, da.ReleasedAt.IsZero())
}
