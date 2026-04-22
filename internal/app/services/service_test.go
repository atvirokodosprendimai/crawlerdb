package services_test

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"time"

	store "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/db/gorm"
	fetcher "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/http"
	broker "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/nats"
	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/robots"
	"github.com/atvirokodosprendimai/crawlerdb/internal/app/services"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/valueobj"
	natsserver "github.com/nats-io/nats-server/v2/test"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// helpers

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := store.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, store.Migrate(db))
	return db
}

func setupTestNATS(t *testing.T) *broker.NATSBroker {
	t.Helper()
	opts := natsserver.DefaultTestOptions
	opts.Port = -1
	s := natsserver.RunServer(&opts)
	t.Cleanup(s.Shutdown)

	b, err := broker.New(s.ClientURL(), nats.Name("test"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = b.Close() })
	return b
}

func testConfig() valueobj.CrawlConfig {
	return valueobj.CrawlConfig{
		Scope:      valueobj.ScopeSameDomain,
		MaxDepth:   3,
		Extraction: valueobj.ExtractionStandard,
		UserAgent:  "TestBot/1.0",
	}
}

// --- JobService Tests ---

func TestJobService_CreateJob(t *testing.T) {
	db := setupTestDB(t)
	b := setupTestNATS(t)
	jobRepo := store.NewJobRepository(db)
	urlRepo := store.NewURLRepository(db)
	svc := services.NewJobService(jobRepo, urlRepo, b)
	ctx := context.Background()

	job, err := svc.CreateJob(ctx, "https://example.com", testConfig())
	require.NoError(t, err)
	assert.NotEmpty(t, job.ID)
	assert.Equal(t, entities.JobStatusPending, job.Status)

	// Verify persisted.
	found, err := svc.GetJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, job.ID, found.ID)
}

func TestJobService_CreateJobRejectsActiveDomainDuplicate(t *testing.T) {
	db := setupTestDB(t)
	b := setupTestNATS(t)
	jobRepo := store.NewJobRepository(db)
	urlRepo := store.NewURLRepository(db)
	svc := services.NewJobService(jobRepo, urlRepo, b)
	ctx := context.Background()

	job, err := svc.CreateJob(ctx, "https://example.com", testConfig())
	require.NoError(t, err)
	require.NoError(t, svc.StartJob(ctx, job.ID))

	_, err = svc.CreateJob(ctx, "https://example.com/about", testConfig())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists for domain example.com")

	require.NoError(t, svc.StopJob(ctx, job.ID))
	_, err = svc.CreateJob(ctx, "https://example.com/about", testConfig())
	require.NoError(t, err)
}

func TestJobService_StartPauseResumeStop(t *testing.T) {
	db := setupTestDB(t)
	b := setupTestNATS(t)
	jobRepo := store.NewJobRepository(db)
	urlRepo := store.NewURLRepository(db)
	svc := services.NewJobService(jobRepo, urlRepo, b)
	ctx := context.Background()

	job, err := svc.CreateJob(ctx, "https://example.com", testConfig())
	require.NoError(t, err)

	require.NoError(t, svc.StartJob(ctx, job.ID))
	found, _ := svc.GetJob(ctx, job.ID)
	assert.Equal(t, entities.JobStatusRunning, found.Status)

	require.NoError(t, svc.PauseJob(ctx, job.ID))
	found, _ = svc.GetJob(ctx, job.ID)
	assert.Equal(t, entities.JobStatusPaused, found.Status)

	require.NoError(t, svc.ResumeJob(ctx, job.ID))
	found, _ = svc.GetJob(ctx, job.ID)
	assert.Equal(t, entities.JobStatusRunning, found.Status)

	require.NoError(t, svc.StopJob(ctx, job.ID))
	found, _ = svc.GetJob(ctx, job.ID)
	assert.Equal(t, entities.JobStatusStopped, found.Status)
}

