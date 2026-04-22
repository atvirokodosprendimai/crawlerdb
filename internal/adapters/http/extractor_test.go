package fetcher_test

import (
	"strings"
	"testing"

	fetcher "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/http"
	"github.com/stretchr/testify/assert"
)

const testHTML = `<!DOCTYPE html>
<html>
<head>
  <title>Test Page</title>
  <meta name="description" content="A test page">
  <meta property="og:title" content="OG Title">
  <link rel="stylesheet" href="/styles.css">
  <link rel="alternate" type="application/xml" href="/feed.xml">
</head>
<body>
  <h1>Hello World</h1>
  <p>Some text here.</p>
  <a href="/about">About</a>
  <a href="/contact" rel="nofollow">Contact Us</a>
  <a href="https://external.com/page">External</a>
  <a href="https://example.com/internal">Internal</a>
  <a href="javascript:void(0)">JS Link</a>
  <a href="mailto:test@example.com">Email</a>
  <a href="/about">Duplicate About</a>
  <img src="/images/photo.jpg" srcset="/images/photo@2x.jpg 2x">
  <script src="/assets/app.js"></script>
  <script>var x = 1;</script>
  <style>.hidden { display: none; }</style>
</body>
</html>`

func TestExtractLinks(t *testing.T) {
	ext := fetcher.NewLinkExtractor()
	links := ext.ExtractLinks(strings.NewReader(testHTML), "https://example.com/page", "example.com")

	// Should find navigable anchors plus browsable link[href] targets.
	// Should skip stylesheet/image/script assets, javascript:, mailto:, duplicate /about.
	assert.Len(t, links, 5)

	// Check external classification.
	var externalCount int
	for _, l := range links {
		if l.IsExternal {
			externalCount++
		}
	}
	assert.Equal(t, 1, externalCount)

	// Check nofollow preserved.
	for _, l := range links {
		if strings.Contains(l.Normalized, "contact") {
			assert.Equal(t, "nofollow", l.Rel)
		}
	}
}

func TestExtractTitle(t *testing.T) {
	title := fetcher.ExtractTitle(strings.NewReader(testHTML))
	assert.Equal(t, "Test Page", title)
}

func TestExtractTitle_Empty(t *testing.T) {
	title := fetcher.ExtractTitle(strings.NewReader("<html><body>no title</body></html>"))
	assert.Equal(t, "", title)
}

func TestExtractMetaTags(t *testing.T) {
	meta := fetcher.ExtractMetaTags(strings.NewReader(testHTML))
	assert.Equal(t, "A test page", meta["description"])
	assert.Equal(t, "OG Title", meta["og:title"])
}

func TestExtractText(t *testing.T) {
	text := fetcher.ExtractText(strings.NewReader(testHTML))
	assert.Contains(t, text, "Hello World")
	assert.Contains(t, text, "Some text here.")
	assert.NotContains(t, text, "var x = 1")     // script content stripped
	assert.NotContains(t, text, "display: none") // style content stripped
}

func TestExtractLinks_RelativeURLs(t *testing.T) {
	html := `<html><body>
		<a href="../sibling">Up</a>
		<a href="child">Down</a>
		<a href="//cdn.example.com/lib.js">Protocol-relative</a>
	</body></html>`

	ext := fetcher.NewLinkExtractor()
	links := ext.ExtractLinks(strings.NewReader(html), "https://example.com/dir/page", "example.com")

	assert.GreaterOrEqual(t, len(links), 2)
}

func TestExtractLinks_OnlyKeepsBrowsableEmbeddedDocuments(t *testing.T) {
	html := `<html><body>
		<iframe src="/docs/viewer"></iframe>
		<embed src="/docs/report.pdf"></embed>
		<object data="/docs/schema.xml"></object>
		<script src="/assets/app.js"></script>
		<img src="/images/logo.svg">
		<link rel="stylesheet" href="/assets/site.css">
	</body></html>`

	ext := fetcher.NewLinkExtractor()
	links := ext.ExtractLinks(strings.NewReader(html), "https://example.com/page", "example.com")

	var got []string
	for _, link := range links {
		got = append(got, link.Normalized)
	}

	assert.ElementsMatch(t, []string{
		"https://example.com/docs/viewer",
		"https://example.com/docs/report.pdf",
		"https://example.com/docs/schema.xml",
	}, got)
}
