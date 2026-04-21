package gui

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/valueobj"
	"github.com/go-chi/chi/v5"
)

type jobSettingsView struct {
	Job            *entities.Job
	SeedURL        string
	Scope          string
	Extraction     string
	MaxDepth       int
	ExternalDepth  int
	RateLimit      string
	RevisitTTL     string
	MaxConcurrency int
	AntiBotMode    string
	UserAgent      string
	SuccessMessage string
	ErrorMessage   string
}

func newJobSettingsView(job *entities.Job) jobSettingsView {
	view := jobSettingsView{Job: job}
	if job == nil {
		return view
	}
	view.SeedURL = job.SeedURL
	view.Scope = string(job.Config.Scope)
	view.Extraction = string(job.Config.Extraction)
	view.MaxDepth = job.Config.MaxDepth
	view.ExternalDepth = job.Config.ExternalDepth
	view.RateLimit = durationString(job.Config.RateLimit)
	view.RevisitTTL = durationString(job.Config.RevisitTTL)
	view.MaxConcurrency = job.Config.MaxConcurrency
	view.AntiBotMode = string(job.Config.AntiBotMode)
	view.UserAgent = job.Config.UserAgent
	return view
}

func durationString(value valueobj.Duration) string {
	if value.Duration <= 0 {
		return ""
	}
	return value.Duration.String()
}

func (v jobSettingsView) crawlConfig() (valueobj.CrawlConfig, error) {
	rateLimit, err := parseOptionalDuration(v.RateLimit)
	if err != nil {
		return valueobj.CrawlConfig{}, fmt.Errorf("rate limit: %w", err)
	}
	revisitTTL, err := parseOptionalDuration(v.RevisitTTL)
	if err != nil {
		return valueobj.CrawlConfig{}, fmt.Errorf("revisit ttl: %w", err)
	}
	config := valueobj.CrawlConfig{
		Scope:          valueobj.CrawlScope(v.Scope),
		MaxDepth:       v.MaxDepth,
		ExternalDepth:  v.ExternalDepth,
		Extraction:     valueobj.ExtractionLevel(v.Extraction),
		RateLimit:      valueobj.Duration{Duration: rateLimit},
		MaxConcurrency: v.MaxConcurrency,
		AntiBotMode:    valueobj.AntiBotMode(v.AntiBotMode),
		UserAgent:      strings.TrimSpace(v.UserAgent),
		RevisitTTL:     valueobj.Duration{Duration: revisitTTL},
	}
	if err := config.Validate(); err != nil {
		return valueobj.CrawlConfig{}, err
	}
	return config, nil
}

func parseOptionalDuration(raw string) (time.Duration, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, nil
	}
	return time.ParseDuration(trimmed)
}

func readJobSettingsView(r *http.Request, job *entities.Job) (jobSettingsView, error) {
	view := newJobSettingsView(job)
	if err := r.ParseForm(); err != nil {
		return view, err
	}
	view.SeedURL = strings.TrimSpace(r.FormValue("seed_url"))
	view.Scope = strings.TrimSpace(r.FormValue("scope"))
	view.Extraction = strings.TrimSpace(r.FormValue("extraction"))
	view.RateLimit = strings.TrimSpace(r.FormValue("rate_limit"))
	view.RevisitTTL = strings.TrimSpace(r.FormValue("revisit_ttl"))
	view.AntiBotMode = strings.TrimSpace(r.FormValue("antibot_mode"))
	view.UserAgent = strings.TrimSpace(r.FormValue("user_agent"))

	var err error
	if view.MaxDepth, err = parseIntField(r.FormValue("max_depth")); err != nil {
		return view, fmt.Errorf("max depth: %w", err)
	}
	if view.ExternalDepth, err = parseIntField(r.FormValue("external_depth")); err != nil {
		return view, fmt.Errorf("external depth: %w", err)
	}
	if view.MaxConcurrency, err = parseIntField(r.FormValue("max_concurrency")); err != nil {
		return view, fmt.Errorf("max concurrency: %w", err)
	}
	return view, nil
}

func parseIntField(raw string) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, err
	}
	return value, nil
}

func handleJobSettingsPage(jobRepo ports.JobRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jobID := chi.URLParam(r, "id")
		job, err := jobRepo.FindByID(r.Context(), jobID)
		if err != nil {
			writeError(w, err, http.StatusInternalServerError)
			return
		}
		if job == nil {
			writeError(w, nil, http.StatusNotFound)
			return
		}
		view := newJobSettingsView(job)
		if r.URL.Query().Get("saved") == "1" {
			view.SuccessMessage = "Settings saved."
		}
		renderTempl(w, r, JobSettingsPage(view))
	}
}

func handleJobSettingsUpdate(jobRepo ports.JobRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jobID := chi.URLParam(r, "id")
		job, err := jobRepo.FindByID(r.Context(), jobID)
		if err != nil {
			writeError(w, err, http.StatusInternalServerError)
			return
		}
		if job == nil {
			writeError(w, nil, http.StatusNotFound)
			return
		}

		view, err := readJobSettingsView(r, job)
		if err != nil {
			view.ErrorMessage = err.Error()
			renderTempl(w, r, JobSettingsPage(view))
			return
		}

		config, err := view.crawlConfig()
		if err != nil {
			view.ErrorMessage = err.Error()
			renderTempl(w, r, JobSettingsPage(view))
			return
		}

		job.SeedURL = view.SeedURL
		job.Config = config
		job.UpdatedAt = time.Now().UTC()
		if err := jobRepo.Update(r.Context(), job); err != nil {
			view.ErrorMessage = err.Error()
			renderTempl(w, r, JobSettingsPage(view))
			return
		}

		http.Redirect(w, r, "/jobs/"+job.ID+"/settings?saved=1", http.StatusSeeOther)
	}
}