func TestJobService_StopJob_RequeuesCrawlingURLs(t *testing.T) {
	db := setupTestDB(t)
	b := setupTestNATS(t)
	jobRepo := store.NewJobRepository(db)
	urlRepo := store.NewURLRepository(db)
	svc := services.NewJobService(jobRepo, urlRepo, b)
	ctx := context.Background()

	job, err := svc.CreateJob(ctx, "https://example.com", testConfig())
	require.NoError(t, err)
	require.NoError(t, svc.StartJob(ctx, job.ID))

	u1 := entities.NewCrawlURL(job.ID, "https://example.com", "https://example.com/", "hash1", 0, "")
	u2 := entities.NewCrawlURL(job.ID, "https://example.com/about", "https://example.com/about", "hash2", 1, "")
	require.NoError(t, urlRepo.Enqueue(ctx, u1))
	require.NoError(t, urlRepo.Enqueue(ctx, u2))

	claimed, err := urlRepo.Claim(ctx, job.ID, 2)
	require.NoError(t, err)
	require.Len(t, claimed, 2)

	require.NoError(t, svc.StopJob(ctx, job.ID))

	found, err := svc.GetJob(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, entities.JobStatusStopped, found.Status)

	counts, err := urlRepo.CountByStatus(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, counts[entities.URLStatusPending])
	assert.Equal(t, 0, counts[entities.URLStatusCrawling])
}

func TestJobService_ListJobs(t *testing.T) {
	db := setupTestDB(t)
	b := setupTestNATS(t)
	jobRepo := store.NewJobRepository(db)
	urlRepo := store.NewURLRepository(db)
	svc := services.NewJobService(jobRepo, urlRepo, b)
	ctx := context.Background()

	for i := range 3 {
		_, err := svc.CreateJob(ctx, "https://example"+string(rune('a'+i))+".com", testConfig())
		require.NoError(t, err)
	}

	jobs, err := svc.ListJobs(ctx, 10, 0)
	require.NoError(t, err)
	assert.Len(t, jobs, 3)
}

func TestJobService_RetryJob(t *testing.T) {
	db := setupTestDB(t)
	b := setupTestNATS(t)
	jobRepo := store.NewJobRepository(db)
	urlRepo := store.NewURLRepository(db)
	svc := services.NewJobService(jobRepo, urlRepo, b)
	ctx := context.Background()

	job, err := svc.CreateJob(ctx, "https://example.com", testConfig())
	require.NoError(t, err)
	require.NoError(t, svc.StartJob(ctx, job.ID))
	require.NoError(t, svc.StopJob(ctx, job.ID))

	retried, err := svc.RetryJob(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, retried)
	assert.NotEqual(t, job.ID, retried.ID)
	assert.Equal(t, job.SeedURL, retried.SeedURL)
	assert.Equal(t, job.Config, retried.Config)
	assert.Equal(t, entities.JobStatusPending, retried.Status)
}

func TestJobService_RevisitJob(t *testing.T) {
	db := setupTestDB(t)
	b := setupTestNATS(t)
	jobRepo := store.NewJobRepository(db)
	urlRepo := store.NewURLRepository(db)
	svc := services.NewJobService(jobRepo, urlRepo, b)
	ctx := context.Background()

	job, err := svc.CreateJob(ctx, "https://example.com", testConfig())
	require.NoError(t, err)
	require.NoError(t, svc.StartJob(ctx, job.ID))

	doneURL := entities.NewCrawlURL(job.ID, "https://example.com/done", "https://example.com/done", "hash-done", 0, "")
	require.NoError(t, urlRepo.Enqueue(ctx, doneURL))
	require.NoError(t, doneURL.Claim())
	require.NoError(t, doneURL.MarkDone())
	doneURL.RevisitAt = time.Now().Add(time.Hour)
	require.NoError(t, urlRepo.Complete(ctx, doneURL))

	require.NoError(t, svc.CompleteJob(ctx, job.ID))

	requeued, err := svc.RevisitJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), requeued)

	found, err := svc.GetJob(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, entities.JobStatusRunning, found.Status)
	assert.True(t, found.FinishedAt.IsZero())

	pending, err := urlRepo.FindPending(ctx, job.ID, 10)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, "hash-done", pending[0].URLHash)
	assert.True(t, pending[0].RevisitAt.IsZero())
	assert.Empty(t, pending[0].LastError)
}

// --- CrawlService Tests ---

