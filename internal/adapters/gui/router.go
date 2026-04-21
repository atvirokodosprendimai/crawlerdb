package gui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/a-h/templ"
	store "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/db/gorm"
	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/export"
	"github.com/atvirokodosprendimai/crawlerdb/internal/app/services"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
	"github.com/atvirokodosprendimai/crawlerdb/web"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"gorm.io/gorm"
)

// Router owns the HTTP handler and any long-lived GUI resources.
type Router struct {
	handler http.Handler
	sse     *SSEBroker
	sub     ports.Subscription
}

// NewRouter creates the chi router with all API endpoints.
func NewRouter(db *gorm.DB, broker ports.MessageBroker, logger *slog.Logger) *Router {
	r := chi.NewRouter()

	// Middleware.
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Use(corsMiddleware)

	jobRepo := store.NewJobRepository(db)
	pageRepo := store.NewPageRepository(db)
	urlRepo := store.NewURLRepository(db)

	exportSvc := services.NewExportService(
		export.NewJSONExporter(pageRepo),
		export.NewCSVExporter(pageRepo, urlRepo),
		export.NewSitemapExporter(urlRepo),
	)
	datastarHandlers := newDatastarDashboardHandlers(db, broker, jobRepo, urlRepo)

	sseBroker := NewSSEBroker(logger)

	// Forward NATS GUI events to SSE.
	var sub ports.Subscription
	if broker != nil {
		sub, _ = broker.Subscribe("gui.push.>", func(subj string, data []byte) error {
			sseBroker.Broadcast(subj, json.RawMessage(data))
			return nil
		})
	}

	// API routes.
	r.Route("/api", func(r chi.Router) {
		r.Use(middleware.SetHeader("Content-Type", "application/json"))
		// Jobs.
		r.Get("/jobs/overview", func(w http.ResponseWriter, req *http.Request) {
			jobs, err := jobRepo.List(req.Context(), 100, 0)
			if err != nil {
				writeError(w, err, http.StatusInternalServerError)
				return
			}

			type jobOverview struct {
				Job            any            `json:"job"`
				Counts         map[string]int `json:"counts"`
				ExceptionCount int            `json:"exception_count"`
			}

			overview := make([]jobOverview, 0, len(jobs))
			for _, job := range jobs {
				counts, err := urlRepo.CountByStatus(req.Context(), job.ID)
				if err != nil {
					writeError(w, err, http.StatusInternalServerError)
					return
				}

				flat := make(map[string]int, len(counts))
				for status, count := range counts {
					flat[string(status)] = count
				}

				overview = append(overview, jobOverview{
					Job:            job,
					Counts:         flat,
					ExceptionCount: flat["error"] + flat["blocked"],
				})
			}

			writeJSON(w, overview)
		})

		r.Get("/jobs", func(w http.ResponseWriter, req *http.Request) {
			jobs, err := jobRepo.List(req.Context(), 100, 0)
			if err != nil {
				writeError(w, err, http.StatusInternalServerError)
				return
			}
			writeJSON(w, jobs)
		})

		r.Get("/jobs/{id}", func(w http.ResponseWriter, req *http.Request) {
			id := chi.URLParam(req, "id")
			job, err := jobRepo.FindByID(req.Context(), id)
			if err != nil {
				writeError(w, err, http.StatusInternalServerError)
				return
			}
			if job == nil {
				writeError(w, nil, http.StatusNotFound)
				return
			}
			writeJSON(w, job)
		})

		// Job actions via NATS request/reply.
		r.Post("/jobs", func(w http.ResponseWriter, req *http.Request) {
			var body json.RawMessage
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				writeError(w, err, http.StatusBadRequest)
				return
			}
			reply, err := broker.Request(req.Context(), "job.create", body)
			if err != nil {
				writeError(w, err, http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(reply)
		})

		r.Post("/jobs/{id}/stop", func(w http.ResponseWriter, req *http.Request) {
			id := chi.URLParam(req, "id")
			data, _ := json.Marshal(map[string]string{"job_id": id})
			reply, err := broker.Request(req.Context(), "job.stop", data)
			if err != nil {
				writeError(w, err, http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(reply)
		})

		r.Post("/jobs/{id}/pause", func(w http.ResponseWriter, req *http.Request) {
			id := chi.URLParam(req, "id")
			data, _ := json.Marshal(map[string]string{"job_id": id})
			reply, err := broker.Request(req.Context(), "job.pause", data)
			if err != nil {
				writeError(w, err, http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(reply)
		})

		r.Post("/jobs/{id}/resume", func(w http.ResponseWriter, req *http.Request) {
			id := chi.URLParam(req, "id")
			data, _ := json.Marshal(map[string]string{"job_id": id})
			reply, err := broker.Request(req.Context(), "job.resume", data)
			if err != nil {
				writeError(w, err, http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(reply)
		})

		r.Post("/jobs/{id}/retry", func(w http.ResponseWriter, req *http.Request) {
			id := chi.URLParam(req, "id")
			data, _ := json.Marshal(map[string]string{"job_id": id})
			reply, err := broker.Request(req.Context(), "job.retry", data)
			if err != nil {
				writeError(w, err, http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(reply)
		})

		// Pages.
		r.Get("/jobs/{id}/pages", func(w http.ResponseWriter, req *http.Request) {
			id := chi.URLParam(req, "id")
			pages, err := pageRepo.FindByJobID(req.Context(), id, 100, 0)
			if err != nil {
				writeError(w, err, http.StatusInternalServerError)
				return
			}
			writeJSON(w, pages)
		})

		r.Get("/jobs/{id}/pages/{pageID}/content", func(w http.ResponseWriter, req *http.Request) {
			jobID := chi.URLParam(req, "id")
			pageID := chi.URLParam(req, "pageID")

			var page store.PageModel
			err := db.WithContext(req.Context()).
				Where("id = ? AND job_id = ?", pageID, jobID).
				First(&page).Error
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					writeError(w, nil, http.StatusNotFound)
					return
				}
				writeError(w, err, http.StatusInternalServerError)
				return
			}
			if strings.TrimSpace(page.ContentPath) == "" {
				writeError(w, fmt.Errorf("page content not stored"), http.StatusNotFound)
				return
			}

			path := filepath.Clean(page.ContentPath)
			if !filepath.IsAbs(path) {
				path = filepath.Join(".", path)
			}
			if _, err := os.Stat(path); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					writeError(w, fmt.Errorf("stored file missing"), http.StatusNotFound)
					return
				}
				writeError(w, err, http.StatusInternalServerError)
				return
			}

			if page.ContentType != "" {
				w.Header().Set("Content-Type", page.ContentType)
			}
			w.Header().Set("Content-Disposition", "inline")
			http.ServeFile(w, req, path)
		})

		// URLs.
		r.Get("/jobs/{id}/urls", func(w http.ResponseWriter, req *http.Request) {
			id := chi.URLParam(req, "id")
			counts, err := urlRepo.CountByStatus(req.Context(), id)
			if err != nil {
				writeError(w, err, http.StatusInternalServerError)
				return
			}
			writeJSON(w, counts)
		})

		r.Get("/jobs/{id}/site", func(w http.ResponseWriter, req *http.Request) {
			type siteRow struct {
				URLID       string `json:"url_id"`
				RawURL      string `json:"raw_url"`
				Normalized  string `json:"normalized"`
				Depth       int    `json:"depth"`
				Status      string `json:"status"`
				RetryCount  int    `json:"retry_count"`
				LastError   string `json:"last_error,omitempty"`
				FoundOn     string `json:"found_on,omitempty"`
				UpdatedAt   string `json:"updated_at"`
				PageID      string `json:"page_id,omitempty"`
				HTTPStatus  int    `json:"http_status,omitempty"`
				ContentType string `json:"content_type,omitempty"`
				ContentPath string `json:"content_path,omitempty"`
				ContentSize int64  `json:"content_size,omitempty"`
				Title       string `json:"title,omitempty"`
				FetchedAt   string `json:"fetched_at,omitempty"`
				FileURL     string `json:"file_url,omitempty"`
			}

			type siteDBRow struct {
				URLID       string `gorm:"column:url_id"`
				RawURL      string `gorm:"column:raw_url"`
				Normalized  string `gorm:"column:normalized"`
				Depth       int    `gorm:"column:depth"`
				Status      string `gorm:"column:status"`
				RetryCount  int    `gorm:"column:retry_count"`
				LastError   string `gorm:"column:last_error"`
				FoundOn     string `gorm:"column:found_on"`
				UpdatedAt   string `gorm:"column:updated_at"`
				PageID      string `gorm:"column:page_id"`
				HTTPStatus  int    `gorm:"column:http_status"`
				ContentType string `gorm:"column:content_type"`
				ContentPath string `gorm:"column:content_path"`
				ContentSize int64  `gorm:"column:content_size"`
				Title       string `gorm:"column:title"`
				FetchedAt   string `gorm:"column:fetched_at"`
			}

			jobID := chi.URLParam(req, "id")
			limit := parsePositiveInt(req.URL.Query().Get("limit"), 50)
			offset := parsePositiveInt(req.URL.Query().Get("offset"), 0)
			status := strings.TrimSpace(req.URL.Query().Get("status"))
			queryText := strings.TrimSpace(req.URL.Query().Get("q"))
			content := strings.TrimSpace(req.URL.Query().Get("content"))
			depth := strings.TrimSpace(req.URL.Query().Get("depth"))

			baseQuery := db.WithContext(req.Context()).
				Table("urls").
				Joins("LEFT JOIN pages ON pages.url_id = urls.id").
				Where("urls.job_id = ?", jobID)

			if status != "" && status != "all" {
				baseQuery = baseQuery.Where("urls.status = ?", status)
			}
			if content == "stored" {
				baseQuery = baseQuery.Where("pages.content_path <> ''")
			}
			if queryText != "" {
				like := "%" + strings.ToLower(queryText) + "%"
				baseQuery = baseQuery.Where(
					"lower(urls.normalized) LIKE ? OR lower(urls.raw_url) LIKE ? OR lower(pages.title) LIKE ?",
					like,
					like,
					like,
				)
			}
			if depth != "" && depth != "all" {
				baseQuery = baseQuery.Where("urls.depth = ?", parsePositiveInt(depth, 0))
			}

			var total int64
			if err := baseQuery.Session(&gorm.Session{}).Count(&total).Error; err != nil {
				writeError(w, err, http.StatusInternalServerError)
				return
			}

			var rows []siteDBRow
			err := baseQuery.
				Select(`
					urls.id AS url_id,
					urls.raw_url,
					urls.normalized,
					urls.depth,
					urls.status,
					urls.retry_count,
					urls.last_error,
					urls.found_on,
					urls.updated_at,
					pages.id AS page_id,
					pages.http_status,
					pages.content_type,
					pages.content_path,
					pages.content_size,
					pages.title,
					pages.fetched_at
				`).
				Order("urls.depth ASC, urls.normalized ASC").
				Limit(limit).
				Offset(offset).
				Scan(&rows).Error
			if err != nil {
				writeError(w, err, http.StatusInternalServerError)
				return
			}

			items := make([]siteRow, 0, len(rows))
			for _, row := range rows {
				item := siteRow{
					URLID:       row.URLID,
					RawURL:      row.RawURL,
					Normalized:  row.Normalized,
					Depth:       row.Depth,
					Status:      row.Status,
					RetryCount:  row.RetryCount,
					LastError:   row.LastError,
					FoundOn:     row.FoundOn,
					UpdatedAt:   row.UpdatedAt,
					PageID:      row.PageID,
					HTTPStatus:  row.HTTPStatus,
					ContentType: row.ContentType,
					ContentPath: row.ContentPath,
					ContentSize: row.ContentSize,
					Title:       row.Title,
					FetchedAt:   row.FetchedAt,
				}
				if row.PageID != "" && row.ContentPath != "" {
					item.FileURL = fmt.Sprintf("/api/jobs/%s/pages/%s/content", jobID, row.PageID)
				}
				items = append(items, item)
			}

			writeJSON(w, map[string]any{
				"items":   items,
				"limit":   limit,
				"offset":  offset,
				"total":   total,
				"status":  status,
				"query":   queryText,
				"content": content,
				"depth":   depth,
			})
		})

		r.Get("/jobs/{id}/exceptions", func(w http.ResponseWriter, req *http.Request) {
			id := chi.URLParam(req, "id")
			limit := parsePositiveInt(req.URL.Query().Get("limit"), 50)
			offset := parsePositiveInt(req.URL.Query().Get("offset"), 0)

			items, err := urlRepo.FindByJobIDAndStatuses(
				req.Context(),
				id,
				[]entities.URLStatus{entities.URLStatusError, entities.URLStatusBlocked},
				limit,
				offset,
			)
			if err != nil {
				writeError(w, err, http.StatusInternalServerError)
				return
			}

			writeJSON(w, map[string]any{
				"items":  items,
				"limit":  limit,
				"offset": offset,
			})
		})

		r.Get("/gui/dashboard", datastarHandlers.handleDashboard)
		r.Post("/gui/jobs", datastarHandlers.handleCreateJob)
		r.Post("/gui/jobs/{id}/{action}", datastarHandlers.handleJobAction)
		r.Get("/gui/events", datastarHandlers.handleEvents)
		r.Get("/gui/clock", datastarHandlers.handleClock)

		// Export.
		r.Get("/jobs/{id}/export", func(w http.ResponseWriter, req *http.Request) {
			id := chi.URLParam(req, "id")
			format := req.URL.Query().Get("format")
			if format == "" {
				format = "json"
			}

			switch format {
			case "csv":
				w.Header().Set("Content-Type", "text/csv")
				w.Header().Set("Content-Disposition", "attachment; filename=export.csv")
			case "sitemap":
				w.Header().Set("Content-Type", "application/xml")
				w.Header().Set("Content-Disposition", "attachment; filename=sitemap.xml")
			default:
				w.Header().Set("Content-Type", "application/json")
			}

			err := exportSvc.Export(req.Context(), ports.ExportFormat(format), ports.ExportFilter{JobID: id}, w)
			if err != nil {
				writeError(w, err, http.StatusInternalServerError)
			}
		})

		// SSE.
		r.Get("/events", sseBroker.ServeHTTP)

		// Health.
		r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, map[string]string{"status": "ok"})
		})
	})

	staticFS, _ := fs.Sub(web.StaticFS, "static")
	fileServer := http.FileServer(http.FS(staticFS))
	r.Handle("/assets/*", http.StripPrefix("/assets/", fileServer))
	r.Get("/", templ.Handler(DashboardPage()).ServeHTTP)

	return &Router{
		handler: r,
		sse:     sseBroker,
		sub:     sub,
	}
}

// ServeHTTP delegates to the underlying chi router.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.handler.ServeHTTP(w, req)
}

// Close releases long-lived GUI resources.
func (r *Router) Close() error {
	if r.sse != nil {
		r.sse.Close()
	}
	if r.sub != nil {
		if err := r.sub.Unsubscribe(); err != nil {
			return err
		}
	}
	return nil
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error, code int) {
	w.WriteHeader(code)
	msg := "not found"
	if err != nil {
		msg = err.Error()
	}
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func parsePositiveInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	var v int
	if _, err := fmt.Sscanf(raw, "%d", &v); err != nil || v < 0 {
		return fallback
	}
	return v
}
