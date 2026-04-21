package ports

import (
	"context"
	"io"
)

// ExportFormat defines the output format for data export.
type ExportFormat string

const (
	ExportJSON    ExportFormat = "json"
	ExportCSV     ExportFormat = "csv"
	ExportSQLite  ExportFormat = "sqlite"
	ExportSitemap ExportFormat = "sitemap"
)

// ExportFilter defines criteria for filtering exported data.
type ExportFilter struct {
	JobID    string   `json:"job_id"`
	Statuses []string `json:"statuses,omitempty"`
	Domains  []string `json:"domains,omitempty"`
}

// Exporter writes crawl data in a specific format.
type Exporter interface {
	Export(ctx context.Context, filter ExportFilter, w io.Writer) error
	Format() ExportFormat
}
