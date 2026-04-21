package gui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	store "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/db/gorm"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
	"gorm.io/gorm"
)

const (
	exceptionPageSize = 25
	sitePageSize      = 50
)

type dashboardSignals struct {
	SelectedJobID    string `json:"selectedJobId"`
	ExceptionsOffset int    `json:"exceptionsOffset"`
	SiteOffset       int    `json:"siteOffset"`
	SiteQuery        string `json:"siteQuery"`
	SiteStatus       string `json:"siteStatus"`
	SiteContent      string `json:"siteContent"`
	SiteDepth        string `json:"siteDepth"`
	SeedURL          string `json:"seedUrl"`
	Scope            string `json:"scope"`
	MaxDepth         int    `json:"maxDepth"`
}

type dashboardOverviewItem struct {
	Job            *entities.Job
	Counts         map[string]int
	ExceptionCount int
}

type dashboardGlobalStats struct {
	Incidents    int
	Running      int
	Queue        int
	SelectedID   string
	SelectedNote string
}

type dashboardControl struct {
	Label  string
	Class  string
	Action string
}

type dashboardSignalCard struct {
	Value string
	Label string
	Note  string
	Hot   bool
}

type dashboardSelectedView struct {
	Job         *entities.Job
	Counts      map[string]int
	Controls    []dashboardControl
	SignalCards []dashboardSignalCard
	SettingsURL string
}

type dashboardExceptionView struct {
	Items        []*entities.CrawlURL
	PageNote     string
	EmptyMessage string
	PrevAction   string
	NextAction   string
	PrevDisabled bool
	NextDisabled bool
}

type dashboardSiteRow struct {
	URLID       string
	Normalized  string
	Depth       int
	Status      string
	RetryCount  int
	LastError   string
	FoundOn     string
	UpdatedAt   time.Time
	PageID      string
	HTTPStatus  int
	ContentType string
	ContentPath string
	ContentSize int64
	Title       string
	FetchedAt   time.Time
	FileURL     string
}

type dashboardSiteView struct {
	Items        []dashboardSiteRow
	PageNote     string
	EmptyMessage string
	PrevAction   string
	NextAction   string
	PrevDisabled bool
	NextDisabled bool
}

type dashboardView struct {
	Overview   []dashboardOverviewItem
	Global     dashboardGlobalStats
	Selected   *dashboardSelectedView
	Exceptions dashboardExceptionView
	Site       dashboardSiteView
}

type dashboardStateLoader struct {
	db      *gorm.DB
	jobRepo ports.JobRepository
	urlRepo ports.URLRepository
}

func defaultDashboardSignals() dashboardSignals {
	return dashboardSignals{
		SiteStatus:  "all",
		SiteContent: "all",
		SiteDepth:   "all",
		Scope:       "same_domain",
		MaxDepth:    3,
	}
}

func normalizeDashboardSignals(signals *dashboardSignals) {
	if signals.SiteStatus == "" {
		signals.SiteStatus = "all"
	}
	if signals.SiteContent == "" {
		signals.SiteContent = "all"
	}
	if signals.SiteDepth == "" {
		signals.SiteDepth = "all"
	}
	if signals.Scope == "" {
		signals.Scope = "same_domain"
	}
	if signals.MaxDepth <= 0 {
		signals.MaxDepth = 3
	}
	if signals.ExceptionsOffset < 0 {
		signals.ExceptionsOffset = 0
	}
	if signals.SiteOffset < 0 {
		signals.SiteOffset = 0
	}
}

