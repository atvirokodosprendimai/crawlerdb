package services_test

import (
	"testing"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalize(t *testing.T) {
	n := services.NewURLNormalizer()

	tests := []struct {
		name     string
		input    string
		base     string
		expected string
	}{
		{"lowercase host", "HTTPS://EXAMPLE.COM/Path", "", "https://example.com/Path"},
		{"strip fragment", "https://example.com/page#section", "", "https://example.com/page"},
		{"sort query params", "https://example.com/page?z=1&a=2&m=3", "", "https://example.com/page?a=2&m=3&z=1"},
		{"strip trailing slash", "https://example.com/page/", "", "https://example.com/page"},
		{"keep root slash", "https://example.com/", "", "https://example.com/"},
		{"strip default port 80", "http://example.com:80/page", "", "http://example.com/page"},
		{"strip default port 443", "https://example.com:443/page", "", "https://example.com/page"},
		{"keep non-default port", "https://example.com:8080/page", "", "https://example.com:8080/page"},
		{"resolve relative URL", "/about", "https://example.com/page", "https://example.com/about"},
		{"resolve relative with base path", "other", "https://example.com/dir/page", "https://example.com/dir/other"},
		{"remove dot segments", "https://example.com/a/b/../c", "", "https://example.com/a/c"},
		{"empty query string stripped", "https://example.com/page?", "", "https://example.com/page"},
		{"decode unnecessary encoding", "https://example.com/%7Euser", "", "https://example.com/~user"},
		{"scheme lowercase", "HTTP://example.com/page", "", "http://example.com/page"},
		{"repair malformed http scheme", "HTTP//example.com/file.pdf", "", "http://example.com/file.pdf"},
		{"repair malformed https scheme", "HTTPS//example.com/file.pdf", "", "https://example.com/file.pdf"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := n.Normalize(tt.input, tt.base)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result.Normalized)
			assert.NotEmpty(t, result.Hash)
		})
	}
}

func TestNormalize_InvalidURL(t *testing.T) {
	n := services.NewURLNormalizer()
	_, err := n.Normalize("://invalid", "")
	assert.Error(t, err)
}

func TestNormalize_HashConsistency(t *testing.T) {
	n := services.NewURLNormalizer()

	r1, err := n.Normalize("HTTPS://EXAMPLE.COM/page?b=2&a=1", "")
	require.NoError(t, err)

	r2, err := n.Normalize("https://example.com/page?a=1&b=2", "")
	require.NoError(t, err)

	assert.Equal(t, r1.Hash, r2.Hash, "same URL different forms should produce same hash")
	assert.Equal(t, r1.Normalized, r2.Normalized)
}

func TestIsInternal(t *testing.T) {
	n := services.NewURLNormalizer()

	tests := []struct {
		name     string
		url      string
		seedHost string
		expected bool
	}{
		{"same domain", "https://example.com/page", "example.com", true},
		{"different domain", "https://other.com/page", "example.com", false},
		{"subdomain", "https://sub.example.com/page", "example.com", false},
		{"www variant", "https://www.example.com/page", "example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, n.IsInternal(tt.url, tt.seedHost))
		})
	}
}

func TestIsSameOrSubdomain(t *testing.T) {
	n := services.NewURLNormalizer()

	tests := []struct {
		name     string
		url      string
		seedHost string
		expected bool
	}{
		{"same domain", "https://example.com/page", "example.com", true},
		{"subdomain", "https://sub.example.com/page", "example.com", true},
		{"deep subdomain", "https://a.b.example.com/page", "example.com", true},
		{"different domain", "https://other.com/page", "example.com", false},
		{"suffix match but different", "https://notexample.com/page", "example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, n.IsSameOrSubdomain(tt.url, tt.seedHost))
		})
	}
}
