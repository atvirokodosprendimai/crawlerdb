package store

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
	"gorm.io/gorm"
)

// PageRepository implements ports.PageRepository using GORM.
type PageRepository struct {
	db         *gorm.DB
	contentDir string
}

type PageRepositoryOption func(*PageRepository)

// WithContentDir sets the root directory for raw page files.
func WithContentDir(dir string) PageRepositoryOption {
	return func(r *PageRepository) {
		if strings.TrimSpace(dir) != "" {
			r.contentDir = dir
		}
	}
}

// NewPageRepository creates a new PageRepository.
func NewPageRepository(db *gorm.DB, opts ...PageRepositoryOption) *PageRepository {
	r := &PageRepository{
		db:         db,
		contentDir: "data",
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func (r *PageRepository) Store(ctx context.Context, page *entities.Page) error {
	if err := r.persistContent(ctx, page); err != nil {
		return fmt.Errorf("persist page content: %w", err)
	}
	m, err := pageToModel(page)
	if err != nil {
		return fmt.Errorf("convert page to model: %w", err)
	}
	return r.db.WithContext(ctx).Create(m).Error
}

func (r *PageRepository) FindByURLID(ctx context.Context, urlID string) (*entities.Page, error) {
	var m PageModel
	if err := r.db.WithContext(ctx).Where("url_id = ?", urlID).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return modelToPage(&m)
}

func (r *PageRepository) FindByJobID(ctx context.Context, jobID string, limit, offset int) ([]*entities.Page, error) {
	var models []PageModel
	if err := r.db.WithContext(ctx).
		Where("job_id = ?", jobID).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&models).Error; err != nil {
		return nil, err
	}
	pages := make([]*entities.Page, 0, len(models))
	for _, m := range models {
		p, err := modelToPage(&m)
		if err != nil {
			return nil, err
		}
		pages = append(pages, p)
	}
	return pages, nil
}

func (r *PageRepository) persistContent(ctx context.Context, page *entities.Page) error {
	payload := page.RawContent
	if len(payload) == 0 && page.HTMLBody != "" {
		payload = []byte(page.HTMLBody)
	}
	if len(payload) == 0 {
		page.ContentPath = ""
		page.ContentSize = 0
		return nil
	}

	var crawlURL URLModel
	if err := r.db.WithContext(ctx).Where("id = ?", page.URLID).First(&crawlURL).Error; err != nil {
		return err
	}

	relativePath, err := buildContentPath(r.contentDir, crawlURL.Normalized, page.ContentType)
	if err != nil {
		return err
	}
	absPath := relativePath
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(".", absPath)
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return fmt.Errorf("create content dir: %w", err)
	}
	if err := os.WriteFile(absPath, payload, 0o644); err != nil {
		return fmt.Errorf("write content file: %w", err)
	}

	page.ContentPath = filepath.ToSlash(relativePath)
	page.ContentSize = int64(len(payload))
	page.RawContent = nil
	page.HTMLBody = ""
	page.TextContent = ""
	return nil
}

func buildContentPath(rootDir, normalizedURL, contentType string) (string, error) {
	sum := fmt.Sprintf("%x", md5.Sum([]byte(normalizedURL)))
	if len(sum) < 5 {
		return "", fmt.Errorf("md5 sum too short")
	}

	ext := inferContentExtension(normalizedURL, contentType)
	parts := []string{
		rootDir,
		string(sum[0]),
		string(sum[1]),
		string(sum[2]),
		string(sum[3]),
		string(sum[4]),
		sum + ext,
	}
	return filepath.Join(parts...), nil
}

func inferContentExtension(rawURL, contentType string) string {
	if mediaType, _, err := mime.ParseMediaType(contentType); err == nil {
		switch strings.ToLower(mediaType) {
		case "text/html", "application/xhtml+xml":
			return ".html"
		case "application/pdf":
			return ".pdf"
		case "application/json":
			return ".json"
		case "application/xml", "text/xml":
			return ".xml"
		case "text/plain":
			return ".txt"
		}
	}

	if parsed, err := url.Parse(rawURL); err == nil {
		ext := strings.ToLower(filepath.Ext(parsed.Path))
		if ext != "" && len(ext) <= 10 {
			return ext
		}
	}
	return ".bin"
}
