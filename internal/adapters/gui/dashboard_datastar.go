package gui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/a-h/templ"
	store "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/db/gorm"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/valueobj"
	"github.com/go-chi/chi/v5"
	"github.com/starfederation/datastar-go/datastar"
	"gorm.io/gorm"
)

type datastarDashboardHandlers struct {
	db     *gorm.DB
	loader *dashboardStateLoader
	broker ports.MessageBroker
}

func newDatastarDashboardHandlers(db *gorm.DB, broker ports.MessageBroker, jobRepo ports.JobRepository, urlRepo ports.URLRepository) *datastarDashboardHandlers {
	return &datastarDashboardHandlers{
		db: db,
		loader: &dashboardStateLoader{
			db:      db,
			jobRepo: jobRepo,
			urlRepo: urlRepo,
		},
		broker: broker,
	}
}

func (h *datastarDashboardHandlers) handleDashboard(w http.ResponseWriter, r *http.Request) {
	signals, err := readDashboardSignals(r)
	if err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}

	view, err := h.loader.load(r.Context(), &signals)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}

	sse := datastar.NewSSE(w, r)
	patchDashboardSignals(sse, signals)
	if err := patchDashboardRoot(r.Context(), sse, view); err != nil {
		writeError(w, err, http.StatusInternalServerError)
	}
}

