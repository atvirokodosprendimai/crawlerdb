package antibot

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
)

// Strategy defines how to respond to anti-bot detection.
type Strategy interface {
	// Handle processes a detection result and returns an action.
	Handle(result ports.DetectionResult) Action
}

// ActionType indicates what the caller should do after detection.
type ActionType string

const (
	ActionRetry   ActionType = "retry"
	ActionSkip    ActionType = "skip"
	ActionBackoff ActionType = "backoff"
	ActionSolve   ActionType = "solve"
)

// Action describes the response to an anti-bot event.
type Action struct {
	Type     ActionType
	Delay    time.Duration
	Reason   string
	SolveURL string // For captcha solving services.
}

// SkipStrategy ignores anti-bot signals and continues crawling.
type SkipStrategy struct{}

// NewSkipStrategy creates a strategy that skips all anti-bot events.
func NewSkipStrategy() *SkipStrategy {
	return &SkipStrategy{}
}

// Handle always returns skip.
func (s *SkipStrategy) Handle(_ ports.DetectionResult) Action {
	return Action{
		Type:   ActionSkip,
		Reason: "skip strategy: ignoring anti-bot signal",
	}
}

// RotateStrategy rotates proxies and retries on detection.
type RotateStrategy struct {
	maxRetries int
	backoff    time.Duration
	logger     *slog.Logger
}

// NewRotateStrategy creates a strategy that rotates proxies on detection.
func NewRotateStrategy(maxRetries int, backoff time.Duration, logger *slog.Logger) *RotateStrategy {
	return &RotateStrategy{
		maxRetries: maxRetries,
		backoff:    backoff,
		logger:     logger,
	}
}

// Handle returns retry with backoff for blocks/challenges, skip for captchas.
func (s *RotateStrategy) Handle(result ports.DetectionResult) Action {
	switch result.EventType {
	case "rate_limit":
		return Action{
			Type:   ActionBackoff,
			Delay:  s.backoff * 2,
			Reason: fmt.Sprintf("rate limited, backing off %v", s.backoff*2),
		}
	case "block", "challenge":
		return Action{
			Type:   ActionRetry,
			Delay:  s.backoff,
			Reason: fmt.Sprintf("blocked/challenged by %s, rotating proxy", result.Provider),
		}
	case "captcha":
		// Rotate strategy cannot solve captchas; skip.
		return Action{
			Type:   ActionSkip,
			Reason: fmt.Sprintf("captcha from %s, cannot solve in rotate mode", result.Provider),
		}
	default:
		return Action{
			Type:   ActionSkip,
			Reason: "unknown detection type",
		}
	}
}

// SolverStrategy delegates captcha solving to an external service.
type SolverStrategy struct {
	solver     ports.CaptchaSolver
	maxRetries int
	backoff    time.Duration
	logger     *slog.Logger
}

// NewSolverStrategy creates a strategy that solves captchas via external service.
func NewSolverStrategy(solver ports.CaptchaSolver, maxRetries int, backoff time.Duration, logger *slog.Logger) *SolverStrategy {
	return &SolverStrategy{
		solver:     solver,
		maxRetries: maxRetries,
		backoff:    backoff,
		logger:     logger,
	}
}

// Handle returns solve for captchas, retry for blocks, backoff for rate limits.
func (s *SolverStrategy) Handle(result ports.DetectionResult) Action {
	switch result.EventType {
	case "captcha":
		return Action{
			Type:   ActionSolve,
			Reason: fmt.Sprintf("captcha from %s, delegating to solver", result.Provider),
		}
	case "rate_limit":
		return Action{
			Type:   ActionBackoff,
			Delay:  s.backoff * 2,
			Reason: "rate limited, backing off",
		}
	case "block", "challenge":
		return Action{
			Type:   ActionRetry,
			Delay:  s.backoff,
			Reason: fmt.Sprintf("blocked/challenged by %s, retrying", result.Provider),
		}
	default:
		return Action{
			Type:   ActionSkip,
			Reason: "unknown detection type",
		}
	}
}
