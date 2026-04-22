package ports

import (
	"context"
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
)

// JobRepository defines persistence operations for jobs.
type JobRepository interface {
	Create(ctx context.Context, job *entities.Job) error
	FindByID(ctx context.Context, id string) (*entities.Job, error)
	Update(ctx context.Context, job *entities.Job) error
	List(ctx context.Context, limit, offset int) ([]*entities.Job, error)
	FindByStatus(ctx context.Context, status entities.JobStatus) ([]*entities.Job, error)
}

// URLRepository defines persistence operations for crawl URLs.
type URLRepository interface {
	Enqueue(ctx context.Context, url *entities.CrawlURL) error
	EnqueueBatch(ctx context.Context, urls []*entities.CrawlURL) error
	Claim(ctx context.Context, jobID string, limit int) ([]*entities.CrawlURL, error)
	ClaimByIDs(ctx context.Context, jobID string, ids []string) ([]*entities.CrawlURL, error)
	RequeueCrawlingByDomain(ctx context.Context, jobID, domain string) (int64, error)
	RequeueTimedOutCrawling(ctx context.Context, before time.Time) (int64, error)
	RequeueDueRevisits(ctx context.Context, before time.Time) (int64, error)
	RequeueJobForRevisit(ctx context.Context, jobID string) (int64, error)
	Complete(ctx context.Context, url *entities.CrawlURL) error
	FindByHash(ctx context.Context, jobID, urlHash string) (*entities.CrawlURL, error)
	FindPending(ctx context.Context, jobID string, limit int) ([]*entities.CrawlURL, error)
	FindByJobID(ctx context.Context, jobID string, limit, offset int) ([]*entities.CrawlURL, error)
	FindByJobIDAndStatuses(ctx context.Context, jobID string, statuses []entities.URLStatus, limit, offset int) ([]*entities.CrawlURL, error)
	CountByStatus(ctx context.Context, jobID string) (map[entities.URLStatus]int, error)
}

// PageRepository defines persistence operations for crawled pages.
type PageRepository interface {
	Store(ctx context.Context, page *entities.Page) error
	FindByURLID(ctx context.Context, urlID string) (*entities.Page, error)
	FindByJobID(ctx context.Context, jobID string, limit, offset int) ([]*entities.Page, error)
}