func (h *datastarDashboardHandlers) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	signals, err := readDashboardSignals(r)
	if err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	if h.broker == nil {
		writeError(w, fmt.Errorf("message broker unavailable"), http.StatusServiceUnavailable)
		return
	}
	if strings.TrimSpace(signals.SeedURL) == "" {
		writeError(w, fmt.Errorf("seed url is required"), http.StatusBadRequest)
		return
	}

	req := struct {
		SeedURL string               `json:"seed_url"`
		Config  valueobj.CrawlConfig `json:"config"`
	}{
		SeedURL: strings.TrimSpace(signals.SeedURL),
		Config: valueobj.CrawlConfig{
			Scope:          valueobj.CrawlScope(signals.Scope),
			MaxDepth:       signals.MaxDepth,
			Extraction:     valueobj.ExtractionStandard,
			UserAgent:      "CrawlerDB/1.0",
			MaxConcurrency: 1,
			RateLimit:      valueobj.Duration{1 * time.Second},
		},
	}
	if err := req.Config.Validate(); err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}

	body, _ := json.Marshal(req)
	reply, err := h.broker.Request(r.Context(), "job.create", body)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}

	var resp struct {
		JobID string `json:"job_id"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(reply, &resp); err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	if resp.Error != "" {
		writeError(w, fmt.Errorf("%s", resp.Error), http.StatusBadRequest)
		return
	}

	signals.SelectedJobID = resp.JobID
	signals.ExceptionsOffset = 0
	signals.SiteOffset = 0
	signals.SeedURL = ""

	view, err := h.loader.load(r.Context(), &signals)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}

	sse := datastar.NewSSE(w, r)
	patchDashboardSignals(sse, signals)
	if err := patchDashboardRoot(r.Context(), sse, view); err != nil {
		writeError(w, err, http.StatusInternalServerError)
	}
}

func (h *datastarDashboardHandlers) handleJobAction(w http.ResponseWriter, r *http.Request) {
	signals, err := readDashboardSignals(r)
	if err != nil {
		signals = defaultDashboardSignals()
	}

	jobID := chi.URLParam(r, "id")

	action := chi.URLParam(r, "action")
	if action == "delete" {
		if err := store.NewJobRepository(h.db).MarkForDeletion(r.Context(), jobID, time.Now().UTC()); err != nil {
			writeError(w, err, http.StatusInternalServerError)
			return
		}
		signals.SelectedJobID = jobID
		signals.ExceptionsOffset = 0
		signals.SiteOffset = 0

		view, err := h.loader.load(r.Context(), &signals)
		if err != nil {
			writeError(w, err, http.StatusInternalServerError)
			return
		}

		sse := datastar.NewSSE(w, r)
		patchDashboardSignals(sse, signals)
		if err := patchDashboardRoot(r.Context(), sse, view); err != nil {
			writeError(w, err, http.StatusInternalServerError)
		}
		return
	}
	if action == "dedupe" {
		if _, err := store.NewURLRepository(h.db).DedupeJobURLs(r.Context(), jobID); err != nil {
			writeError(w, err, http.StatusInternalServerError)
			return
		}
		signals.SelectedJobID = jobID
		signals.ExceptionsOffset = 0
		signals.SiteOffset = 0

		view, err := h.loader.load(r.Context(), &signals)
		if err != nil {
			writeError(w, err, http.StatusInternalServerError)
			return
		}

		sse := datastar.NewSSE(w, r)
		patchDashboardSignals(sse, signals)
		if err := patchDashboardRoot(r.Context(), sse, view); err != nil {
			writeError(w, err, http.StatusInternalServerError)
		}
		return
	}

	if h.broker == nil {
		writeError(w, fmt.Errorf("message broker unavailable"), http.StatusServiceUnavailable)
		return
	}

	subject, err := actionSubject(action)
	if err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}

	reqBody, _ := json.Marshal(map[string]string{"job_id": jobID})
	reply, err := h.broker.Request(r.Context(), subject, reqBody)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}

	var resp map[string]any
	if err := json.Unmarshal(reply, &resp); err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	if msg, _ := resp["error"].(string); msg != "" {
		writeError(w, fmt.Errorf("%s", msg), http.StatusBadRequest)
		return
	}

	if action == "retry" {
		if newID, _ := resp["job_id"].(string); newID != "" {
			signals.SelectedJobID = newID
		}
	} else {
		signals.SelectedJobID = jobID
	}
	signals.ExceptionsOffset = 0
	signals.SiteOffset = 0

	view, err := h.loader.load(r.Context(), &signals)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}

	sse := datastar.NewSSE(w, r)
	patchDashboardSignals(sse, signals)
	if err := patchDashboardRoot(r.Context(), sse, view); err != nil {
		writeError(w, err, http.StatusInternalServerError)
	}
}

func (h *datastarDashboardHandlers) handleEvents(w http.ResponseWriter, r *http.Request) {
	if _, err := readDashboardSignals(r); err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	if h.broker == nil {
		writeError(w, fmt.Errorf("message broker unavailable"), http.StatusServiceUnavailable)
		return
	}

	events := make(chan struct {
		subject string
		body    string
	}, 64)

	subjects := []string{
		"crawl.result.>",
		"job.created",
		"job.updated",
		"url.discovered",
		"url.blocked",
		"captcha.detected",
		"captcha.solved",
	}
	subs := make([]ports.Subscription, 0, len(subjects))
	for _, subject := range subjects {
		sub, err := h.broker.Subscribe(subject, func(subject string, data []byte) error {
			body := string(data)
			if json.Valid(data) {
				var pretty bytes.Buffer
				if err := json.Indent(&pretty, data, "", "  "); err == nil {
					body = pretty.String()
				}
			}
			select {
			case events <- struct {
				subject string
				body    string
			}{subject: subject, body: body}:
			default:
			}
			return nil
		})
		if err != nil {
			for _, existing := range subs {
				_ = existing.Unsubscribe()
			}
			writeError(w, err, http.StatusInternalServerError)
			return
		}
		subs = append(subs, sub)
	}
	defer func() {
		for _, sub := range subs {
			_ = sub.Unsubscribe()
		}
	}()

	sse := datastar.NewSSE(w, r)
	removedEmpty := false
	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-events:
			if !removedEmpty {
				_ = sse.RemoveElement("#event-stream-empty")
				removedEmpty = true
			}
			html, err := renderComponent(r.Context(), StreamItem(streamEventType(event.subject), event.body, time.Now().Format("15:04:05")))
			if err != nil {
				return
			}
			_ = sse.PatchElements(html, datastar.WithSelector("#event-stream"), datastar.WithMode(datastar.ElementPatchModePrepend))
		}
	}
}

func (h *datastarDashboardHandlers) handleClock(w http.ResponseWriter, r *http.Request) {
	if _, err := readDashboardSignals(r); err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}

	sse := datastar.NewSSE(w, r)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		display := time.Now().Local().Format("2006-01-02 15:04:05 MST")
		if err := sse.PatchElementTempl(ClockValue(display), datastar.WithSelector("#dashboard-clock"), datastar.WithMode(datastar.ElementPatchModeInner)); err != nil {
			return
		}

		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func readDashboardSignals(r *http.Request) (dashboardSignals, error) {
	signals := defaultDashboardSignals()
	hasSignals := r.Method != http.MethodGet || r.URL.Query().Get(datastar.DatastarKey) != ""
	if !hasSignals {
		return signals, nil
	}
	if err := datastar.ReadSignals(r, &signals); err != nil && !errors.Is(err, context.Canceled) {
		return dashboardSignals{}, err
	}
	normalizeDashboardSignals(&signals)
	return signals, nil
}

func patchDashboardSignals(sse *datastar.ServerSentEventGenerator, signals dashboardSignals) {
	_ = sse.MarshalAndPatchSignals(map[string]any{
		"selectedJobId":    signals.SelectedJobID,
		"exceptionsOffset": signals.ExceptionsOffset,
		"siteOffset":       signals.SiteOffset,
		"siteQuery":        signals.SiteQuery,
		"siteStatus":       signals.SiteStatus,
		"siteContent":      signals.SiteContent,
		"siteDepth":        signals.SiteDepth,
		"seedUrl":          signals.SeedURL,
		"scope":            signals.Scope,
		"maxDepth":         signals.MaxDepth,
	})
}

func patchDashboardRoot(ctx context.Context, sse *datastar.ServerSentEventGenerator, view dashboardView) error {
	html, err := renderComponent(ctx, DashboardRoot(view))
	if err != nil {
		return err
	}
	sse.PatchElements(html, datastar.WithSelector("#dashboard-root"), datastar.WithMode(datastar.ElementPatchModeInner))
	return nil
}

func renderComponent(ctx context.Context, component templ.Component) (string, error) {
	var buf bytes.Buffer
	if err := component.Render(ctx, &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func actionSubject(action string) (string, error) {
	switch action {
	case "stop":
		return "job.stop", nil
	case "pause":
		return "job.pause", nil
	case "resume":
		return "job.resume", nil
	case "retry":
		return "job.retry", nil
	default:
		return "", fmt.Errorf("unsupported action: %s", action)
	}
}
