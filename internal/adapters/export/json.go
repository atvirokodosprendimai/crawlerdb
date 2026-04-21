package export

import (
	"context"
	"encoding/json"
	"io"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
)

// JSONExporter exports crawled pages as JSON.
type JSONExporter struct {
	pageRepo ports.PageRepository
}

// NewJSONExporter creates a JSON exporter.
func NewJSONExporter(pageRepo ports.PageRepository) *JSONExporter {
	return &JSONExporter{pageRepo: pageRepo}
}

// Format returns the export format.
func (e *JSONExporter) Format() ports.ExportFormat {
	return ports.ExportJSON
}

// Export writes pages for the given job as a JSON array.
func (e *JSONExporter) Export(ctx context.Context, filter ports.ExportFilter, w io.Writer) error {
	pages, err := e.pageRepo.FindByJobID(ctx, filter.JobID, 10000, 0)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(pages)
}