func (l *dashboardStateLoader) load(ctx context.Context, signals *dashboardSignals) (dashboardView, error) {
	jobs, err := l.jobRepo.List(ctx, 100, 0)
	if err != nil {
		return dashboardView{}, err
	}

	overview := make([]dashboardOverviewItem, 0, len(jobs))
	for _, job := range jobs {
		counts, err := l.urlRepo.CountByStatus(ctx, job.ID)
		if err != nil {
			return dashboardView{}, err
		}
		flat := flattenCounts(counts)
		overview = append(overview, dashboardOverviewItem{
			Job:            job,
			Counts:         flat,
			ExceptionCount: flat["error"] + flat["blocked"],
		})
	}
	sortOverviewItems(overview)

	selectedItem := findSelectedOverviewItem(overview, signals.SelectedJobID)
	if selectedItem == nil && len(overview) > 0 {
		selectedItem = &overview[0]
		signals.SelectedJobID = overview[0].Job.ID
	}

	view := dashboardView{
		Overview: overview,
		Global:   buildGlobalStats(overview, signals.SelectedJobID),
	}

	if selectedItem == nil {
		view.Global.SelectedID = "None"
		view.Global.SelectedNote = "Choose a job to open its exception desk."
		view.Exceptions = dashboardExceptionView{
			EmptyMessage: "Select a job to inspect its exception queue.",
			PageNote:     "Page 1",
		}
		view.Site = dashboardSiteView{
			EmptyMessage: "Select a job to browse its URLs and stored content.",
			PageNote:     "Page 1",
		}
		return view, nil
	}

	selectedCounts := selectedItem.Counts
	view.Global.SelectedID = shortID(selectedItem.Job.ID)
	view.Global.SelectedNote = fmt.Sprintf("%d open incidents", selectedItem.ExceptionCount)
	view.Selected = buildSelectedView(selectedItem.Job, selectedCounts)
	view.Exceptions, err = l.loadExceptions(ctx, signals, selectedItem.Job.ID, selectedCounts)
	if err != nil {
		return dashboardView{}, err
	}
	view.Site, err = l.loadSite(ctx, signals, selectedItem.Job.ID)
	if err != nil {
		return dashboardView{}, err
	}
	return view, nil
}

func (l *dashboardStateLoader) loadExceptions(ctx context.Context, signals *dashboardSignals, jobID string, counts map[string]int) (dashboardExceptionView, error) {
	items, err := l.urlRepo.FindByJobIDAndStatuses(
		ctx,
		jobID,
		[]entities.URLStatus{entities.URLStatusError, entities.URLStatusBlocked},
		exceptionPageSize,
		signals.ExceptionsOffset,
	)
	if err != nil {
		return dashboardExceptionView{}, err
	}
	total := counts["error"] + counts["blocked"]
	page := (signals.ExceptionsOffset / exceptionPageSize) + 1
	view := dashboardExceptionView{
		Items:        items,
		PageNote:     fmt.Sprintf("Page %d · showing %d of %d incidents", page, len(items), total),
		EmptyMessage: "No blocked or failed URLs in this job.",
		PrevAction:   exceptionsPageExpr(signals.ExceptionsOffset - exceptionPageSize),
		NextAction:   exceptionsPageExpr(signals.ExceptionsOffset + exceptionPageSize),
		PrevDisabled: signals.ExceptionsOffset == 0,
		NextDisabled: signals.ExceptionsOffset+exceptionPageSize >= total,
	}
	return view, nil
}

