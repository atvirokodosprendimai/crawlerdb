package extraction_test

import (
	"net/http"
	"testing"

	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/http/extraction"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/valueobj"
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
	assert.Empty(t, page.TextContent, "minimal should not include text")
	assert.Empty(t, page.StructuredData, "minimal should not include structured data")
}

func TestExtract_Standard(t *testing.T) {
	ext := extraction.NewExtractor()
	resp := makeResp()
	body := []byte(fullHTML)

	profile := valueobj.ExtractionProfile{Level: valueobj.ExtractionStandard}
	page := ext.Extract(resp, body, "url1", "job1", "https://example.com/page", "example.com", profile, 0)

	assert.NotEmpty(t, page.HTMLBody, "standard should include HTML body")
	assert.Empty(t, page.TextContent, "standard should not include text")
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
