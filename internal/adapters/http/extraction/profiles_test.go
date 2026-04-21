package extraction_test

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/http/extraction"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/valueobj"
	"github.com/jung-kurt/gofpdf"
	"github.com/stretchr/testify/assert"
)

const fullHTML = `<!DOCTYPE html>
<html>
<head>
  <title>Full Test</title>
  <meta name="description" content="Full description">
  <script type="application/ld+json">{"@type":"WebPage","name":"Test"}</script>
</head>
<body>
  <h1>Main Content</h1>
  <p>Paragraph text.</p>
  <a href="/link1">Link 1</a>
  <a href="https://external.com">External</a>
</body>
</html>`

func makeResp() *ports.FetchResponse {
	return &ports.FetchResponse{
		StatusCode:  200,
		ContentType: "text/html; charset=utf-8",
		Headers:     http.Header{"Content-Type": {"text/html; charset=utf-8"}, "Server": {"nginx"}},
		URL:         "https://example.com/page",
	}
}

func TestExtract_Minimal(t *testing.T) {
	ext := extraction.NewExtractor()
	resp := makeResp()
	body := []byte(fullHTML)

	profile := valueobj.ExtractionProfile{Level: valueobj.ExtractionMinimal}
	page := ext.Extract(resp, body, "url1", "job1", "https://example.com/page", "example.com", profile, 0)

	assert.Equal(t, 200, page.HTTPStatus)
	assert.Equal(t, "Full Test", page.Title)
	assert.Equal(t, "Full description", page.MetaTags["description"])
	assert.NotEmpty(t, page.Links)
	assert.Empty(t, page.HTMLBody, "minimal should not include HTML body")
	assert.Contains(t, page.TextContent, "Main Content")
	assert.Empty(t, page.StructuredData, "minimal should not include structured data")
}

func TestExtract_Standard(t *testing.T) {
	ext := extraction.NewExtractor()
	resp := makeResp()
	body := []byte(fullHTML)

	profile := valueobj.ExtractionProfile{Level: valueobj.ExtractionStandard}
	page := ext.Extract(resp, body, "url1", "job1", "https://example.com/page", "example.com", profile, 0)

	assert.NotEmpty(t, page.HTMLBody, "standard should include HTML body")
	assert.Contains(t, page.TextContent, "Main Content")
}

func TestExtract_Full(t *testing.T) {
	ext := extraction.NewExtractor()
	resp := makeResp()
	body := []byte(fullHTML)

	profile := valueobj.ExtractionProfile{Level: valueobj.ExtractionFull}
	page := ext.Extract(resp, body, "url1", "job1", "https://example.com/page", "example.com", profile, 0)

	assert.NotEmpty(t, page.HTMLBody, "full should include HTML body")
	assert.Contains(t, page.TextContent, "Main Content")
	assert.Contains(t, page.TextContent, "Paragraph text")
	assert.Len(t, page.StructuredData, 1)
}

func TestExtract_LinksClassification(t *testing.T) {
	ext := extraction.NewExtractor()
	resp := makeResp()
	body := []byte(fullHTML)

	profile := valueobj.ExtractionProfile{Level: valueobj.ExtractionMinimal}
	page := ext.Extract(resp, body, "url1", "job1", "https://example.com/page", "example.com", profile, 0)

	var internal, external int
	for _, l := range page.Links {
		if l.IsExternal {
			external++
		} else {
			internal++
		}
	}
	assert.Equal(t, 1, internal)
	assert.Equal(t, 1, external)
}

func TestExtract_NonHTMLPreservesRawContentWithoutHTMLExtraction(t *testing.T) {
	ext := extraction.NewExtractor()
	resp := &ports.FetchResponse{
		StatusCode:  200,
		ContentType: "application/pdf",
		Headers:     http.Header{"Content-Type": {"application/pdf"}},
		URL:         "https://example.com/file.pdf",
	}
	body := []byte("%PDF-1.4 test")

	profile := valueobj.ExtractionProfile{Level: valueobj.ExtractionFull}
	page := ext.Extract(resp, body, "url1", "job1", "https://example.com/file.pdf", "example.com", profile, 0)

	assert.Equal(t, body, page.RawContent)
	assert.Empty(t, page.Title)
	assert.Empty(t, page.MetaTags)
	assert.Empty(t, page.Links)
	assert.Empty(t, page.HTMLBody)
	assert.Empty(t, page.TextContent)
}

func TestExtract_PDFStoresSearchableText(t *testing.T) {
	ext := extraction.NewExtractor()
	resp := &ports.FetchResponse{
		StatusCode:  200,
		ContentType: "application/pdf",
		Headers:     http.Header{"Content-Type": {"application/pdf"}},
		URL:         "https://example.com/file.pdf",
	}

	pdfDoc := gofpdf.New("P", "mm", "A4", "")
	pdfDoc.AddPage()
	pdfDoc.SetFont("Arial", "", 12)
	pdfDoc.Cell(40, 10, "Invoice 123")
	var buf bytes.Buffer
	assert.NoError(t, pdfDoc.Output(&buf))

	page := ext.Extract(resp, buf.Bytes(), "url1", "job1", "https://example.com/file.pdf", "example.com", valueobj.ExtractionProfile{Level: valueobj.ExtractionMinimal}, 0)

	assert.Contains(t, page.TextContent, "Invoice 123")
	assert.Equal(t, buf.Bytes(), page.RawContent)
}

func TestExtract_NonHTMLTextStoresSearchableText(t *testing.T) {
	ext := extraction.NewExtractor()
	resp := &ports.FetchResponse{
		StatusCode:  200,
		ContentType: "text/csv; charset=utf-8",
		Headers:     http.Header{"Content-Type": {"text/csv; charset=utf-8"}},
		URL:         "https://example.com/file.csv",
	}
	body := []byte("name,email\nJohn,john@example.com\n")

	page := ext.Extract(resp, body, "url1", "job1", "https://example.com/file.csv", "example.com", valueobj.ExtractionProfile{Level: valueobj.ExtractionMinimal}, 0)

	assert.Equal(t, "name,email John,john@example.com", page.TextContent)
}
