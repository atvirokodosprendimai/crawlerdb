package extraction

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"strings"
	"time"

	fetcher "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/http"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/valueobj"
	"github.com/ledongthuc/pdf"
)

// Extractor processes an HTTP response into a Page entity.
type Extractor struct {
	linkExtractor *fetcher.LinkExtractor
}

// NewExtractor creates a new content extractor.
func NewExtractor() *Extractor {
	return &Extractor{
		linkExtractor: fetcher.NewLinkExtractor(),
	}
}

// Extract processes a fetch response into a Page based on the extraction profile.
func (e *Extractor) Extract(
	resp *ports.FetchResponse,
	body []byte,
	urlID, jobID, pageURL, seedHost string,
	profile valueobj.ExtractionProfile,
	duration time.Duration,
) *entities.Page {
	page := entities.NewPage(urlID, jobID)
	page.HTTPStatus = resp.StatusCode
	page.ContentType = resp.ContentType
	page.FetchDuration = duration
	page.FetchedAt = time.Now().UTC()
	page.RawContent = append([]byte(nil), body...)

	// Extract headers.
	headers := make(map[string]string)
	for k, v := range resp.Headers {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	page.Headers = headers

	if isHTMLContentType(resp.ContentType) {
		doc := e.linkExtractor.ExtractDocument(strings.NewReader(string(body)), pageURL, seedHost)
		page.Title = doc.Title
		page.MetaTags = doc.Meta
		page.Links = doc.Links

		// Standard: include HTML body in-memory for downstream storage.
		if profile.IncludesHTML() {
			page.HTMLBody = string(body)
		}

		page.TextContent = doc.Text

		if profile.IncludesStructuredData() {
			page.StructuredData = extractStructuredData(body)
		}
	} else {
		page.TextContent = ExtractSearchText(resp.ContentType, body)
	}

	return page
}

func ExtractSearchText(contentType string, body []byte) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	mediaType = strings.ToLower(mediaType)
	if strings.HasPrefix(mediaType, "text/") {
		return normalizeSearchText(string(body))
	}
	switch mediaType {
	case "application/pdf":
		return extractPDFSearchText(body)
	case "application/json", "application/xml", "application/javascript", "application/x-javascript", "application/csv":
		return normalizeSearchText(string(body))
	default:
		return ""
	}
}

func extractPDFSearchText(body []byte) string {
	reader, err := pdf.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return ""
	}
	textReader, err := reader.GetPlainText()
	if err != nil {
		return ""
	}
	text, err := io.ReadAll(textReader)
	if err != nil {
		return ""
	}
	return normalizeSearchText(string(text))
}

func normalizeSearchText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func isHTMLContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	switch strings.ToLower(mediaType) {
	case "text/html", "application/xhtml+xml":
		return true
	default:
		return false
	}
}

// extractStructuredData finds JSON-LD blocks in HTML.
func extractStructuredData(body []byte) []any {
	content := string(body)
	var results []any

	// Simple JSON-LD extraction: find <script type="application/ld+json">...</script>
	for {
		idx := strings.Index(content, `type="application/ld+json"`)
		if idx < 0 {
			idx = strings.Index(content, `type='application/ld+json'`)
		}
		if idx < 0 {
			break
		}

		// Find the > after the type attribute.
		start := strings.Index(content[idx:], ">")
		if start < 0 {
			break
		}
		start += idx + 1

		// Find closing </script>.
		end := strings.Index(content[start:], "</script>")
		if end < 0 {
			break
		}

		jsonStr := strings.TrimSpace(content[start : start+end])
		if jsonStr != "" {
			var data any
			if err := json.Unmarshal([]byte(jsonStr), &data); err == nil {
				results = append(results, data)
			}
		}

		content = content[start+end:]
	}

	return results
}

// ReadBody reads the full response body and returns it as bytes.
func ReadBody(body io.ReadCloser) ([]byte, error) {
	defer body.Close()
	return io.ReadAll(body)
}