func (l *dashboardStateLoader) loadSite(ctx context.Context, signals *dashboardSignals, jobID string) (dashboardSiteView, error) {
	query := l.db.WithContext(ctx).
		Table("urls").
		Joins("LEFT JOIN pages ON pages.url_id = urls.id").
		Where("urls.job_id = ?", jobID)

	if signals.SiteStatus != "" && signals.SiteStatus != "all" {
		query = query.Where("urls.status = ?", signals.SiteStatus)
	}
	if signals.SiteContent == "stored" {
		query = query.Where("pages.content_path <> ''")
	}
	if q := strings.TrimSpace(signals.SiteQuery); q != "" {
		like := "%" + strings.ToLower(q) + "%"
		query = query.Where(
			"lower(urls.normalized) LIKE ? OR lower(urls.raw_url) LIKE ? OR lower(pages.title) LIKE ?",
			like,
			like,
			like,
		)
	}
	if signals.SiteDepth != "" && signals.SiteDepth != "all" {
		query = query.Where("urls.depth = ?", signals.SiteDepth)
	}

	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return dashboardSiteView{}, err
	}

	type siteDBRow struct {
		URLID       string    `gorm:"column:url_id"`
		Normalized  string    `gorm:"column:normalized"`
		Depth       int       `gorm:"column:depth"`
		Status      string    `gorm:"column:status"`
		RetryCount  int       `gorm:"column:retry_count"`
		LastError   string    `gorm:"column:last_error"`
		FoundOn     string    `gorm:"column:found_on"`
		UpdatedAt   time.Time `gorm:"column:updated_at"`
		PageID      string    `gorm:"column:page_id"`
		HTTPStatus  int       `gorm:"column:http_status"`
		ContentType string    `gorm:"column:content_type"`
		ContentPath string    `gorm:"column:content_path"`
		ContentSize int64     `gorm:"column:content_size"`
		Title       string    `gorm:"column:title"`
		FetchedAt   time.Time `gorm:"column:fetched_at"`
	}

	var rows []siteDBRow
	err := query.
		Select(`
			urls.id AS url_id,
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
		Limit(sitePageSize).
		Offset(signals.SiteOffset).
		Scan(&rows).Error
	if err != nil {
		return dashboardSiteView{}, err
	}

	items := make([]dashboardSiteRow, 0, len(rows))
	for _, row := range rows {
		item := dashboardSiteRow{
			URLID:       row.URLID,
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

	page := (signals.SiteOffset / sitePageSize) + 1
	view := dashboardSiteView{
		Items:        items,
		PageNote:     fmt.Sprintf("Page %d · showing %d of %d URLs", page, len(items), total),
		EmptyMessage: "No URLs match the current explorer filters.",
		PrevAction:   sitePageExpr(signals.SiteOffset - sitePageSize),
		NextAction:   sitePageExpr(signals.SiteOffset + sitePageSize),
		PrevDisabled: signals.SiteOffset == 0,
		NextDisabled: signals.SiteOffset+sitePageSize >= int(total),
	}
	return view, nil
}

func flattenCounts(counts map[entities.URLStatus]int) map[string]int {
	flat := make(map[string]int, len(counts))
	for status, count := range counts {
		flat[string(status)] = count
	}
	return flat
}

func sortOverviewItems(items []dashboardOverviewItem) {
	statusWeight := map[entities.JobStatus]int{
		entities.JobStatusRunning:   0,
		entities.JobStatusPaused:    1,
		entities.JobStatusFailed:    2,
		entities.JobStatusStopped:   3,
		entities.JobStatusPending:   4,
		entities.JobStatusCompleted: 5,
	}
	slices.SortFunc(items, func(a, b dashboardOverviewItem) int {
		if diff := b.ExceptionCount - a.ExceptionCount; diff != 0 {
			return diff
		}
		aw := statusWeight[a.Job.Status]
		bw := statusWeight[b.Job.Status]
		if aw != bw {
			return aw - bw
		}
		if a.Job.UpdatedAt.Equal(b.Job.UpdatedAt) {
			return 0
		}
		if a.Job.UpdatedAt.After(b.Job.UpdatedAt) {
			return -1
		}
		return 1
	})
}

func findSelectedOverviewItem(items []dashboardOverviewItem, selectedID string) *dashboardOverviewItem {
	for i := range items {
		if items[i].Job != nil && items[i].Job.ID == selectedID {
			return &items[i]
		}
	}
	return nil
}

func buildGlobalStats(items []dashboardOverviewItem, selectedID string) dashboardGlobalStats {
	stats := dashboardGlobalStats{
		SelectedID:   "None",
		SelectedNote: "Choose a job to open its exception desk.",
	}
	for _, item := range items {
		stats.Incidents += item.ExceptionCount
		if item.Job != nil && item.Job.Status == entities.JobStatusRunning {
			stats.Running++
		}
		stats.Queue += item.Counts["pending"] + item.Counts["crawling"]
	}
	if selectedID == "" {
		return stats
	}
	return stats
}

func buildSelectedView(job *entities.Job, counts map[string]int) *dashboardSelectedView {
	if job == nil {
		return nil
	}
	controls := make([]dashboardControl, 0, 5)
	switch job.Status {
	case entities.JobStatusRunning:
		controls = append(controls,
			dashboardControl{Label: "Pause", Class: "btn btn-warning", Action: jobActionExpr(job.ID, "pause")},
			dashboardControl{Label: "Stop", Class: "btn btn-danger", Action: jobActionExpr(job.ID, "stop")},
		)
	case entities.JobStatusPaused:
		controls = append(controls,
			dashboardControl{Label: "Resume", Class: "btn btn-primary", Action: jobActionExpr(job.ID, "resume")},
			dashboardControl{Label: "Stop", Class: "btn btn-danger", Action: jobActionExpr(job.ID, "stop")},
		)
	case entities.JobStatusFailed, entities.JobStatusStopped:
		controls = append(controls,
			dashboardControl{Label: "Retry", Class: "btn btn-primary", Action: jobActionExpr(job.ID, "retry")},
		)
	}
	controls = append(controls,
		dashboardControl{Label: "Dedupe URLs", Class: "btn btn-secondary", Action: jobActionExpr(job.ID, "dedupe")},
	)

	return &dashboardSelectedView{
		Job:         job,
		Counts:      counts,
		Controls:    controls,
		SettingsURL: "/jobs/" + job.ID + "/settings",
		SignalCards: []dashboardSignalCard{
			{
				Value: formatNumber(counts["error"] + counts["blocked"]),
				Label: "Open incidents",
				Note:  fmt.Sprintf("%d blocked / %d failed", counts["blocked"], counts["error"]),
				Hot:   true,
			},
			{
				Value: formatNumber(counts["pending"] + counts["crawling"]),
				Label: "Queue load",
				Note:  fmt.Sprintf("%d pending, %d crawling", counts["pending"], counts["crawling"]),
			},
			{
				Value: formatNumber(counts["done"]),
				Label: "Completed",
				Note:  "URLs that finished successfully",
			},
			{
				Value: string(job.Status),
				Label: "Job state",
				Note:  formatDateTime(job.UpdatedAt),
			},
		},
	}
}

func formatNumber(v int) string {
	return fmt.Sprintf("%d", v)
}

func statusClass(status string) string {
	return "status-chip status-" + status
}

func shortID(value string) string {
	if len(value) > 10 {
		return value[:10] + "…"
	}
	if value == "" {
		return "—"
	}
	return value
}

func formatDateTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

func formatBytes(size int64) string {
	switch {
	case size <= 0:
		return "—"
	case size < 1024:
		return fmt.Sprintf("%d B", size)
	case size < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	case size < 1024*1024*1024:
		return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
	default:
		return fmt.Sprintf("%.1f GB", float64(size)/(1024*1024*1024))
	}
}

func newJobSubmitExpr() string {
	return "@post('/api/gui/jobs')"
}

func refreshDashboardExpr() string {
	return "@get('/api/gui/dashboard')"
}

func selectJobExpr(id string) string {
	return fmt.Sprintf("$selectedJobId = '%s'; $exceptionsOffset = 0; $siteOffset = 0; @get('/api/gui/dashboard')", id)
}

func jobActionExpr(id, action string) string {
	return fmt.Sprintf("@post('/api/gui/jobs/%s/%s')", id, action)
}

func exceptionsNewestExpr() string {
	return "$exceptionsOffset = 0; @get('/api/gui/dashboard')"
}

func exceptionsPageExpr(offset int) string {
	if offset < 0 {
		offset = 0
	}
	return fmt.Sprintf("$exceptionsOffset = %d; @get('/api/gui/dashboard')", offset)
}

func applySiteFiltersExpr() string {
	return "$siteOffset = 0; @get('/api/gui/dashboard')"
}

func resetSiteFiltersExpr() string {
	return "$siteQuery = ''; $siteStatus = 'all'; $siteContent = 'all'; $siteDepth = 'all'; $siteOffset = 0; @get('/api/gui/dashboard')"
}

func sitePageExpr(offset int) string {
	if offset < 0 {
		offset = 0
	}
	return fmt.Sprintf("$siteOffset = %d; @get('/api/gui/dashboard')", offset)
}

func streamEventType(subject string) string {
	if strings.TrimSpace(subject) == "" {
		return "event"
	}
	return subject
}

func activeClass(selected *dashboardSelectedView, id string) string {
	if selected != nil && selected.Job != nil && selected.Job.ID == id {
		return "active"
	}
	return ""
}

func hotClass(hot bool) string {
	if hot {
		return "hot"
	}
	return ""
}

func foundOn(value string) string {
	if value == "" {
		return "seed"
	}
	return value
}

func exceptionReason(item *entities.CrawlURL) string {
	if item == nil || item.LastError == "" {
		return "No error message recorded"
	}
	return item.LastError
}

func lastUpdated(updatedAt time.Time, fetchedAt time.Time) string {
	if !updatedAt.IsZero() {
		return formatDateTime(updatedAt)
	}
	return formatDateTime(fetchedAt)
}

func pageRepoFromDB(db *gorm.DB) *store.PageRepository {
	return store.NewPageRepository(db)
}
