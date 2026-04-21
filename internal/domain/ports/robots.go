package ports

import (
	"context"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/valueobj"
)

// RobotsChecker determines whether a URL is allowed by robots.txt.
type RobotsChecker interface {
	IsAllowed(ctx context.Context, url, userAgent string) (bool, error)
	GetPolicy(ctx context.Context, domain string) (*valueobj.RobotsPolicy, error)
}
