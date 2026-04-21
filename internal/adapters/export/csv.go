package export

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
)

// CSVExporter exports crawled data as CSV.
type CSVExporter struct {
	pageRepo ports.PageRepository
	urlRepo  ports.URLRepository
}

// NewCSVExporter creates a CSV exporter.
func NewCSVExporter(pageRepo ports.PageRepository, urlRepo ports.URLRepository) *CSVExporter {
	return &CSVExporter{pageRepo: pageRepo, urlRepo: urlRepo}
}

// Format returns the export format.
func (e *CSVExporter) Format() ports.ExportFormat {
	return ports.ExportCSV
}

// Export writes pages for the given job as CSV.
func (e *CSVExporter) Export(ctx context.Context, filter ports.ExportFilter, w io.Writer) error {
	pages, err := e.pageRepo.FindByJobID(ctx, filter.JobID, 10000, 0)
	if err != nil {
		return fmt.Errorf("fetch pages: %w", err)
	}

	cw := csv.NewWriter(w)
	defer cw.Flush()

	// Header.
	if err := cw.Write([]string{
		"url", "title", "http_status", "content_type", "links_count", "fetched_at",
	}); err != nil {
		return err
	}

	// Build URL lookup map.
	urls, _ := e.urlRepo.FindByJobID(ctx, filter.JobID, 100000, 0)
	urlMap := make(map[string]string, len(urls))
	for _, u := range urls {
		urlMap[u.ID] = u.Normalized
	}

	// Data rows.
	for _, p := range pages {
		pageURL := urlMap[p.URLID]
		if pageURL == "" {
			pageURL = p.URLID
		}

		if err := cw.Write([]string{
			pageURL,
			p.Title,
			strconv.Itoa(p.HTTPStatus),
			p.ContentType,
			strconv.Itoa(len(p.Links)),
			p.FetchedAt.Format("2006-01-02T15:04:05Z"),
		}); err != nil {
			return err
		}
	}

	return cw.Error()
}
