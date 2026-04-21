package store_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
	return store.NewJobRepository(db), store.NewURLRepository(db), store.NewPageRepository(db, store.WithContentDir(t.TempDir()))
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

func TestURLRepository_DedupByNormalized(t *testing.T) {
	jobs, urls, _ := setupDB(t)
	ctx := context.Background()

	job := newJob()
	require.NoError(t, jobs.Create(ctx, job))

	u1 := entities.NewCrawlURL(job.ID, "https://example.com", "https://example.com/", "hash1", 0, "")
	u2 := entities.NewCrawlURL(job.ID, "https://example.com/", "https://example.com/", "hash2", 1, "")

	require.NoError(t, urls.Enqueue(ctx, u1))
	require.NoError(t, urls.Enqueue(ctx, u2))

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

func TestURLRepository_RequeueCrawlingByDomain(t *testing.T) {
	jobs, urls, _ := setupDB(t)
	ctx := context.Background()

	job := newJob()
	require.NoError(t, jobs.Create(ctx, job))

	u1 := entities.NewCrawlURL(job.ID, "https://example.com", "https://example.com/", "hash1", 0, "")
	u2 := entities.NewCrawlURL(job.ID, "https://example.com/about", "https://example.com/about", "hash2", 1, "")
	u3 := entities.NewCrawlURL(job.ID, "https://other.com", "https://other.com/", "hash3", 0, "")
	require.NoError(t, urls.Enqueue(ctx, u1))
	require.NoError(t, urls.Enqueue(ctx, u2))
	require.NoError(t, urls.Enqueue(ctx, u3))

	claimed, err := urls.Claim(ctx, job.ID, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 3)

	requeued, err := urls.RequeueCrawlingByDomain(ctx, job.ID, "example.com")
	require.NoError(t, err)
	assert.Equal(t, int64(2), requeued)

	counts, err := urls.CountByStatus(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, counts[entities.URLStatusPending])
	assert.Equal(t, 1, counts[entities.URLStatusCrawling])
}

func TestURLRepository_RequeueTimedOutCrawling(t *testing.T) {
	jobs, urls, _ := setupDB(t)
	ctx := context.Background()

	job := newJob()
	require.NoError(t, jobs.Create(ctx, job))

	u := entities.NewCrawlURL(job.ID, "https://example.com", "https://example.com/", "hash1", 0, "")
	require.NoError(t, urls.Enqueue(ctx, u))

	claimed, err := urls.Claim(ctx, job.ID, 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	staleAt := time.Now().Add(-2 * time.Minute).UTC()
	require.NoError(t, urls.Complete(ctx, &entities.CrawlURL{
		ID:         claimed[0].ID,
		JobID:      claimed[0].JobID,
		Normalized: claimed[0].Normalized,
		Depth:      claimed[0].Depth,
		Status:     entities.URLStatusCrawling,
		UpdatedAt:  staleAt,
	}))

	requeued, err := urls.RequeueTimedOutCrawling(ctx, time.Now().Add(-1*time.Minute))
	require.NoError(t, err)
	assert.Equal(t, int64(1), requeued)

	found, err := urls.FindByHash(ctx, job.ID, "hash1")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, entities.URLStatusPending, found.Status)
	assert.Equal(t, 1, found.RetryCount)
	assert.Contains(t, found.LastError, "crawl timeout")
}

func TestURLRepository_Complete_PreservesUniqueColumnsForSparseWorkerPayload(t *testing.T) {
	jobs, urls, _ := setupDB(t)
	ctx := context.Background()

	job := newJob()
	require.NoError(t, jobs.Create(ctx, job))

	u1 := entities.NewCrawlURL(job.ID, "https://example.com", "https://example.com", "hash1", 0, "")
	u2 := entities.NewCrawlURL(job.ID, "https://example.com/about", "https://example.com/about", "hash2", 1, "")
	require.NoError(t, urls.Enqueue(ctx, u1))
	require.NoError(t, urls.Enqueue(ctx, u2))

	claimed, err := urls.Claim(ctx, job.ID, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 2)

	for _, claimedURL := range claimed {
		// Worker results only carry the fields needed for state transitions.
		sparse := &entities.CrawlURL{
			ID:         claimedURL.ID,
			JobID:      claimedURL.JobID,
			Normalized: claimedURL.Normalized,
			Depth:      claimedURL.Depth,
			Status:     entities.URLStatusCrawling,
		}
		require.NoError(t, sparse.MarkDone())
		require.NoError(t, urls.Complete(ctx, sparse))
	}

	done1, err := urls.FindByHash(ctx, job.ID, "hash1")
	require.NoError(t, err)
	require.NotNil(t, done1)
	assert.Equal(t, entities.URLStatusDone, done1.Status)
	assert.Equal(t, "hash1", done1.URLHash)
	assert.False(t, done1.CreatedAt.IsZero())

	done2, err := urls.FindByHash(ctx, job.ID, "hash2")
	require.NoError(t, err)
	require.NotNil(t, done2)
	assert.Equal(t, entities.URLStatusDone, done2.Status)
	assert.Equal(t, "hash2", done2.URLHash)
	assert.False(t, done2.CreatedAt.IsZero())
}

func TestURLRepository_FindByJobIDAndStatuses(t *testing.T) {
	jobs, urls, _ := setupDB(t)
	ctx := context.Background()

	job := newJob()
	require.NoError(t, jobs.Create(ctx, job))

	u1 := entities.NewCrawlURL(job.ID, "https://example.com/error", "https://example.com/error", "hash1", 1, "")
	u2 := entities.NewCrawlURL(job.ID, "https://example.com/blocked", "https://example.com/blocked", "hash2", 2, "")
	u3 := entities.NewCrawlURL(job.ID, "https://example.com/done", "https://example.com/done", "hash3", 3, "")
	require.NoError(t, urls.Enqueue(ctx, u1))
	require.NoError(t, urls.Enqueue(ctx, u2))
	require.NoError(t, urls.Enqueue(ctx, u3))

	for _, u := range []*entities.CrawlURL{u1, u2, u3} {
		require.NoError(t, u.Claim())
	}

	require.NoError(t, u1.MarkError())
	u1.LastError = "fetch timeout"
	require.NoError(t, urls.Complete(ctx, u1))

	require.NoError(t, u2.MarkBlocked())
	u2.LastError = "blocked by robots.txt"
	require.NoError(t, urls.Complete(ctx, u2))

	require.NoError(t, u3.MarkDone())
	require.NoError(t, urls.Complete(ctx, u3))

	found, err := urls.FindByJobIDAndStatuses(ctx, job.ID, []entities.URLStatus{entities.URLStatusError, entities.URLStatusBlocked}, 10, 0)
	require.NoError(t, err)
	require.Len(t, found, 2)
	assert.Equal(t, entities.URLStatusBlocked, found[0].Status)
	assert.Equal(t, "blocked by robots.txt", found[0].LastError)
	assert.Equal(t, entities.URLStatusError, found[1].Status)
	assert.Equal(t, "fetch timeout", found[1].LastError)
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
	page.RawContent = []byte("<html><body>example</body></html>")
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
	assert.NotEmpty(t, found.ContentPath)
	assert.Contains(t, filepath.ToSlash(found.ContentPath), "/example.com/")
	assert.Equal(t, int64(len("<html><body>example</body></html>")), found.ContentSize)
	_, err = os.Stat(filepath.Clean(found.ContentPath))
	require.NoError(t, err)
}

func TestPageRepository_StoreAndFind_StagedContent(t *testing.T) {
	jobs, urls, pages := setupDB(t)
	ctx := context.Background()

	job := newJob()
	require.NoError(t, jobs.Create(ctx, job))

	u := entities.NewCrawlURL(job.ID, "https://example.com/video.mp4", "https://example.com/video.mp4", "hash-binary", 0, "")
	require.NoError(t, urls.Enqueue(ctx, u))

	stagedFile := filepath.Join(t.TempDir(), "video.mp4")
	require.NoError(t, os.WriteFile(stagedFile, []byte("mp4-data"), 0o644))

	page := entities.NewPage(u.ID, job.ID)
	page.HTTPStatus = 200
	page.ContentType = "video/mp4"
	page.ContentPath = stagedFile
	page.ContentSize = int64(len("mp4-data"))
	page.FetchedAt = time.Now().UTC()

	require.NoError(t, pages.Store(ctx, page))

	found, err := pages.FindByURLID(ctx, u.ID)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, stagedFile, found.ContentPath)
	assert.Equal(t, int64(len("mp4-data")), found.ContentSize)
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
		p.RawContent = []byte("page")
		p.FetchedAt = time.Now().UTC()
		require.NoError(t, pages.Store(ctx, p))
	}

	found, err := pages.FindByJobID(ctx, job.ID, 10, 0)
	require.NoError(t, err)
	assert.Len(t, found, 3)
}

func TestBuildContentPath_UsesASCIIDomainDirectory(t *testing.T) {
	path, err := store.BuildContentPathForTest("data", "https://www.žąsys.lt/paslaugos", "text/html; charset=utf-8")
	require.NoError(t, err)
	assert.Contains(t, path, "data/www.xn--sys-hpa81d.lt/")
	assert.True(t, strings.HasSuffix(path, ".html"))
}
