package antibot_test

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/antibot"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
	"github.com/stretchr/testify/assert"
)

func TestSkipStrategy_AlwaysSkips(t *testing.T) {
	s := antibot.NewSkipStrategy()

	tests := []string{"block", "rate_limit", "captcha", "challenge"}
	for _, evt := range tests {
		action := s.Handle(ports.DetectionResult{
			Detected:  true,
			EventType: evt,
		})
		assert.Equal(t, antibot.ActionSkip, action.Type, "event %s", evt)
	}
}

func TestRotateStrategy_Block(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	s := antibot.NewRotateStrategy(3, 2*time.Second, logger)

	action := s.Handle(ports.DetectionResult{
		Detected:  true,
		EventType: "block",
		Provider:  "cloudflare",
	})

	assert.Equal(t, antibot.ActionRetry, action.Type)
	assert.Equal(t, 2*time.Second, action.Delay)
}

func TestRotateStrategy_RateLimit(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	s := antibot.NewRotateStrategy(3, 2*time.Second, logger)

	action := s.Handle(ports.DetectionResult{
		Detected:  true,
		EventType: "rate_limit",
	})

	assert.Equal(t, antibot.ActionBackoff, action.Type)
	assert.Equal(t, 4*time.Second, action.Delay) // 2x backoff
}

func TestRotateStrategy_CaptchaSkips(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	s := antibot.NewRotateStrategy(3, 2*time.Second, logger)

	action := s.Handle(ports.DetectionResult{
		Detected:  true,
		EventType: "captcha",
		Provider:  "recaptcha",
	})

	assert.Equal(t, antibot.ActionSkip, action.Type)
}

func TestSolverStrategy_Captcha(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	s := antibot.NewSolverStrategy(nil, 3, 2*time.Second, logger)

	action := s.Handle(ports.DetectionResult{
		Detected:  true,
		EventType: "captcha",
		Provider:  "recaptcha",
	})

	assert.Equal(t, antibot.ActionSolve, action.Type)
}

func TestSolverStrategy_Block(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	s := antibot.NewSolverStrategy(nil, 3, 2*time.Second, logger)

	action := s.Handle(ports.DetectionResult{
		Detected:  true,
		EventType: "block",
		Provider:  "akamai",
	})

	assert.Equal(t, antibot.ActionRetry, action.Type)
}
