package gui_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	store "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/db/gorm"
	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/gui"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/valueobj"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupTestRouter(t *testing.T) (http.Handler, *gorm.DB) {
	t.Helper()
	db, err := store.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, store.Migrate(db))

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	return gui.NewRouter(db, nil, logger), db
}

func TestRouter_Health(t *testing.T) {
	router, _ := setupTestRouter(t)
	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ok", resp["status"])
}

func TestRouter_ListJobs_Empty(t *testing.T) {
	router, _ := setupTestRouter(t)
	req := httptest.NewRequest("GET", "/api/jobs", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRouter_GetJob_NotFound(t *testing.T) {
	router, _ := setupTestRouter(t)
	req := httptest.NewRequest("GET", "/api/jobs/nonexistent", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestRouter_CORS(t *testing.T) {
	router, _ := setupTestRouter(t)
	req := httptest.NewRequest("OPTIONS", "/api/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Contains(t, w.Header().Get("Access-Control-Allow-Methods"), "GET")
}

func TestRouter_Exceptions(t *testing.T) {
	router, db := setupTestRouter(t)
	ctx := t.Context()

	jobRepo := store.NewJobRepository(db)
	urlRepo := store.NewURLRepository(db)

	job := entities.NewJob("https://example.com", valueobj.CrawlConfig{
		Scope:      valueobj.ScopeSameDomain,
		MaxDepth:   3,
		Extraction: valueobj.ExtractionStandard,
	})
	require.NoError(t, jobRepo.Create(ctx, job))

	url1 := entities.NewCrawlURL(job.ID, "https://example.com/error", "https://example.com/error", "hash1", 1, "")
	url2 := entities.NewCrawlURL(job.ID, "https://example.com/blocked", "https://example.com/blocked", "hash2", 2, "")
	require.NoError(t, urlRepo.Enqueue(ctx, url1))
	require.NoError(t, urlRepo.Enqueue(ctx, url2))
	require.NoError(t, url1.Claim())
	require.NoError(t, url2.Claim())
	require.NoError(t, url1.MarkError())
	url1.LastError = "fetch timeout"
	require.NoError(t, urlRepo.Complete(ctx, url1))
	require.NoError(t, url2.MarkBlocked())
	url2.LastError = "blocked by robots.txt"
	require.NoError(t, urlRepo.Complete(ctx, url2))

	req := httptest.NewRequest("GET", "/api/jobs/"+job.ID+"/exceptions?limit=10&offset=0", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Items []entities.CrawlURL `json:"items"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Items, 2)
	assert.Equal(t, "blocked by robots.txt", resp.Items[0].LastError)
}

func TestRouter_SiteExplorerAndContent(t *testing.T) {
	router, db := setupTestRouter(t)
	ctx := t.Context()

	jobRepo := store.NewJobRepository(db)
	urlRepo := store.NewURLRepository(db)
	pageRepo := store.NewPageRepository(db, store.WithContentDir(filepath.Join(t.TempDir(), "data")))

	job := entities.NewJob("https://example.com", valueobj.CrawlConfig{
		Scope:      valueobj.ScopeSameDomain,
		MaxDepth:   3,
		Extraction: valueobj.ExtractionStandard,
	})
	require.NoError(t, jobRepo.Create(ctx, job))

	crawlURL := entities.NewCrawlURL(job.ID, "https://example.com/about", "https://example.com/about", "hash-about", 1, "https://example.com")
	require.NoError(t, urlRepo.Enqueue(ctx, crawlURL))
	require.NoError(t, crawlURL.Claim())
	require.NoError(t, crawlURL.MarkDone())
	require.NoError(t, urlRepo.Complete(ctx, crawlURL))

	page := entities.NewPage(crawlURL.ID, job.ID)
	page.HTTPStatus = http.StatusOK
	page.ContentType = "text/html; charset=utf-8"
	page.Title = "About"
	page.RawContent = []byte("<html><body>about</body></html>")
	page.FetchedAt = time.Now().UTC()
	require.NoError(t, pageRepo.Store(ctx, page))

	req := httptest.NewRequest("GET", "/api/jobs/"+job.ID+"/site?limit=10&offset=0&content=stored", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Items []struct {
			URLID       string `json:"url_id"`
			Normalized  string `json:"normalized"`
			Title       string `json:"title"`
			FileURL     string `json:"file_url"`
			ContentType string `json:"content_type"`
		} `json:"items"`
		Total int `json:"total"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Items, 1)
	assert.Equal(t, 1, resp.Total)
	assert.Equal(t, "https://example.com/about", resp.Items[0].Normalized)
	assert.Equal(t, "About", resp.Items[0].Title)
	assert.Contains(t, resp.Items[0].FileURL, "/api/jobs/"+job.ID+"/pages/"+page.ID+"/content")
	assert.Contains(t, resp.Items[0].ContentType, "text/html")

	contentReq := httptest.NewRequest("GET", resp.Items[0].FileURL, nil)
	contentW := httptest.NewRecorder()
	router.ServeHTTP(contentW, contentReq)

	assert.Equal(t, http.StatusOK, contentW.Code)
	assert.Contains(t, contentW.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, contentW.Body.String(), "about")
}

func TestRouter_JobSettingsPageAndUpdate(t *testing.T) {
	router, db := setupTestRouter(t)
	ctx := t.Context()

	jobRepo := store.NewJobRepository(db)
	job := entities.NewJob("https://example.com", valueobj.CrawlConfig{
		Scope:          valueobj.ScopeSameDomain,
		MaxDepth:       3,
		Extraction:     valueobj.ExtractionStandard,
		RateLimit:      valueobj.Duration{Duration: time.Second},
		RevisitTTL:     valueobj.Duration{Duration: 24 * time.Hour},
		MaxConcurrency: 1,
		UserAgent:      "CrawlerDB/1.0",
	})
	require.NoError(t, jobRepo.Create(ctx, job))

	getReq := httptest.NewRequest("GET", "/jobs/"+job.ID+"/settings", nil)
	getW := httptest.NewRecorder()
	router.ServeHTTP(getW, getReq)

	assert.Equal(t, http.StatusOK, getW.Code)
	assert.Contains(t, getW.Body.String(), "Edit Site Settings")
	assert.Contains(t, getW.Body.String(), job.SeedURL)

	body := strings.NewReader("seed_url=https%3A%2F%2Fexample.com%2Fupdated&scope=follow_externals&extraction=full&max_depth=9&external_depth=2&rate_limit=3s&revisit_ttl=48h&max_concurrency=4&antibot_mode=rotate&user_agent=TestBot%2F2.0")
	postReq := httptest.NewRequest("POST", "/jobs/"+job.ID+"/settings", body)
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postW := httptest.NewRecorder()
	router.ServeHTTP(postW, postReq)

	assert.Equal(t, http.StatusSeeOther, postW.Code)
	assert.Equal(t, "/jobs/"+job.ID+"/settings?saved=1", postW.Header().Get("Location"))

	updated, err := jobRepo.FindByID(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, "https://example.com/updated", updated.SeedURL)
	assert.Equal(t, valueobj.ScopeFollowExternals, updated.Config.Scope)
	assert.Equal(t, valueobj.ExtractionFull, updated.Config.Extraction)
	assert.Equal(t, 9, updated.Config.MaxDepth)
	assert.Equal(t, 2, updated.Config.ExternalDepth)
	assert.Equal(t, 3*time.Second, updated.Config.RateLimit.Duration)
	assert.Equal(t, 48*time.Hour, updated.Config.RevisitTTL.Duration)
	assert.Equal(t, 4, updated.Config.MaxConcurrency)
	assert.Equal(t, valueobj.AntiBotRotate, updated.Config.AntiBotMode)
	assert.Equal(t, "TestBot/2.0", updated.Config.UserAgent)
}

func TestRouter_DeleteSiteMarksJobForSweep(t *testing.T) {
	router, db := setupTestRouter(t)
	ctx := t.Context()

	jobRepo := store.NewJobRepository(db)
	urlRepo := store.NewURLRepository(db)
	pageRepo := store.NewPageRepository(db, store.WithContentDir(filepath.Join(t.TempDir(), "data")))

	job := entities.NewJob("https://example.com", valueobj.CrawlConfig{
		Scope:      valueobj.ScopeSameDomain,
		MaxDepth:   3,
		Extraction: valueobj.ExtractionStandard,
	})
	require.NoError(t, jobRepo.Create(ctx, job))

	url := entities.NewCrawlURL(job.ID, "https://example.com/about", "https://example.com/about", "hash-delete", 1, "")
	require.NoError(t, urlRepo.Enqueue(ctx, url))
	require.NoError(t, url.Claim())
	require.NoError(t, url.MarkDone())
	require.NoError(t, urlRepo.Complete(ctx, url))

	page := entities.NewPage(url.ID, job.ID)
	page.ContentType = "text/html"
	page.RawContent = []byte("delete me")
	page.FetchedAt = time.Now().UTC()
	require.NoError(t, pageRepo.Store(ctx, page))

	storedPage, err := pageRepo.FindByURLID(ctx, url.ID)
	require.NoError(t, err)
	require.NotNil(t, storedPage)
	_, err = os.Stat(storedPage.ContentPath)
	require.NoError(t, err)

	deleteReq := httptest.NewRequest("POST", "/api/gui/jobs/"+job.ID+"/delete", strings.NewReader(""))
	deleteReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	deleteW := httptest.NewRecorder()
	router.ServeHTTP(deleteW, deleteReq)

	assert.Equal(t, http.StatusOK, deleteW.Code)

	foundJob, err := jobRepo.FindByID(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, foundJob)
	assert.Equal(t, entities.JobStatusStopped, foundJob.Status)
	assert.False(t, foundJob.DeleteMarkedAt.IsZero())

	pages, err := pageRepo.FindByJobID(ctx, job.ID, 10, 0)
	require.NoError(t, err)
	assert.Len(t, pages, 1)

	urls, err := urlRepo.FindByJobID(ctx, job.ID, 10, 0)
	require.NoError(t, err)
	assert.Len(t, urls, 1)

	_, err = os.Stat(storedPage.ContentPath)
	require.NoError(t, err)

	var antibotCount int64
	require.NoError(t, db.Table("antibot_events").Where("job_id = ?", job.ID).Count(&antibotCount).Error)
	assert.Equal(t, int64(0), antibotCount)
}
