package store_test

import (
	"context"
	"testing"
	"time"

	store "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/db/gorm"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/valueobj"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func createTestJob(t *testing.T, db *gorm.DB) *entities.Job {
	t.Helper()
	jobRepo := store.NewJobRepository(db)
	job := entities.NewJob("https://example.com", valueobj.CrawlConfig{
		Scope:    valueobj.ScopeSameDomain,
		MaxDepth: 3,
	})
	require.NoError(t, jobRepo.Create(context.Background(), job))
	return job
}

func setupWorkerDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := store.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, store.Migrate(db))
	return db
}

func TestWorkerRepository_RegisterAndFind(t *testing.T) {
	db := setupWorkerDB(t)
	repo := store.NewWorkerRepository(db)
	ctx := context.Background()

	w := entities.NewWorker("host-1", 10)
	require.NoError(t, repo.Register(ctx, w))

	found, err := repo.FindByID(ctx, w.ID)
	require.NoError(t, err)
	assert.Equal(t, w.ID, found.ID)
	assert.Equal(t, "host-1", found.Hostname)
	assert.Equal(t, entities.WorkerStatusOnline, found.Status)
}

func TestWorkerRepository_Heartbeat(t *testing.T) {
	db := setupWorkerDB(t)
	repo := store.NewWorkerRepository(db)
	ctx := context.Background()

	w := entities.NewWorker("host-1", 10)
	require.NoError(t, repo.Register(ctx, w))

	newTime := time.Now().UTC().Add(5 * time.Second)
	require.NoError(t, repo.UpdateHeartbeat(ctx, w.ID, newTime))

	found, err := repo.FindByID(ctx, w.ID)
	require.NoError(t, err)
	assert.True(t, found.LastHeartbeat.After(w.LastHeartbeat))
}

func TestWorkerRepository_FindStale(t *testing.T) {
	db := setupWorkerDB(t)
	repo := store.NewWorkerRepository(db)
	ctx := context.Background()

	// Fresh worker.
	w1 := entities.NewWorker("host-1", 10)
	require.NoError(t, repo.Register(ctx, w1))

	// Stale worker (heartbeat 30s ago).
	w2 := entities.NewWorker("host-2", 5)
	w2.LastHeartbeat = time.Now().UTC().Add(-30 * time.Second)
	require.NoError(t, repo.Register(ctx, w2))

	stale, err := repo.FindStale(ctx, 15*time.Second)
	require.NoError(t, err)
	assert.Len(t, stale, 1)
	assert.Equal(t, w2.ID, stale[0].ID)
}

func TestWorkerRepository_MarkOffline(t *testing.T) {
	db := setupWorkerDB(t)
	repo := store.NewWorkerRepository(db)
	ctx := context.Background()

	w := entities.NewWorker("host-1", 10)
	require.NoError(t, repo.Register(ctx, w))
	require.NoError(t, repo.MarkOffline(ctx, w.ID))

	found, _ := repo.FindByID(ctx, w.ID)
	assert.Equal(t, entities.WorkerStatusOffline, found.Status)
}

func TestWorkerRepository_ListOnline(t *testing.T) {
	db := setupWorkerDB(t)
	repo := store.NewWorkerRepository(db)
	ctx := context.Background()

	w1 := entities.NewWorker("host-1", 10)
	w2 := entities.NewWorker("host-2", 5)
	require.NoError(t, repo.Register(ctx, w1))
	require.NoError(t, repo.Register(ctx, w2))
	require.NoError(t, repo.MarkOffline(ctx, w2.ID))

	online, err := repo.ListOnline(ctx)
	require.NoError(t, err)
	assert.Len(t, online, 1)
	assert.Equal(t, w1.ID, online[0].ID)
}

func TestDomainAssignmentRepository_AssignAndFind(t *testing.T) {
	db := setupWorkerDB(t)
	workerRepo := store.NewWorkerRepository(db)
	domainRepo := store.NewDomainAssignmentRepository(db)
	ctx := context.Background()
	job := createTestJob(t, db)

	w := entities.NewWorker("host-1", 10)
	require.NoError(t, workerRepo.Register(ctx, w))

	a := entities.NewDomainAssignment(w.ID, job.ID, "example.com", 3)
	require.NoError(t, domainRepo.Assign(ctx, a))

	// Find by worker.
	assignments, err := domainRepo.FindByWorker(ctx, w.ID)
	require.NoError(t, err)
	assert.Len(t, assignments, 1)
	assert.Equal(t, "example.com", assignments[0].Domain)

	// Find by domain.
	found, err := domainRepo.FindByDomain(ctx, job.ID, "example.com")
	require.NoError(t, err)
	assert.NotNil(t, found)
	assert.Equal(t, w.ID, found.WorkerID)
}

func TestDomainAssignmentRepository_Release(t *testing.T) {
	db := setupWorkerDB(t)
	workerRepo := store.NewWorkerRepository(db)
	domainRepo := store.NewDomainAssignmentRepository(db)
	ctx := context.Background()
	job := createTestJob(t, db)

	w := entities.NewWorker("host-1", 10)
	require.NoError(t, workerRepo.Register(ctx, w))

	a := entities.NewDomainAssignment(w.ID, job.ID, "example.com", 2)
	require.NoError(t, domainRepo.Assign(ctx, a))
	require.NoError(t, domainRepo.Release(ctx, a.ID))

	// Should not find active assignment.
	found, err := domainRepo.FindByDomain(ctx, job.ID, "example.com")
	require.NoError(t, err)
	assert.Nil(t, found)

	// Worker has no active assignments.
	assignments, err := domainRepo.FindByWorker(ctx, w.ID)
	require.NoError(t, err)
	assert.Len(t, assignments, 0)
}

func TestDomainAssignmentRepository_ReleaseByWorker(t *testing.T) {
	db := setupWorkerDB(t)
	workerRepo := store.NewWorkerRepository(db)
	domainRepo := store.NewDomainAssignmentRepository(db)
	ctx := context.Background()
	job := createTestJob(t, db)

	w := entities.NewWorker("host-1", 10)
	require.NoError(t, workerRepo.Register(ctx, w))

	a1 := entities.NewDomainAssignment(w.ID, job.ID, "example.com", 2)
	a2 := entities.NewDomainAssignment(w.ID, job.ID, "other.com", 2)
	require.NoError(t, domainRepo.Assign(ctx, a1))
	require.NoError(t, domainRepo.Assign(ctx, a2))

	// Release all by worker.
	require.NoError(t, domainRepo.ReleaseByWorker(ctx, w.ID))

	assignments, err := domainRepo.FindByWorker(ctx, w.ID)
	require.NoError(t, err)
	assert.Len(t, assignments, 0)
}

func TestDomainAssignmentRepository_UniqueConstraint(t *testing.T) {
	db := setupWorkerDB(t)
	workerRepo := store.NewWorkerRepository(db)
	domainRepo := store.NewDomainAssignmentRepository(db)
	ctx := context.Background()
	job := createTestJob(t, db)

	w1 := entities.NewWorker("host-1", 10)
	w2 := entities.NewWorker("host-2", 10)
	require.NoError(t, workerRepo.Register(ctx, w1))
	require.NoError(t, workerRepo.Register(ctx, w2))

	a1 := entities.NewDomainAssignment(w1.ID, job.ID, "example.com", 2)
	require.NoError(t, domainRepo.Assign(ctx, a1))

	// Second worker trying same domain should fail (unique constraint).
	a2 := entities.NewDomainAssignment(w2.ID, job.ID, "example.com", 2)
	err := domainRepo.Assign(ctx, a2)
	assert.Error(t, err)
}
