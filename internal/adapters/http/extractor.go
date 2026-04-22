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
			for _, ref := range extractNodeReferences(n) {
				if ref.href == "" || isIgnoredScheme(ref.href) {
					continue
				}

				norm, err := e.normalizer.Normalize(ref.href, pageURL)
				if err != nil {
					continue
				}
				if _, ok := seen[norm.Hash]; ok {
					continue
				}

				seen[norm.Hash] = struct{}{}
				link := entities.DiscoveredLink{
					RawURL:     ref.href,
					Normalized: norm.Normalized,
					URLHash:    norm.Hash,
					IsExternal: !e.normalizer.IsInternal(norm.Normalized, seedHost),
					Rel:        ref.rel,
					Anchor:     strings.TrimSpace(ref.anchor),
				}
				links = append(links, link)
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	return links
}

type extractedReference struct {
	href   string
	rel    string
	anchor string
}

func extractNodeReferences(n *html.Node) []extractedReference {
	var refs []extractedReference
	seen := make(map[string]struct{})

	appendRef := func(rawURL, rel, anchor string) {
		rawURL = strings.TrimSpace(rawURL)
		if rawURL == "" {
			return
		}
		if _, ok := seen[rawURL]; ok {
			return
		}
		seen[rawURL] = struct{}{}
		refs = append(refs, extractedReference{
			href:   rawURL,
			rel:    rel,
			anchor: anchor,
		})
	}

	appendSrcset := func(raw string) {
		for _, candidate := range parseSrcset(raw) {
			appendRef(candidate, "", "")
		}
	}

	switch n.Data {
	case "a":
		appendRef(getAttr(n, "href"), getAttr(n, "rel"), extractText(n))
	case "link":
		appendRef(getAttr(n, "href"), getAttr(n, "rel"), "")
		appendSrcset(getAttr(n, "imagesrcset"))
	case "img", "source":
		appendRef(getAttr(n, "src"), "", "")
		appendRef(getAttr(n, "data-src"), "", "")
		appendSrcset(getAttr(n, "srcset"))
		appendSrcset(getAttr(n, "data-srcset"))
	case "script", "iframe", "audio", "video", "track", "embed":
		appendRef(getAttr(n, "src"), "", "")
		appendRef(getAttr(n, "data-src"), "", "")
	case "object":
		appendRef(getAttr(n, "data"), "", "")
	}

	return refs
}

func parseSrcset(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		fields := strings.Fields(strings.TrimSpace(part))
		if len(fields) == 0 {
			continue
		}
		out = append(out, fields[0])
	}
	return out
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
