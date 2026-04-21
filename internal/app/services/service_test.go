package services_test

import (
	"context"
	"io"
	"net/http"
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

// --- CrawlService Tests ---

func TestCrawlService_EnqueueAndDispatch(t *testing.T) {
	db := setupTestDB(t)
	b := setupTestNATS(t)
	jobRepo := store.NewJobRepository(db)
	urlRepo := store.NewURLRepository(db)
	pageRepo := store.NewPageRepository(db, store.WithContentDir(filepath.Join(t.TempDir(), "data")))
	crawlSvc := services.NewCrawlService(jobRepo, urlRepo, pageRepo, b)
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
	crawlSvc := services.NewCrawlService(jobRepo, urlRepo, pageRepo, b)
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

	n, err := crawlSvc.DispatchURLs(ctx, job.ID, job.Config, 10)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	n, err = crawlSvc.DispatchURLs(ctx, job.ID, job.Config, 10)
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	time.Sleep(25 * time.Millisecond)

	n, err = crawlSvc.DispatchURLs(ctx, job.ID, job.Config, 10)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

func TestCrawlService_ProcessResult(t *testing.T) {
	db := setupTestDB(t)
	b := setupTestNATS(t)
	jobRepo := store.NewJobRepository(db)
	urlRepo := store.NewURLRepository(db)
	pageRepo := store.NewPageRepository(db, store.WithContentDir(filepath.Join(t.TempDir(), "data")))
	crawlSvc := services.NewCrawlService(jobRepo, urlRepo, pageRepo, b)
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
	crawlSvc := services.NewCrawlService(jobRepo, urlRepo, pageRepo, b)
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

	worker := services.NewWorkerService(mockHTTPFetcher, checker, rl, nil, b, 2, nil)

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
