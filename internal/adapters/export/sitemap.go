package export

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
)

// SitemapExporter exports crawled URLs as an XML sitemap.
type SitemapExporter struct {
	urlRepo ports.URLRepository
}

// NewSitemapExporter creates a sitemap exporter.
func NewSitemapExporter(urlRepo ports.URLRepository) *SitemapExporter {
	return &SitemapExporter{urlRepo: urlRepo}
}

// Format returns the export format.
func (e *SitemapExporter) Format() ports.ExportFormat {
	return ports.ExportSitemap
}

type urlSet struct {
	XMLName xml.Name  `xml:"urlset"`
	XMLNS   string    `xml:"xmlns,attr"`
	URLs    []siteURL `xml:"url"`
}

type siteURL struct {
	Loc string `xml:"loc"`
}

// Export writes URLs for the given job as XML sitemap.
func (e *SitemapExporter) Export(ctx context.Context, filter ports.ExportFilter, w io.Writer) error {
	urls, err := e.urlRepo.FindByJobID(ctx, filter.JobID, 100000, 0)
	if err != nil {
		return fmt.Errorf("fetch URLs: %w", err)
	}

	set := urlSet{
		XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9",
	}

	for _, u := range urls {
		if u.Status != entities.URLStatusDone {
			continue
		}
		set.URLs = append(set.URLs, siteURL{Loc: u.Normalized})
	}

	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}

	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	return enc.Encode(set)
}
