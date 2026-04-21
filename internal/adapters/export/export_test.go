package export_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"

	store "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/db/gorm"
	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/export"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/valueobj"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := store.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, store.Migrate(db))
	return db
}

func seedData(t *testing.T, db *gorm.DB) *entities.Job {
	t.Helper()
	ctx := context.Background()
	jobRepo := store.NewJobRepository(db)
	urlRepo := store.NewURLRepository(db)
	pageRepo := store.NewPageRepository(db)

	job := entities.NewJob("https://example.com", valueobj.CrawlConfig{
		Scope:      valueobj.ScopeSameDomain,
		MaxDepth:   3,
		Extraction: valueobj.ExtractionStandard,
		UserAgent:  "TestBot/1.0",
	})
	require.NoError(t, jobRepo.Create(ctx, job))

	url1 := entities.NewCrawlURL(job.ID, "https://example.com/", "https://example.com/", "hash1", 0, "")
	require.NoError(t, urlRepo.Enqueue(ctx, url1))
	require.NoError(t, url1.Claim())
	require.NoError(t, url1.MarkDone())
	require.NoError(t, urlRepo.Complete(ctx, url1))

	url2 := entities.NewCrawlURL(job.ID, "https://example.com/about", "https://example.com/about", "hash2", 1, "https://example.com/")
	require.NoError(t, urlRepo.Enqueue(ctx, url2))
	require.NoError(t, url2.Claim())
	require.NoError(t, url2.MarkDone())
	require.NoError(t, urlRepo.Complete(ctx, url2))

	page1 := entities.NewPage(url1.ID, job.ID)
	page1.HTTPStatus = 200
	page1.Title = "Home"
	page1.FetchedAt = time.Now().UTC()
	require.NoError(t, pageRepo.Store(ctx, page1))

	page2 := entities.NewPage(url2.ID, job.ID)
	page2.HTTPStatus = 200
	page2.Title = "About"
	page2.FetchedAt = time.Now().UTC()
	require.NoError(t, pageRepo.Store(ctx, page2))

	return job
}

func TestJSONExporter(t *testing.T) {
	db := setupDB(t)
	job := seedData(t, db)

	pageRepo := store.NewPageRepository(db)
	exp := export.NewJSONExporter(pageRepo)
	assert.Equal(t, ports.ExportJSON, exp.Format())

	var buf bytes.Buffer
	err := exp.Export(context.Background(), ports.ExportFilter{JobID: job.ID}, &buf)
	require.NoError(t, err)

	var pages []map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &pages))
	assert.Len(t, pages, 2)
}

func TestCSVExporter(t *testing.T) {
	db := setupDB(t)
	job := seedData(t, db)

	pageRepo := store.NewPageRepository(db)
	urlRepo := store.NewURLRepository(db)
	exp := export.NewCSVExporter(pageRepo, urlRepo)
	assert.Equal(t, ports.ExportCSV, exp.Format())

	var buf bytes.Buffer
	err := exp.Export(context.Background(), ports.ExportFilter{JobID: job.ID}, &buf)
	require.NoError(t, err)

	r := csv.NewReader(strings.NewReader(buf.String()))
	records, err := r.ReadAll()
	require.NoError(t, err)
	// Header + 2 data rows.
	assert.Len(t, records, 3)
	assert.Equal(t, "url", records[0][0])
}

func TestSitemapExporter(t *testing.T) {
	db := setupDB(t)
	job := seedData(t, db)

	urlRepo := store.NewURLRepository(db)
	exp := export.NewSitemapExporter(urlRepo)
	assert.Equal(t, ports.ExportSitemap, exp.Format())

	var buf bytes.Buffer
	err := exp.Export(context.Background(), ports.ExportFilter{JobID: job.ID}, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "<?xml")
	assert.Contains(t, output, "<urlset")
	assert.Contains(t, output, "https://example.com/")
	assert.Contains(t, output, "https://example.com/about")
}
