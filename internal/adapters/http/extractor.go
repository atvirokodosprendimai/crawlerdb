package fetcher

import (
	"io"
	"net/url"
	"strings"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/services"
	"golang.org/x/net/html"
)

// LinkExtractor extracts and classifies links from HTML.
type LinkExtractor struct {
	normalizer *services.URLNormalizer
}

// NewLinkExtractor creates a new link extractor.
func NewLinkExtractor() *LinkExtractor {
	return &LinkExtractor{
		normalizer: services.NewURLNormalizer(),
	}
}

// ExtractLinks parses HTML and returns all discovered links.
func (e *LinkExtractor) ExtractLinks(body io.Reader, pageURL, seedHost string) []entities.DiscoveredLink {
	doc, err := html.Parse(body)
	if err != nil {
		return nil
	}

	var links []entities.DiscoveredLink
	seen := make(map[string]struct{})

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			var href, rel, anchor string

			switch n.Data {
			case "a":
				href = getAttr(n, "href")
				rel = getAttr(n, "rel")
				anchor = extractText(n)
			case "link":
				href = getAttr(n, "href")
				rel = getAttr(n, "rel")
			}

			if href != "" && !isIgnoredScheme(href) {
				norm, err := e.normalizer.Normalize(href, pageURL)
				if err == nil {
					if _, ok := seen[norm.Hash]; !ok {
						seen[norm.Hash] = struct{}{}
						link := entities.DiscoveredLink{
							RawURL:     href,
							Normalized: norm.Normalized,
							URLHash:    norm.Hash,
							IsExternal: !e.normalizer.IsInternal(norm.Normalized, seedHost),
							Rel:        rel,
							Anchor:     strings.TrimSpace(anchor),
						}
						links = append(links, link)
					}
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	return links
}

// ExtractTitle returns the <title> text from HTML.
func ExtractTitle(body io.Reader) string {
	doc, err := html.Parse(body)
	if err != nil {
		return ""
	}

	var title string
	var find func(*html.Node)
	find = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "title" {
			title = extractText(n)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
			if title != "" {
				return
			}
		}
	}
	find(doc)
	return strings.TrimSpace(title)
}

// ExtractMetaTags returns meta tag name/property -> content mappings.
func ExtractMetaTags(body io.Reader) map[string]string {
	doc, err := html.Parse(body)
	if err != nil {
		return nil
	}

	meta := make(map[string]string)
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "meta" {
			name := getAttr(n, "name")
			if name == "" {
				name = getAttr(n, "property")
			}
			content := getAttr(n, "content")
			if name != "" && content != "" {
				meta[name] = content
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return meta
}

// ExtractText returns visible text content from HTML (strips tags).
func ExtractText(body io.Reader) string {
	doc, err := html.Parse(body)
	if err != nil {
		return ""
	}

	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			// Skip non-visible elements.
			switch n.Data {
			case "script", "style", "noscript", "head":
				return
			}
		}
		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				sb.WriteString(text)
				sb.WriteString(" ")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return strings.TrimSpace(sb.String())
}

func getAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func extractText(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return sb.String()
}

func isIgnoredScheme(href string) bool {
	u, err := url.Parse(href)
	if err != nil {
		return true
	}
	switch strings.ToLower(u.Scheme) {
	case "javascript", "mailto", "tel", "data", "blob", "ftp":
		return true
	}
	return false
}
