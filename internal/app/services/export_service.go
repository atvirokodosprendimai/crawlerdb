package services

import (
	"context"
	"fmt"
	"io"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
)

// ExportService manages data export operations.
type ExportService struct {
	exporters map[ports.ExportFormat]ports.Exporter
}

// NewExportService creates an export service with registered exporters.
func NewExportService(exporters ...ports.Exporter) *ExportService {
	m := make(map[ports.ExportFormat]ports.Exporter, len(exporters))
	for _, e := range exporters {
		m[e.Format()] = e
	}
	return &ExportService{exporters: m}
}

// Export writes data in the requested format.
func (s *ExportService) Export(ctx context.Context, format ports.ExportFormat, filter ports.ExportFilter, w io.Writer) error {
	exp, ok := s.exporters[format]
	if !ok {
		return fmt.Errorf("unsupported export format: %s", format)
	}
	return exp.Export(ctx, filter, w)
}

// SupportedFormats returns all registered export formats.
func (s *ExportService) SupportedFormats() []ports.ExportFormat {
	formats := make([]ports.ExportFormat, 0, len(s.exporters))
	for f := range s.exporters {
		formats = append(formats, f)
	}
	return formats
}
