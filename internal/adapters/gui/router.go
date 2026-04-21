package gui

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"

	store "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/db/gorm"
	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/export"
	"github.com/atvirokodosprendimai/crawlerdb/internal/app/services"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
	"github.com/atvirokodosprendimai/crawlerdb/web"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"gorm.io/gorm"
)

// NewRouter creates the chi router with all API endpoints.
func NewRouter(db *gorm.DB, broker ports.MessageBroker, logger *slog.Logger) http.Handler {
	r := chi.NewRouter()

	// Middleware.
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.SetHeader("Content-Type", "application/json"))
	r.Use(corsMiddleware)

	jobRepo := store.NewJobRepository(db)
	pageRepo := store.NewPageRepository(db)
	urlRepo := store.NewURLRepository(db)

	exportSvc := services.NewExportService(
		export.NewJSONExporter(pageRepo),
		export.NewCSVExporter(pageRepo, urlRepo),
		export.NewSitemapExporter(urlRepo),
	)

	sseBroker := NewSSEBroker(logger)

	// Forward NATS GUI events to SSE.
	if broker != nil {
		_, _ = broker.Subscribe("gui.push.>", func(subj string, data []byte) error {
			sseBroker.Broadcast(subj, json.RawMessage(data))
			return nil
		})
	}

	// API routes.
	r.Route("/api", func(r chi.Router) {
		// Jobs.
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

	// Serve embedded static files.
	staticFS, _ := fs.Sub(web.StaticFS, "static")
	fileServer := http.FileServer(http.FS(staticFS))
	r.Handle("/*", fileServer)

	return r
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
