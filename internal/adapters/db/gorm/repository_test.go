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

func TestURLRepository_EnqueueUpdatesExistingNormalizedRow(t *testing.T) {
	jobs, urls, _ := setupDB(t)
	ctx := context.Background()

	job := newJob()
	require.NoError(t, jobs.Create(ctx, job))

	first := entities.NewCrawlURL(job.ID, "https://example.com/one", "https://example.com/", "hash1", 5, "")
	second := entities.NewCrawlURL(job.ID, "https://example.com/two", "https://example.com/", "hash2", 2, "https://example.com/src")

	require.NoError(t, urls.Enqueue(ctx, first))
	require.NoError(t, urls.Enqueue(ctx, second))

	pending, err := urls.FindPending(ctx, job.ID, 10)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, 2, pending[0].Depth)
	assert.Equal(t, "https://example.com/src", pending[0].FoundOn)
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

func TestURLRepository_RequeueCrawlingByJob(t *testing.T) {
	jobs, urls, _ := setupDB(t)
	ctx := context.Background()

	job := newJob()
	require.NoError(t, jobs.Create(ctx, job))

	u1 := entities.NewCrawlURL(job.ID, "https://example.com", "https://example.com/", "hash1", 0, "")
	u2 := entities.NewCrawlURL(job.ID, "https://example.com/about", "https://example.com/about", "hash2", 1, "")
	u3 := entities.NewCrawlURL(job.ID, "https://example.com/contact", "https://example.com/contact", "hash3", 1, "")
	require.NoError(t, urls.Enqueue(ctx, u1))
	require.NoError(t, urls.Enqueue(ctx, u2))
	require.NoError(t, urls.Enqueue(ctx, u3))

	claimed, err := urls.Claim(ctx, job.ID, 2)
	require.NoError(t, err)
	require.Len(t, claimed, 2)

	requeued, err := urls.RequeueCrawlingByJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), requeued)

	counts, err := urls.CountByStatus(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, 3, counts[entities.URLStatusPending])
	assert.Equal(t, 0, counts[entities.URLStatusCrawling])
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

func TestURLRepository_RequeueTimedOutCrawlingWithLimitMarksErrorsAfterRetryExhaustion(t *testing.T) {
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
		RetryCount: 2,
		UpdatedAt:  staleAt,
	}))

	requeued, failed, err := urls.RequeueTimedOutCrawlingWithLimit(ctx, time.Now().Add(-1*time.Minute), 3)
	require.NoError(t, err)
	assert.Equal(t, int64(0), requeued)
	assert.Equal(t, int64(1), failed)

	found, err := urls.FindByHash(ctx, job.ID, "hash1")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, entities.URLStatusError, found.Status)
	assert.Equal(t, 3, found.RetryCount)
	assert.Contains(t, found.LastError, "max retries exceeded")
}

func TestURLRepository_FailPendingOverRetryLimit(t *testing.T) {
	jobs, urls, _ := setupDB(t)
	ctx := context.Background()

	job := newJob()
	require.NoError(t, jobs.Create(ctx, job))

	exhausted := entities.NewCrawlURL(job.ID, "https://example.com/a", "https://example.com/a", "hash-a", 0, "")
	exhausted.RetryCount = 3
	fresh := entities.NewCrawlURL(job.ID, "https://example.com/b", "https://example.com/b", "hash-b", 0, "")

	require.NoError(t, urls.Enqueue(ctx, exhausted))
	require.NoError(t, urls.Enqueue(ctx, fresh))

	failed, err := urls.FailPendingOverRetryLimit(ctx, job.ID, 3)
	require.NoError(t, err)
	assert.Equal(t, int64(1), failed)

	foundExhausted, err := urls.FindByHash(ctx, job.ID, "hash-a")
	require.NoError(t, err)
	require.NotNil(t, foundExhausted)
	assert.Equal(t, entities.URLStatusError, foundExhausted.Status)
	assert.Contains(t, foundExhausted.LastError, "max retries exceeded")

	foundFresh, err := urls.FindByHash(ctx, job.ID, "hash-b")
	require.NoError(t, err)
	require.NotNil(t, foundFresh)
	assert.Equal(t, entities.URLStatusPending, foundFresh.Status)
}

