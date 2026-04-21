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
)

func setupDB(t *testing.T) (*store.JobRepository, *store.URLRepository, *store.PageRepository) {
	t.Helper()
	db, err := store.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, store.Migrate(db))
	return store.NewJobRepository(db), store.NewURLRepository(db), store.NewPageRepository(db)
}

func newJob() *entities.Job {
	return entities.NewJob("https://example.com", valueobj.CrawlConfig{
		Scope:      valueobj.ScopeSameDomain,
		MaxDepth:   5,
		Extraction: valueobj.ExtractionStandard,
	})
}

// --- Job Repository Tests ---

func TestJobRepository_Create(t *testing.T) {
	jobs, _, _ := setupDB(t)
	ctx := context.Background()
	job := newJob()

	err := jobs.Create(ctx, job)
	require.NoError(t, err)

	found, err := jobs.FindByID(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, job.ID, found.ID)
	assert.Equal(t, job.SeedURL, found.SeedURL)
	assert.Equal(t, entities.JobStatusPending, found.Status)
}

func TestJobRepository_Update(t *testing.T) {
	jobs, _, _ := setupDB(t)
	ctx := context.Background()
	job := newJob()
	require.NoError(t, jobs.Create(ctx, job))

	require.NoError(t, job.Start())
	require.NoError(t, jobs.Update(ctx, job))

	found, err := jobs.FindByID(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, entities.JobStatusRunning, found.Status)
}

func TestJobRepository_List(t *testing.T) {
	jobs, _, _ := setupDB(t)
	ctx := context.Background()

	for range 3 {
		require.NoError(t, jobs.Create(ctx, newJob()))
	}

	list, err := jobs.List(ctx, 10, 0)
	require.NoError(t, err)
	assert.Len(t, list, 3)
}

func TestJobRepository_FindByStatus(t *testing.T) {
	jobs, _, _ := setupDB(t)
	ctx := context.Background()

	j1 := newJob()
	require.NoError(t, jobs.Create(ctx, j1))

	j2 := newJob()
	require.NoError(t, j2.Start())
	require.NoError(t, jobs.Create(ctx, j2))

	pending, err := jobs.FindByStatus(ctx, entities.JobStatusPending)
	require.NoError(t, err)
	assert.Len(t, pending, 1)
	assert.Equal(t, j1.ID, pending[0].ID)
}

func TestJobRepository_FindByID_NotFound(t *testing.T) {
	jobs, _, _ := setupDB(t)
	found, err := jobs.FindByID(context.Background(), "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, found)
}

// --- URL Repository Tests ---

func TestURLRepository_EnqueueAndClaim(t *testing.T) {
	jobs, urls, _ := setupDB(t)
	ctx := context.Background()

	job := newJob()
	require.NoError(t, jobs.Create(ctx, job))

	u := entities.NewCrawlURL(job.ID, "https://example.com", "https://example.com", "hash1", 0, "")
	require.NoError(t, urls.Enqueue(ctx, u))

	claimed, err := urls.Claim(ctx, job.ID, 10)
	require.NoError(t, err)
	assert.Len(t, claimed, 1)
	assert.Equal(t, entities.URLStatusCrawling, claimed[0].Status)
}

func TestURLRepository_EnqueueBatch(t *testing.T) {
	jobs, urls, _ := setupDB(t)
	ctx := context.Background()

	job := newJob()
	require.NoError(t, jobs.Create(ctx, job))

	batch := make([]*entities.CrawlURL, 5)
	for i := range 5 {
		batch[i] = entities.NewCrawlURL(job.ID, "https://example.com/"+string(rune('a'+i)), "https://example.com/"+string(rune('a'+i)), "hash"+string(rune('0'+i)), 1, "")
	}
	require.NoError(t, urls.EnqueueBatch(ctx, batch))

	pending, err := urls.FindPending(ctx, job.ID, 100)
	require.NoError(t, err)
	assert.Len(t, pending, 5)
}

func TestURLRepository_Dedup(t *testing.T) {
	jobs, urls, _ := setupDB(t)
	ctx := context.Background()

	job := newJob()
	require.NoError(t, jobs.Create(ctx, job))

	u1 := entities.NewCrawlURL(job.ID, "https://example.com", "https://example.com", "samehash", 0, "")
	u2 := entities.NewCrawlURL(job.ID, "https://example.com", "https://example.com", "samehash", 0, "")

	require.NoError(t, urls.Enqueue(ctx, u1))
	require.NoError(t, urls.Enqueue(ctx, u2)) // should be ignored

	pending, err := urls.FindPending(ctx, job.ID, 100)
	require.NoError(t, err)
	assert.Len(t, pending, 1)
}

func TestURLRepository_FindByHash(t *testing.T) {
	jobs, urls, _ := setupDB(t)
	ctx := context.Background()

	job := newJob()
	require.NoError(t, jobs.Create(ctx, job))

	u := entities.NewCrawlURL(job.ID, "https://example.com", "https://example.com", "testhash", 0, "")
	require.NoError(t, urls.Enqueue(ctx, u))

	found, err := urls.FindByHash(ctx, job.ID, "testhash")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "testhash", found.URLHash)

	notFound, err := urls.FindByHash(ctx, job.ID, "nope")
	require.NoError(t, err)
	assert.Nil(t, notFound)
}

