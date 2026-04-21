package store

import (
	"encoding/json"
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/valueobj"
)

// --- Job converters ---

func jobToModel(j *entities.Job) (*JobModel, error) {
	cfgJSON, err := json.Marshal(j.Config)
	if err != nil {
		return nil, err
	}
	statsJSON, err := json.Marshal(j.Stats)
	if err != nil {
		return nil, err
	}
	m := &JobModel{
		ID:        j.ID,
		SeedURL:   j.SeedURL,
		Config:    string(cfgJSON),
		Status:    string(j.Status),
		Stats:     string(statsJSON),
		Error:     j.Error,
		CreatedAt: j.CreatedAt,
		UpdatedAt: j.UpdatedAt,
	}
	if !j.DeleteMarkedAt.IsZero() {
		t := j.DeleteMarkedAt
		m.DeleteMarkedAt = &t
	}
	if !j.StartedAt.IsZero() {
		t := j.StartedAt
		m.StartedAt = &t
	}
	if !j.FinishedAt.IsZero() {
		t := j.FinishedAt
		m.FinishedAt = &t
	}
	return m, nil
}

func modelToJob(m *JobModel) (*entities.Job, error) {
	var cfg valueobj.CrawlConfig
	if err := json.Unmarshal([]byte(m.Config), &cfg); err != nil {
		return nil, err
	}
	var stats entities.JobStats
	if err := json.Unmarshal([]byte(m.Stats), &stats); err != nil {
		return nil, err
	}
	j := &entities.Job{
		ID:        m.ID,
		SeedURL:   m.SeedURL,
		Config:    cfg,
		Status:    entities.JobStatus(m.Status),
		Stats:     stats,
		Error:     m.Error,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}
	if m.DeleteMarkedAt != nil {
		j.DeleteMarkedAt = *m.DeleteMarkedAt
	}
	if m.StartedAt != nil {
		j.StartedAt = *m.StartedAt
	}
	if m.FinishedAt != nil {
		j.FinishedAt = *m.FinishedAt
	}
	return j, nil
}

// --- URL converters ---

func urlToModel(u *entities.CrawlURL) *URLModel {
	m := &URLModel{
		ID:         u.ID,
		JobID:      u.JobID,
		RawURL:     u.RawURL,
		Normalized: u.Normalized,
		URLHash:    u.URLHash,
		Depth:      u.Depth,
		Status:     string(u.Status),
		RetryCount: u.RetryCount,
		LastError:  u.LastError,
		FoundOn:    u.FoundOn,
		CreatedAt:  u.CreatedAt,
		UpdatedAt:  u.UpdatedAt,
	}
	if !u.RevisitAt.IsZero() {
		t := u.RevisitAt
		m.RevisitAt = &t
	}
	return m
}

func modelToURL(m *URLModel) *entities.CrawlURL {
	u := &entities.CrawlURL{
		ID:         m.ID,
		JobID:      m.JobID,
		RawURL:     m.RawURL,
		Normalized: m.Normalized,
		URLHash:    m.URLHash,
		Depth:      m.Depth,
		Status:     entities.URLStatus(m.Status),
		RetryCount: m.RetryCount,
		LastError:  m.LastError,
		FoundOn:    m.FoundOn,
		CreatedAt:  m.CreatedAt,
		UpdatedAt:  m.UpdatedAt,
	}
	if m.RevisitAt != nil {
		u.RevisitAt = *m.RevisitAt
	}
	return u
}

// --- Page converters ---

func pageToModel(p *entities.Page) (*PageModel, error) {
	headersJSON, err := json.Marshal(p.Headers)
	if err != nil {
		return nil, err
	}
	metaJSON, err := json.Marshal(p.MetaTags)
	if err != nil {
		return nil, err
	}
	sdJSON, err := json.Marshal(p.StructuredData)
	if err != nil {
		return nil, err
	}
	linksJSON, err := json.Marshal(p.Links)
	if err != nil {
		return nil, err
	}

	return &PageModel{
		ID:             p.ID,
		URLID:          p.URLID,
		JobID:          p.JobID,
		HTTPStatus:     p.HTTPStatus,
		ContentType:    p.ContentType,
		ContentPath:    p.ContentPath,
		ContentSize:    p.ContentSize,
		Headers:        string(headersJSON),
		Title:          p.Title,
		MetaTags:       string(metaJSON),
		HTMLBody:       p.HTMLBody,
		TextContent:    p.TextContent,
		StructuredData: string(sdJSON),
		Links:          string(linksJSON),
		FetchDuration:  p.FetchDuration.Milliseconds(),
		FetchedAt:      p.FetchedAt,
		CreatedAt:      p.CreatedAt,
	}, nil
}

func modelToPage(m *PageModel) (*entities.Page, error) {
	var headers map[string]string
	if err := json.Unmarshal([]byte(m.Headers), &headers); err != nil {
		headers = map[string]string{}
	}
	var meta map[string]string
	if err := json.Unmarshal([]byte(m.MetaTags), &meta); err != nil {
		meta = map[string]string{}
	}
	var sd []any
	if err := json.Unmarshal([]byte(m.StructuredData), &sd); err != nil {
		sd = nil
	}
	var links []entities.DiscoveredLink
	if err := json.Unmarshal([]byte(m.Links), &links); err != nil {
		links = nil
	}

	return &entities.Page{
		ID:             m.ID,
		URLID:          m.URLID,
		JobID:          m.JobID,
		HTTPStatus:     m.HTTPStatus,
		ContentType:    m.ContentType,
		ContentPath:    m.ContentPath,
		ContentSize:    m.ContentSize,
		Headers:        headers,
		Title:          m.Title,
		MetaTags:       meta,
		HTMLBody:       m.HTMLBody,
		TextContent:    m.TextContent,
		StructuredData: sd,
		Links:          links,
		FetchDuration:  time.Duration(m.FetchDuration) * time.Millisecond,
		FetchedAt:      m.FetchedAt,
		CreatedAt:      m.CreatedAt,
	}, nil
}
