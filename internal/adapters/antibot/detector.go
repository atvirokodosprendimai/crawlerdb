package antibot

import (
	"strings"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
)

// Detector implements ports.AntiBotDetector.
type Detector struct{}

// NewDetector creates a new anti-bot detector.
func NewDetector() *Detector {
	return &Detector{}
}

// Analyze checks an HTTP response for anti-bot signals.
func (d *Detector) Analyze(resp *ports.FetchResponse, body []byte) ports.DetectionResult {
	content := string(body)
	contentLower := strings.ToLower(content)

	// Check HTTP status patterns.
	if result := d.checkStatusCode(resp.StatusCode, contentLower); result.Detected {
		return result
	}

	// Check captcha providers.
	if result := d.checkCaptchaProviders(contentLower); result.Detected {
		return result
	}

	// Check JavaScript challenges.
	if result := d.checkJSChallenges(contentLower); result.Detected {
		return result
	}

	return ports.DetectionResult{Detected: false}
}

func (d *Detector) checkStatusCode(status int, body string) ports.DetectionResult {
	switch {
	case status == 403 && containsAny(body, challengeIndicators):
		return ports.DetectionResult{
			Detected:  true,
			EventType: "block",
			Details:   "403 with challenge page",
		}
	case status == 429:
		return ports.DetectionResult{
			Detected:  true,
			EventType: "rate_limit",
			Details:   "HTTP 429 Too Many Requests",
		}
	case status == 503 && containsAny(body, challengeIndicators):
		return ports.DetectionResult{
			Detected:  true,
			EventType: "challenge",
			Details:   "503 with challenge page",
		}
	}
	return ports.DetectionResult{}
}

func (d *Detector) checkCaptchaProviders(body string) ports.DetectionResult {
	providers := []struct {
		name    string
		signals []string
	}{
		{"recaptcha", []string{"google.com/recaptcha", "grecaptcha", "g-recaptcha"}},
		{"hcaptcha", []string{"hcaptcha.com", "h-captcha"}},
		{"cloudflare_turnstile", []string{"challenges.cloudflare.com/turnstile", "cf-turnstile"}},
		{"datadome", []string{"datadome.co", "dd.js"}},
		{"funcaptcha", []string{"funcaptcha.com", "arkoselabs.com"}},
		{"geetest", []string{"geetest.com", "gt_lib"}},
	}

	for _, p := range providers {
		if containsAny(body, p.signals) {
			return ports.DetectionResult{
				Detected:  true,
				EventType: "captcha",
				Provider:  p.name,
				Details:   "captcha provider detected: " + p.name,
			}
		}
	}
	return ports.DetectionResult{}
}

func (d *Detector) checkJSChallenges(body string) ports.DetectionResult {
	challenges := []struct {
		name    string
		signals []string
	}{
		{"cloudflare_uam", []string{"cf-browser-verification", "checking your browser", "ray id", "cloudflare"}},
		{"akamai", []string{"akamai", "_abck", "akam"}},
		{"imperva", []string{"incapsula", "imperva", "_incap_"}},
		{"perimeterx", []string{"perimeterx", "_pxhd", "px-captcha"}},
	}

	for _, c := range challenges {
		matchCount := 0
		for _, sig := range c.signals {
			if strings.Contains(body, sig) {
				matchCount++
			}
		}
		// Require at least 2 signals to avoid false positives.
		if matchCount >= 2 {
			return ports.DetectionResult{
				Detected:  true,
				EventType: "challenge",
				Provider:  c.name,
				Details:   "JavaScript challenge detected: " + c.name,
			}
		}
	}
	return ports.DetectionResult{}
}

var challengeIndicators = []string{
	"captcha", "challenge", "verify you are human",
	"please enable javascript", "checking your browser",
	"access denied", "bot detected", "security check",
}

func containsAny(s string, substrs []string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