func TestURLRepository_CountByStatus(t *testing.T) {
	jobs, urls, _ := setupDB(t)
	ctx := context.Background()

	job := newJob()
	require.NoError(t, jobs.Create(ctx, job))

	for i := range 3 {
		u := entities.NewCrawlURL(job.ID, "https://example.com/"+string(rune('a'+i)), "https://example.com/"+string(rune('a'+i)), "h"+string(rune('0'+i)), 0, "")
		require.NoError(t, urls.Enqueue(ctx, u))
	}

	counts, err := urls.CountByStatus(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, 3, counts[entities.URLStatusPending])
}

// --- Page Repository Tests ---

func TestPageRepository_StoreAndFind(t *testing.T) {
	jobs, urls, pages := setupDB(t)
	ctx := context.Background()

	job := newJob()
	require.NoError(t, jobs.Create(ctx, job))

	u := entities.NewCrawlURL(job.ID, "https://example.com", "https://example.com", "hash1", 0, "")
	require.NoError(t, urls.Enqueue(ctx, u))

	page := entities.NewPage(u.ID, job.ID)
	page.HTTPStatus = 200
	page.Title = "Example"
	page.ContentType = "text/html"
	page.FetchedAt = time.Now().UTC()
	page.FetchDuration = 150 * time.Millisecond
	page.Headers = map[string]string{"Content-Type": "text/html"}
	page.Links = []entities.DiscoveredLink{
		{RawURL: "https://example.com/about", IsExternal: false},
	}

	require.NoError(t, pages.Store(ctx, page))

	found, err := pages.FindByURLID(ctx, u.ID)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, 200, found.HTTPStatus)
	assert.Equal(t, "Example", found.Title)
	assert.Len(t, found.Links, 1)
	assert.Equal(t, 150*time.Millisecond, found.FetchDuration)
}

func TestPageRepository_FindByJobID(t *testing.T) {
	jobs, urls, pages := setupDB(t)
	ctx := context.Background()

	job := newJob()
	require.NoError(t, jobs.Create(ctx, job))

	for i := range 3 {
		u := entities.NewCrawlURL(job.ID, "https://example.com/"+string(rune('a'+i)), "https://example.com/"+string(rune('a'+i)), "h"+string(rune('0'+i)), 0, "")
		require.NoError(t, urls.Enqueue(ctx, u))

		p := entities.NewPage(u.ID, job.ID)
		p.HTTPStatus = 200
		p.FetchedAt = time.Now().UTC()
		require.NoError(t, pages.Store(ctx, p))
	}

	found, err := pages.FindByJobID(ctx, job.ID, 10, 0)
	require.NoError(t, err)
	assert.Len(t, found, 3)
}