func TestCrawlService_EnqueueAndDispatch(t *testing.T) {
	db := setupTestDB(t)
	b := setupTestNATS(t)
	jobRepo := store.NewJobRepository(db)
	urlRepo := store.NewURLRepository(db)
	pageRepo := store.NewPageRepository(db, store.WithContentDir(filepath.Join(t.TempDir(), "data")))
	crawlSvc := services.NewCrawlService(jobRepo, urlRepo, pageRepo, nil, b)
	jobSvc := services.NewJobService(jobRepo, urlRepo, b)
	ctx := context.Background()

	job, err := jobSvc.CreateJob(ctx, "https://example.com", testConfig())
	require.NoError(t, err)
	require.NoError(t, jobSvc.StartJob(ctx, job.ID))

	// Enqueue seed.
	require.NoError(t, crawlSvc.EnqueueSeedURL(ctx, job))

	// Subscribe to capture dispatched tasks.
	var received []byte
	done := make(chan struct{})
	sub, err := b.Subscribe(broker.CrawlDispatchSubject(job.ID), func(_ string, data []byte) error {
		received = data
		close(done)
		return nil
	})
	require.NoError(t, err)
	defer sub.Unsubscribe()

	// Dispatch.
	n, err := crawlSvc.DispatchURLs(ctx, job.ID, job.Config, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	<-done
	assert.NotEmpty(t, received)
}

func TestCrawlService_DispatchURLsRateLimitsPerDomain(t *testing.T) {
	db := setupTestDB(t)
	b := setupTestNATS(t)
	jobRepo := store.NewJobRepository(db)
	urlRepo := store.NewURLRepository(db)
	pageRepo := store.NewPageRepository(db, store.WithContentDir(filepath.Join(t.TempDir(), "data")))
	crawlSvc := services.NewCrawlService(jobRepo, urlRepo, pageRepo, nil, b)
	jobSvc := services.NewJobService(jobRepo, urlRepo, b)
	ctx := context.Background()

	job, err := jobSvc.CreateJob(ctx, "https://example.com", valueobj.CrawlConfig{
		Scope:      valueobj.ScopeSameDomain,
		MaxDepth:   3,
		Extraction: valueobj.ExtractionStandard,
		UserAgent:  "TestBot/1.0",
		RateLimit:  valueobj.Duration{Duration: 20 * time.Millisecond},
	})
	require.NoError(t, err)
	require.NoError(t, jobSvc.StartJob(ctx, job.ID))

	require.NoError(t, urlRepo.Enqueue(ctx, entities.NewCrawlURL(job.ID, "https://example.com", "https://example.com/", "hash1", 0, "")))
	require.NoError(t, urlRepo.Enqueue(ctx, entities.NewCrawlURL(job.ID, "https://example.com/about", "https://example.com/about", "hash2", 1, "")))
	require.NoError(t, urlRepo.Enqueue(ctx, entities.NewCrawlURL(job.ID, "https://example.com/contact", "https://example.com/contact", "hash3", 1, "")))

	n, err := crawlSvc.DispatchURLs(ctx, job.ID, job.Config, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	n, err = crawlSvc.DispatchURLs(ctx, job.ID, job.Config, 1)
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	claimed, err := urlRepo.FindByHash(ctx, job.ID, "hash1")
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.NoError(t, claimed.MarkDone())
	require.NoError(t, urlRepo.Complete(ctx, claimed))

	time.Sleep(25 * time.Millisecond)

	n, err = crawlSvc.DispatchURLs(ctx, job.ID, job.Config, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

func TestCrawlService_DispatchURLs_AllowsBurstForSameDomainUpToAvailableCapacity(t *testing.T) {
	db := setupTestDB(t)
	b := setupTestNATS(t)
	jobRepo := store.NewJobRepository(db)
	urlRepo := store.NewURLRepository(db)
	pageRepo := store.NewPageRepository(db, store.WithContentDir(filepath.Join(t.TempDir(), "data")))
	crawlSvc := services.NewCrawlService(jobRepo, urlRepo, pageRepo, nil, b)
	jobSvc := services.NewJobService(jobRepo, urlRepo, b)
	ctx := context.Background()

	job, err := jobSvc.CreateJob(ctx, "https://example.com", valueobj.CrawlConfig{
		Scope:      valueobj.ScopeSameDomain,
		MaxDepth:   3,
		Extraction: valueobj.ExtractionStandard,
		UserAgent:  "TestBot/1.0",
	})
	require.NoError(t, err)
	require.NoError(t, jobSvc.StartJob(ctx, job.ID))

	require.NoError(t, urlRepo.Enqueue(ctx, entities.NewCrawlURL(job.ID, "https://example.com", "https://example.com/", "hash1", 0, "")))
	require.NoError(t, urlRepo.Enqueue(ctx, entities.NewCrawlURL(job.ID, "https://example.com/about", "https://example.com/about", "hash2", 1, "")))
	require.NoError(t, urlRepo.Enqueue(ctx, entities.NewCrawlURL(job.ID, "https://example.com/contact", "https://example.com/contact", "hash3", 1, "")))

	n, err := crawlSvc.DispatchURLs(ctx, job.ID, job.Config, 3)
	require.NoError(t, err)
	assert.Equal(t, 3, n)

	counts, err := urlRepo.CountByStatus(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, 3, counts[entities.URLStatusCrawling])
	assert.Equal(t, 0, counts[entities.URLStatusPending])
}

func TestCrawlService_DispatchURLs_DoesNotOverclaimBeyondAvailableCapacity(t *testing.T) {
	db := setupTestDB(t)
	b := setupTestNATS(t)
	jobRepo := store.NewJobRepository(db)
	urlRepo := store.NewURLRepository(db)
	pageRepo := store.NewPageRepository(db, store.WithContentDir(filepath.Join(t.TempDir(), "data")))
	crawlSvc := services.NewCrawlService(jobRepo, urlRepo, pageRepo, nil, b)
	jobSvc := services.NewJobService(jobRepo, urlRepo, b)
	ctx := context.Background()

	job, err := jobSvc.CreateJob(ctx, "https://example.com", valueobj.CrawlConfig{
		Scope:      valueobj.ScopeIncludeSubdomain,
		MaxDepth:   3,
		Extraction: valueobj.ExtractionStandard,
		UserAgent:  "TestBot/1.0",
	})
	require.NoError(t, err)
	require.NoError(t, jobSvc.StartJob(ctx, job.ID))

	require.NoError(t, urlRepo.Enqueue(ctx, entities.NewCrawlURL(job.ID, "https://example.com", "https://example.com/", "hash1", 0, "")))
	require.NoError(t, urlRepo.Enqueue(ctx, entities.NewCrawlURL(job.ID, "https://a.example.com", "https://a.example.com/", "hash2", 1, "")))
	require.NoError(t, urlRepo.Enqueue(ctx, entities.NewCrawlURL(job.ID, "https://b.example.com", "https://b.example.com/", "hash3", 1, "")))

	claimed, err := urlRepo.Claim(ctx, job.ID, 2)
	require.NoError(t, err)
	require.Len(t, claimed, 2)

	n, err := crawlSvc.DispatchURLs(ctx, job.ID, job.Config, 2)
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	counts, err := urlRepo.CountByStatus(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, counts[entities.URLStatusCrawling])
	assert.Equal(t, 1, counts[entities.URLStatusPending])
}

func TestCrawlService_ProcessResult(t *testing.T) {
	db := setupTestDB(t)
	b := setupTestNATS(t)
	jobRepo := store.NewJobRepository(db)
	urlRepo := store.NewURLRepository(db)
	pageRepo := store.NewPageRepository(db, store.WithContentDir(filepath.Join(t.TempDir(), "data")))
	crawlSvc := services.NewCrawlService(jobRepo, urlRepo, pageRepo, nil, b)
	jobSvc := services.NewJobService(jobRepo, urlRepo, b)
	ctx := context.Background()

	job, err := jobSvc.CreateJob(ctx, "https://example.com", testConfig())
	require.NoError(t, err)
	require.NoError(t, jobSvc.StartJob(ctx, job.ID))
	require.NoError(t, crawlSvc.EnqueueSeedURL(ctx, job))

	// Claim URL.
	claimed, err := urlRepo.Claim(ctx, job.ID, 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	// Simulate crawl result.
	page := entities.NewPage(claimed[0].ID, job.ID)
	page.HTTPStatus = 200
	page.ContentType = "text/html"
	page.RawContent = []byte("<html><body>ok</body></html>")
	page.FetchedAt = time.Now().UTC()

	result := &entities.CrawlResult{
		URL:     claimed[0],
		Page:    page,
		Success: true,
		DiscoveredURLs: []entities.DiscoveredLink{
			{RawURL: "/about", Normalized: "https://example.com/about", URLHash: "h1", IsExternal: false},
			{RawURL: "https://external.com", Normalized: "https://external.com", URLHash: "h2", IsExternal: true},
		},
	}

	err = crawlSvc.ProcessResult(ctx, result)
	require.NoError(t, err)

	// Verify URL marked done.
	assert.Equal(t, entities.URLStatusDone, claimed[0].Status)

	// Verify page stored.
	storedPage, err := pageRepo.FindByURLID(ctx, claimed[0].ID)
	require.NoError(t, err)
	assert.NotNil(t, storedPage)

	// Verify internal link enqueued (external filtered by same-domain scope).
	pending, err := urlRepo.FindPending(ctx, job.ID, 100)
	require.NoError(t, err)
	assert.Len(t, pending, 1) // only /about, not external
}

func TestCrawlService_CheckCompletion(t *testing.T) {
	db := setupTestDB(t)
	b := setupTestNATS(t)
	jobRepo := store.NewJobRepository(db)
	urlRepo := store.NewURLRepository(db)
	pageRepo := store.NewPageRepository(db, store.WithContentDir(filepath.Join(t.TempDir(), "data")))
	crawlSvc := services.NewCrawlService(jobRepo, urlRepo, pageRepo, nil, b)
	ctx := context.Background()

	job := entities.NewJob("https://example.com", testConfig())
	require.NoError(t, jobRepo.Create(ctx, job))

	// No URLs = complete.
	done, err := crawlSvc.CheckCompletion(ctx, job.ID)
	require.NoError(t, err)
	assert.True(t, done)
}

// --- WorkerService Tests ---

func TestWorkerService_ProcessTask(t *testing.T) {
	b := setupTestNATS(t)
	mockHTTPFetcher := &mockFetcher{
		responses: map[string]*ports.FetchResponse{
			"https://example.com/page": {
				StatusCode:  200,
				ContentType: "text/html",
				Headers:     http.Header{"Content-Type": {"text/html"}},
				Body:        io.NopCloser(strings.NewReader("<html><head><title>Test</title></head><body><a href=\"/link1\">Link</a></body></html>")),
				URL:         "https://example.com/page",
			},
		},
	}

	checker := robots.NewChecker(mockHTTPFetcher, "TestBot", time.Hour)
	rl := fetcher.NewAdaptiveRateLimiter(10 * time.Millisecond)

	worker := services.NewWorkerService(mockHTTPFetcher, nil, checker, rl, nil, b, nil, t.TempDir(), 2, time.Minute, nil)

	task := services.CrawlTask{
		JobID:    "job1",
		URLID:    "url1",
		URL:      "https://example.com/page",
		Depth:    0,
		Config:   testConfig(),
		SeedHost: "example.com",
	}

	result := worker.ProcessTask(context.Background(), task)
	assert.True(t, result.Success)
	assert.NotNil(t, result.Page)
	assert.Equal(t, "Test", result.Page.Title)
	assert.Equal(t, 200, result.Page.HTTPStatus)
}

func TestWorkerService_ProcessTask_StagesBinaryContent(t *testing.T) {
	b := setupTestNATS(t)
	mockHTTPFetcher := &mockFetcher{
		responses: map[string]*ports.FetchResponse{
			"https://example.com/video.mp4": {
				StatusCode:  200,
				ContentType: "video/mp4",
				Headers:     http.Header{"Content-Type": {"video/mp4"}},
				Body:        io.NopCloser(strings.NewReader("binary-video-data")),
				URL:         "https://example.com/video.mp4",
			},
		},
	}

	checker := robots.NewChecker(mockHTTPFetcher, "TestBot", time.Hour)
	rl := fetcher.NewAdaptiveRateLimiter(10 * time.Millisecond)
	worker := services.NewWorkerService(mockHTTPFetcher, nil, checker, rl, nil, b, nil, t.TempDir(), 2, time.Minute, nil)

	result := worker.ProcessTask(context.Background(), services.CrawlTask{
		JobID:    "job1",
		URLID:    "url1",
		URL:      "https://example.com/video.mp4",
		Depth:    0,
		Config:   testConfig(),
		SeedHost: "example.com",
	})

	require.True(t, result.Success)
	require.NotNil(t, result.Page)
	assert.NotEmpty(t, result.Page.ContentPath)
	assert.Equal(t, int64(len("binary-video-data")), result.Page.ContentSize)
	assert.Nil(t, result.Page.RawContent)
	_, err := os.Stat(result.Page.ContentPath)
	require.NoError(t, err)
}

func TestWorkerService_ProcessTask_StagesHTMLContentAndClearsTransportBody(t *testing.T) {
	b := setupTestNATS(t)
	mockHTTPFetcher := &mockFetcher{
		responses: map[string]*ports.FetchResponse{
			"https://example.com/page": {
				StatusCode:  200,
				ContentType: "text/html; charset=utf-8",
				Headers:     http.Header{"Content-Type": {"text/html; charset=utf-8"}},
				Body:        io.NopCloser(strings.NewReader("<html><head><title>Test</title></head><body><a href=\"/about\">About</a><p>Hello</p></body></html>")),
				URL:         "https://example.com/page",
			},
		},
	}

	checker := robots.NewChecker(mockHTTPFetcher, "TestBot", time.Hour)
	rl := fetcher.NewAdaptiveRateLimiter(10 * time.Millisecond)
	worker := services.NewWorkerService(mockHTTPFetcher, nil, checker, rl, nil, b, nil, t.TempDir(), 2, time.Minute, nil)

	result := worker.ProcessTask(context.Background(), services.CrawlTask{
		JobID:    "job1",
		URLID:    "url1",
		URL:      "https://example.com/page",
		Depth:    0,
		Config:   testConfig(),
		SeedHost: "example.com",
	})

	require.True(t, result.Success)
	require.NotNil(t, result.Page)
	assert.NotEmpty(t, result.Page.ContentPath)
	assert.Empty(t, result.Page.HTMLBody)
	assert.Nil(t, result.Page.RawContent)
	assert.Contains(t, result.Page.TextContent, "Hello")
	_, err := os.Stat(result.Page.ContentPath)
	require.NoError(t, err)
}

func TestWorkerService_ProcessTask_UploadsTransferObject(t *testing.T) {
	b := setupTestNATS(t)
	mockHTTPFetcher := &mockFetcher{
		responses: map[string]*ports.FetchResponse{
			"https://example.com/file.pdf": {
				StatusCode:  200,
				ContentType: "application/pdf",
				Headers:     http.Header{"Content-Type": {"application/pdf"}},
				Body:        io.NopCloser(strings.NewReader("pdf-binary-data")),
				URL:         "https://example.com/file.pdf",
			},
		},
	}

	store := newMockObjectStore()
	checker := robots.NewChecker(mockHTTPFetcher, "TestBot", time.Hour)
	rl := fetcher.NewAdaptiveRateLimiter(10 * time.Millisecond)
	worker := services.NewWorkerService(mockHTTPFetcher, nil, checker, rl, nil, b, store, t.TempDir(), 2, time.Minute, nil)

	result := worker.ProcessTask(context.Background(), services.CrawlTask{
		JobID:    "job1",
		URLID:    "url1",
		URL:      "https://example.com/file.pdf",
		Depth:    0,
		Config:   testConfig(),
		SeedHost: "example.com",
	})

	require.True(t, result.Success)
	require.NotNil(t, result.Page)
	assert.Equal(t, "job1/url1.pdf", result.Page.TransferObject)
	assert.Equal(t, int64(len("pdf-binary-data")), result.Page.ContentSize)
	assert.Nil(t, result.Page.RawContent)
	assert.Equal(t, []byte("pdf-binary-data"), store.objects["job1/url1.pdf"])
}

func TestCrawlService_ProcessResult_RestoresPageLinksFromDiscoveredURLs(t *testing.T) {
	db := setupTestDB(t)
	b := setupTestNATS(t)
	jobRepo := store.NewJobRepository(db)
	urlRepo := store.NewURLRepository(db)
	pageRepo := store.NewPageRepository(db, store.WithContentDir(filepath.Join(t.TempDir(), "data")))
	crawlSvc := services.NewCrawlService(jobRepo, urlRepo, pageRepo, nil, b)
	jobSvc := services.NewJobService(jobRepo, urlRepo, b)
	ctx := context.Background()

	job, err := jobSvc.CreateJob(ctx, "https://example.com", testConfig())
	require.NoError(t, err)
	require.NoError(t, jobSvc.StartJob(ctx, job.ID))
	require.NoError(t, crawlSvc.EnqueueSeedURL(ctx, job))

	claimed, err := urlRepo.Claim(ctx, job.ID, 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	page := entities.NewPage(claimed[0].ID, job.ID)
	page.HTTPStatus = 200
	page.ContentType = "text/html; charset=utf-8"
	page.ContentPath = filepath.Join(t.TempDir(), "page.html")
	page.TextContent = "ok"
	require.NoError(t, os.WriteFile(page.ContentPath, []byte("<html><body>ok</body></html>"), 0o644))

	discovered := []entities.DiscoveredLink{
		{RawURL: "/about", Normalized: "https://example.com/about", URLHash: "h1", IsExternal: false},
	}
	result := &entities.CrawlResult{
		URL:            claimed[0],
		Page:           page,
		Success:        true,
		DiscoveredURLs: discovered,
	}

	err = crawlSvc.ProcessResult(ctx, result)
	require.NoError(t, err)

	storedPage, err := pageRepo.FindByURLID(ctx, claimed[0].ID)
	require.NoError(t, err)
	require.NotNil(t, storedPage)
	assert.Len(t, storedPage.Links, 1)
	assert.Equal(t, "https://example.com/about", storedPage.Links[0].Normalized)
}

func TestCrawlService_ProcessResult_DownloadsTransferObjectAndDeletesIt(t *testing.T) {
	db := setupTestDB(t)
	b := setupTestNATS(t)
	jobRepo := store.NewJobRepository(db)
	urlRepo := store.NewURLRepository(db)
	pageRepo := store.NewPageRepository(db, store.WithContentDir(filepath.Join(t.TempDir(), "data")))
	objectStore := newMockObjectStore()
	crawlSvc := services.NewCrawlService(jobRepo, urlRepo, pageRepo, objectStore, b)
	jobSvc := services.NewJobService(jobRepo, urlRepo, b)
	ctx := context.Background()

	job, err := jobSvc.CreateJob(ctx, "https://example.com", testConfig())
	require.NoError(t, err)
	require.NoError(t, jobSvc.StartJob(ctx, job.ID))
	require.NoError(t, crawlSvc.EnqueueSeedURL(ctx, job))

	claimed, err := urlRepo.Claim(ctx, job.ID, 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	objectStore.objects["job1/url1.html"] = []byte("<html><body>hello</body></html>")
	page := entities.NewPage(claimed[0].ID, job.ID)
	page.HTTPStatus = 200
	page.ContentType = "text/html; charset=utf-8"
	page.TransferObject = "job1/url1.html"
	page.TextContent = "hello"
	result := &entities.CrawlResult{
		URL:            claimed[0],
		Page:           page,
		Success:        true,
		DiscoveredURLs: []entities.DiscoveredLink{{RawURL: "/about", Normalized: "https://example.com/about", URLHash: "h1"}},
	}

	err = crawlSvc.ProcessResult(ctx, result)
	require.NoError(t, err)

	storedPage, err := pageRepo.FindByURLID(ctx, claimed[0].ID)
	require.NoError(t, err)
	require.NotNil(t, storedPage)
	assert.NotEmpty(t, storedPage.ContentPath)
	_, ok := objectStore.objects["job1/url1.html"]
	assert.False(t, ok)
}

// mockFetcher for worker tests.
type mockFetcher struct {
	responses map[string]*ports.FetchResponse
}

func (m *mockFetcher) Fetch(_ context.Context, url string) (*ports.FetchResponse, error) {
	if resp, ok := m.responses[url]; ok {
		return resp, nil
	}
	return &ports.FetchResponse{
		StatusCode: 404,
		Body:       io.NopCloser(strings.NewReader("")),
		Headers:    http.Header{},
	}, nil
}

type mockObjectStore struct {
	objects map[string][]byte
}

func newMockObjectStore() *mockObjectStore {
	return &mockObjectStore{objects: make(map[string][]byte)}
}

func (m *mockObjectStore) PutBytes(_ context.Context, name string, data []byte) (string, error) {
	m.objects[name] = append([]byte(nil), data...)
	return name, nil
}

func (m *mockObjectStore) GetBytes(_ context.Context, name string) ([]byte, error) {
	data, ok := m.objects[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), data...), nil
}

func (m *mockObjectStore) Delete(_ context.Context, name string) error {
	delete(m.objects, name)
	return nil
}
