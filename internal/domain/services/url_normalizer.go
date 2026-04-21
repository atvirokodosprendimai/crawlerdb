package services

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/valueobj"
)

// URLNormalizer handles URL normalization and classification.
type URLNormalizer struct{}

// NewURLNormalizer creates a new URLNormalizer.
func NewURLNormalizer() *URLNormalizer {
	return &URLNormalizer{}
}

// Normalize normalizes a URL and returns the result with SHA-256 hash.
// If base is non-empty, relative URLs are resolved against it.
func (n *URLNormalizer) Normalize(rawURL, base string) (valueobj.NormalizedURL, error) {
	rawURL = sanitizeRawURL(rawURL)
	base = sanitizeRawURL(base)

	var parsed *url.URL
	var err error

	if base != "" {
		baseURL, e := url.Parse(base)
		if e != nil {
			return valueobj.NormalizedURL{}, fmt.Errorf("parse base URL: %w", e)
		}
		ref, e := url.Parse(rawURL)
		if e != nil {
			return valueobj.NormalizedURL{}, fmt.Errorf("parse URL: %w", e)
		}
		parsed = baseURL.ResolveReference(ref)
	} else {
		parsed, err = url.Parse(rawURL)
		if err != nil {
			return valueobj.NormalizedURL{}, fmt.Errorf("parse URL: %w", err)
		}
	}

	if parsed.Scheme == "" || parsed.Host == "" {
		return valueobj.NormalizedURL{}, fmt.Errorf("invalid URL (missing scheme or host): %q", rawURL)
	}

	// Lowercase scheme and host.
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)

	// Strip default ports.
	host := parsed.Hostname()
	port := parsed.Port()
	if (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
		parsed.Host = host
	}

	// Strip fragment.
	parsed.Fragment = ""
	parsed.RawFragment = ""

	// Resolve dot segments in path.
	if strings.Contains(parsed.Path, "..") || strings.Contains(parsed.Path, "/.") {
		parsed.Path = resolveDotSegments(parsed.Path)
	}

	// Decode unnecessary percent-encoding in path.
	if parsed.RawPath != "" {
		decoded, e := url.PathUnescape(parsed.RawPath)
		if e == nil {
			parsed.Path = decoded
			parsed.RawPath = ""
		}
	}

	// Sort query parameters; strip empty query.
	parsed.ForceQuery = false
	if parsed.RawQuery != "" {
		params := parsed.Query()
		if len(params) == 0 {
			parsed.RawQuery = ""
		} else {
			keys := make([]string, 0, len(params))
			for k := range params {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			var parts []string
			for _, k := range keys {
				vals := params[k]
				sort.Strings(vals)
				for _, v := range vals {
					parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
				}
			}
			parsed.RawQuery = strings.Join(parts, "&")
		}
	}

	// Strip trailing slash (but keep root /).
	if len(parsed.Path) > 1 && strings.HasSuffix(parsed.Path, "/") {
		parsed.Path = strings.TrimRight(parsed.Path, "/")
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}

	normalized := parsed.String()
	hash := sha256Hex(normalized)

	return valueobj.NormalizedURL{
		Raw:        rawURL,
		Normalized: normalized,
		Hash:       hash,
	}, nil
}

func sanitizeRawURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return trimmed
	}
	lower := strings.ToLower(trimmed)
	for _, scheme := range []string{"http", "https"} {
		prefix := scheme + "//"
		if strings.HasPrefix(lower, prefix) {
			return scheme + ":" + trimmed[len(scheme):]
		}
	}
	return trimmed
}

// IsInternal checks if a URL belongs to the exact same host.
func (n *URLNormalizer) IsInternal(rawURL, seedHost string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return strings.ToLower(parsed.Hostname()) == strings.ToLower(seedHost)
}

// IsSameOrSubdomain checks if a URL is the same domain or a subdomain.
func (n *URLNormalizer) IsSameOrSubdomain(rawURL, seedHost string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	seed := strings.ToLower(seedHost)

	if host == seed {
		return true
	}
	return strings.HasSuffix(host, "."+seed)
}

// resolveDotSegments resolves "." and ".." segments in a URL path.
func resolveDotSegments(path string) string {
	segments := strings.Split(path, "/")
	var out []string
	for _, seg := range segments {
		switch seg {
		case ".":
			continue
		case "..":
			if len(out) > 1 { // keep leading empty segment for absolute path
				out = out[:len(out)-1]
			}
		default:
			out = append(out, seg)
		}
	}
	result := strings.Join(out, "/")
	if result == "" {
		return "/"
	}
	return result
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h)
}