func TestURLRepository_RequeueDueRevisits(t *testing.T) {
	jobs, urls, _ := setupDB(t)
	ctx := context.Background()

	job := newJob()
	require.NoError(t, jobs.Create(ctx, job))

	u := entities.NewCrawlURL(job.ID, "https://example.com", "https://example.com/", "hash-revisit", 0, "")
	require.NoError(t, urls.Enqueue(ctx, u))

	claimed, err := urls.Claim(ctx, job.ID, 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.NoError(t, claimed[0].MarkDone())
	claimed[0].RevisitAt = time.Now().Add(-time.Minute)
	require.NoError(t, urls.Complete(ctx, claimed[0]))

	requeued, err := urls.RequeueDueRevisits(ctx, time.Now())
	require.NoError(t, err)
	assert.Equal(t, int64(1), requeued)

	pending, err := urls.FindPending(ctx, job.ID, 10)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.True(t, pending[0].RevisitAt.IsZero())
}

func TestURLRepository_RequeueJobForRevisit(t *testing.T) {
	jobs, urls, _ := setupDB(t)
	ctx := context.Background()

	job := newJob()
	require.NoError(t, jobs.Create(ctx, job))

	doneURL := entities.NewCrawlURL(job.ID, "https://example.com/done", "https://example.com/done", "hash-done", 0, "")
	errURL := entities.NewCrawlURL(job.ID, "https://example.com/error", "https://example.com/error", "hash-error", 1, "")
	blockedURL := entities.NewCrawlURL(job.ID, "https://example.com/blocked", "https://example.com/blocked", "hash-blocked", 1, "")
	pendingURL := entities.NewCrawlURL(job.ID, "https://example.com/pending", "https://example.com/pending", "hash-pending", 2, "")

	for _, u := range []*entities.CrawlURL{doneURL, errURL, blockedURL, pendingURL} {
		require.NoError(t, urls.Enqueue(ctx, u))
	}

	for _, u := range []*entities.CrawlURL{doneURL, errURL, blockedURL} {
		require.NoError(t, u.Claim())
	}

	require.NoError(t, doneURL.MarkDone())
	doneURL.RevisitAt = time.Now().Add(2 * time.Hour)
	require.NoError(t, urls.Complete(ctx, doneURL))

	require.NoError(t, errURL.MarkError())
	errURL.LastError = "timeout"
	errURL.RetryCount = 2
	require.NoError(t, urls.Complete(ctx, errURL))

	require.NoError(t, blockedURL.MarkBlocked())
	blockedURL.LastError = "blocked"
	blockedURL.RetryCount = 1
	require.NoError(t, urls.Complete(ctx, blockedURL))

	requeued, err := urls.RequeueJobForRevisit(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(3), requeued)

	pending, err := urls.FindPending(ctx, job.ID, 10)
	require.NoError(t, err)
	require.Len(t, pending, 4)
}

func TestURLRepository_DedupeJobURLs(t *testing.T) {
	db, err := store.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, store.Migrate(db))
	jobs := store.NewJobRepository(db)
	urls := store.NewURLRepository(db)
	pages := store.NewPageRepository(db, store.WithContentDir(filepath.Join(t.TempDir(), "data")))
	ctx := context.Background()

	job := newJob()
	require.NoError(t, jobs.Create(ctx, job))

	require.NoError(t, db.Exec("DROP INDEX IF EXISTS idx_urls_job_normalized").Error)

	keep := entities.NewCrawlURL(job.ID, "https://example.com/a", "https://example.com/x", "hash-keep", 1, "")
	drop := entities.NewCrawlURL(job.ID, "https://example.com/b", "https://example.com/x", "hash-drop", 3, "https://example.com/src")
	keep.Status = entities.URLStatusDone
	drop.Status = entities.URLStatusError
	require.NoError(t, db.Exec(`INSERT INTO urls (id, job_id, raw_url, normalized, url_hash, depth, status, retry_count, revisit_at, found_on, created_at, updated_at, last_error) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		keep.ID, keep.JobID, keep.RawURL, keep.Normalized, keep.URLHash, keep.Depth, string(keep.Status), keep.RetryCount, nil, keep.FoundOn, keep.CreatedAt, keep.UpdatedAt, keep.LastError).Error)
	require.NoError(t, db.Exec(`INSERT INTO urls (id, job_id, raw_url, normalized, url_hash, depth, status, retry_count, revisit_at, found_on, created_at, updated_at, last_error) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		drop.ID, drop.JobID, drop.RawURL, drop.Normalized, drop.URLHash, drop.Depth, string(drop.Status), drop.RetryCount, nil, drop.FoundOn, drop.CreatedAt, drop.UpdatedAt, drop.LastError).Error)

	page1 := entities.NewPage(keep.ID, job.ID)
	page1.HTTPStatus = 200
	page1.ContentType = "text/html"
	page1.RawContent = []byte("keep")
	page1.FetchedAt = time.Now().Add(-time.Hour).UTC()
	require.NoError(t, pages.Store(ctx, page1))

	page2 := entities.NewPage(drop.ID, job.ID)
	page2.HTTPStatus = 200
	page2.ContentType = "text/html"
	page2.RawContent = []byte("drop")
	page2.FetchedAt = time.Now().UTC()
	require.NoError(t, pages.Store(ctx, page2))

	require.NoError(t, db.Exec(`INSERT INTO antibot_events (id, url_id, job_id, event_type, provider, strategy, resolved, details, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"evt-1", drop.ID, job.ID, "blocked", "cloudflare", "skip", false, "{}", time.Now().UTC()).Error)

	deleted, err := urls.DedupeJobURLs(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	var urlCount int64
	require.NoError(t, db.Table("urls").Where("job_id = ?", job.ID).Count(&urlCount).Error)
	assert.Equal(t, int64(1), urlCount)

	merged, err := urls.FindPending(ctx, job.ID, 10)
	require.NoError(t, err)
	assert.Len(t, merged, 0)

	var finalURL store.URLModel
	require.NoError(t, db.Where("job_id = ?", job.ID).First(&finalURL).Error)
	assert.Equal(t, keep.ID, finalURL.ID)
	assert.Equal(t, 1, finalURL.Depth)
	assert.Equal(t, "https://example.com/src", finalURL.FoundOn)

	storedPage, err := pages.FindByURLID(ctx, keep.ID)
	require.NoError(t, err)
	require.NotNil(t, storedPage)
	assert.Equal(t, page2.ID, storedPage.ID)

	var antibotURLID string
	require.NoError(t, db.Table("antibot_events").Select("url_id").Where("id = ?", "evt-1").Scan(&antibotURLID).Error)
	assert.Equal(t, keep.ID, antibotURLID)
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
	page.TextContent = "example"
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
	assert.Equal(t, "example", found.TextContent)
	assert.Contains(t, filepath.ToSlash(found.ContentPath), "/example.com/")
	assert.Equal(t, int64(len("<html><body>example</body></html>")), found.ContentSize)
	_, err = os.Stat(filepath.Clean(found.ContentPath))
	require.NoError(t, err)
}

func TestPageRepository_StoreAndFind_PersistsTextContent(t *testing.T) {
	jobs, urls, pages := setupDB(t)
	ctx := context.Background()

	job := newJob()
	require.NoError(t, jobs.Create(ctx, job))

	u := entities.NewCrawlURL(job.ID, "https://example.com/file.csv", "https://example.com/file.csv", "hash-text", 0, "")
	require.NoError(t, urls.Enqueue(ctx, u))

	page := entities.NewPage(u.ID, job.ID)
	page.HTTPStatus = 200
	page.ContentType = "text/csv"
	page.TextContent = "name,email John,john@example.com"
	page.RawContent = []byte("name,email\nJohn,john@example.com\n")
	page.FetchedAt = time.Now().UTC()

	require.NoError(t, pages.Store(ctx, page))

	found, err := pages.FindByURLID(ctx, u.ID)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "name,email John,john@example.com", found.TextContent)
}

func TestPageRepository_BackfillTextContent(t *testing.T) {
	jobs, urls, pages := setupDB(t)
	ctx := context.Background()

	job := newJob()
	require.NoError(t, jobs.Create(ctx, job))

	u := entities.NewCrawlURL(job.ID, "https://example.com/file.csv", "https://example.com/file.csv", "hash-backfill", 0, "")
	require.NoError(t, urls.Enqueue(ctx, u))

	page := entities.NewPage(u.ID, job.ID)
	page.HTTPStatus = 200
	page.ContentType = "text/csv"
	page.RawContent = []byte("name,email\nJohn,john@example.com\n")
	page.FetchedAt = time.Now().UTC()
	require.NoError(t, pages.Store(ctx, page))

	updated, err := pages.BackfillTextContent(ctx, job.ID, 100)
	require.NoError(t, err)
	assert.Equal(t, 1, updated)

	found, err := pages.FindByURLID(ctx, u.ID)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "name,email John,john@example.com", found.TextContent)
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
