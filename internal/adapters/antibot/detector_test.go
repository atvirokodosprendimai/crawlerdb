package antibot_test

import (
	"net/http"
	"testing"

	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/antibot"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
	"github.com/stretchr/testify/assert"
)

func TestDetector_CleanPage(t *testing.T) {
	d := antibot.NewDetector()
	result := d.Analyze(&ports.FetchResponse{
		StatusCode: 200,
		Headers:    http.Header{},
	}, []byte("<html><body>Normal page</body></html>"))

	assert.False(t, result.Detected)
}

func TestDetector_403WithChallenge(t *testing.T) {
	d := antibot.NewDetector()
	result := d.Analyze(&ports.FetchResponse{
		StatusCode: 403,
		Headers:    http.Header{},
	}, []byte("<html><body>Access Denied - Please verify you are human</body></html>"))

	assert.True(t, result.Detected)
	assert.Equal(t, "block", result.EventType)
}

func TestDetector_429RateLimit(t *testing.T) {
	d := antibot.NewDetector()
	result := d.Analyze(&ports.FetchResponse{
		StatusCode: 429,
		Headers:    http.Header{},
	}, []byte("Rate limited"))

	assert.True(t, result.Detected)
	assert.Equal(t, "rate_limit", result.EventType)
}

func TestDetector_ReCAPTCHA(t *testing.T) {
	d := antibot.NewDetector()
	body := `<html><body>
		<script src="https://www.google.com/recaptcha/api.js"></script>
		<div class="g-recaptcha" data-sitekey="xxx"></div>
	</body></html>`

	result := d.Analyze(&ports.FetchResponse{
		StatusCode: 200,
		Headers:    http.Header{},
	}, []byte(body))

	assert.True(t, result.Detected)
	assert.Equal(t, "captcha", result.EventType)
	assert.Equal(t, "recaptcha", result.Provider)
}

func TestDetector_HCaptcha(t *testing.T) {
	d := antibot.NewDetector()
	body := `<html><body>
		<script src="https://hcaptcha.com/1/api.js"></script>
		<div class="h-captcha"></div>
	</body></html>`

	result := d.Analyze(&ports.FetchResponse{
		StatusCode: 200,
		Headers:    http.Header{},
	}, []byte(body))

	assert.True(t, result.Detected)
	assert.Equal(t, "captcha", result.EventType)
	assert.Equal(t, "hcaptcha", result.Provider)
}

func TestDetector_CloudflareUAM(t *testing.T) {
	d := antibot.NewDetector()
	body := `<html><body>
		<div id="cf-browser-verification">
			<p>Checking your browser before accessing cloudflare protected site.</p>
			<span>Ray ID: abc123</span>
		</div>
	</body></html>`

	result := d.Analyze(&ports.FetchResponse{
		StatusCode: 503,
		Headers:    http.Header{},
	}, []byte(body))

	assert.True(t, result.Detected)
	// 503 with challenge body triggers status check first.
	assert.Equal(t, "challenge", result.EventType)
}

func TestDetector_DataDome(t *testing.T) {
	d := antibot.NewDetector()
	body := `<html><body>
		<script src="https://js.datadome.co/tags.js"></script>
	</body></html>`

	result := d.Analyze(&ports.FetchResponse{
		StatusCode: 200,
		Headers:    http.Header{},
	}, []byte(body))

	assert.True(t, result.Detected)
	assert.Equal(t, "datadome", result.Provider)
}

func TestDetector_403WithoutChallenge(t *testing.T) {
	d := antibot.NewDetector()
	// 403 without challenge keywords = not bot detection.
	result := d.Analyze(&ports.FetchResponse{
		StatusCode: 403,
		Headers:    http.Header{},
	}, []byte("<html><body>Forbidden</body></html>"))

	assert.False(t, result.Detected)
}
