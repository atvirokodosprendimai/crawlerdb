package gui

import (
	"encoding/json"
	"log/slog"
	"net/http"

	store "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/db/gorm"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
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

		// Health.
		r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, map[string]string{"status": "ok"})
		})
	})

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
