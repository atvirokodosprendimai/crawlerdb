---
tldr: Implementation plan for CrawlerDB — modular recursive web crawler with hexagonal architecture, NATS, SQLite, TDD
status: completed
---

# Plan: Implement CrawlerDB

## Context

- Spec: [[spec - crawlerdb - modular recursive web crawler with NATS orchestration]]
- Module: `github.com/atvirokodosprendimai/crawlerdb`
- Go 1.25.0, TDD-first, hexagonal architecture, DDD

## Phases

### Phase 1 - Project Foundation - status: completed

Skeleton: deps, dir structure, config system, shared utilities.

1. [x] Set up go.mod with all dependencies
   => glebarez/sqlite used instead of gorm.io/driver/sqlite for CGO-free builds
2. [x] Create full directory structure per spec
3. [x] Implement config system
   => Custom `valueobj.Duration` type created for TOML marshaling compatibility
   => configs/default.toml created with all sections
4. [x] Implement ULID generator utility
   => Thread-safe monotonic entropy, 100-goroutine concurrency test

### Phase 2 - Domain Model (TDD) - status: completed

Pure Go domain layer — zero external deps. All tests written first.

1. [x] Define domain entities: Job
   => 12 tests covering all valid/invalid state transitions
2. [x] Define domain entities: URL
   => 7 tests, state machine: pending -> crawling -> {done, blocked, error}
3. [x] Define domain entities: Page, CrawlResult
   => AntiBotDetection struct added to CrawlResult in Phase 10
4. [x] Define value objects
   => CrawlConfig with scope/extraction/antibot validation
5. [x] Define all port interfaces
   => URLRepository gained FindByJobID in Phase 11
6. [x] Implement URLNormalizer domain service
   => resolveDotSegments() for path normalization, ForceQuery fix
7. [x] Implement RobotsEvaluator domain service
   => longest-match precedence, crawl-delay extraction
8. [x] Define domain events

### Phase 3 - Database Layer - status: completed

GORM + goose migrations + repository implementations.

1. [x] Create goose SQL migrations (5 migrations)
   => Embedded via separate migrations/embed.go package
2. [x] Implement GORM models
3. [x] Implement DB connection + migration runner
   => WAL mode + foreign keys enabled
4. [x] Implement JobRepository
5. [x] Implement URLRepository
   => Atomic URL claiming via GORM transactions, ON CONFLICT DO NOTHING dedup
6. [x] Implement PageRepository
   => 12 integration tests against in-memory SQLite

### Phase 4 - NATS Messaging Adapter - status: completed

1. [x] Implement NATS connection manager
2. [x] Implement MessageBroker adapter
   => 5 tests with embedded NATS server
3. [x] Implement NATS subject builder
   => 13 subject constants matching spec

### Phase 5 - HTTP Fetcher + robots.txt - status: completed

1. [x] Implement static HTTP fetcher
   => Functional options: WithUserAgent, WithTimeout, WithTransport
   => 5 tests against httptest.Server
2. [x] Implement robots.txt fetcher + parser
   => User-agent matching (specific > wildcard), sitemap extraction
   => 6 parser tests
3. [x] Implement robots.txt cache (in-memory with TTL)
4. [x] Implement RobotsChecker adapter
   => HTTPS->HTTP fallback, 3 tests with cache verification

### Phase 6 - Link Extraction + Content Extraction - status: completed

1. [x] Implement HTML link extractor
   => ExtractLinks, ExtractTitle, ExtractMetaTags, ExtractText using x/net/html
   => 6 tests
2. [x] Implement content extraction profiles
   => Extractor.Extract() applies minimal/standard/full profiles
   => JSON-LD structured data extraction
   => 4 tests

### Phase 7 - Core Orchestrator - status: completed

1. [x] Implement JobService
   => CreateJob, StartJob, PauseJob, ResumeJob, StopJob, CompleteJob
2. [x] Implement CrawlService
   => DispatchURLs, ProcessResult, CheckCompletion, EnqueueSeedURL
   => Scope-aware URL filtering (same-domain, include-subdomains, follow-externals)
3. [x] Wire up cmd/core/main.go
   => NATS req/reply handlers, dispatch loop every 2s

### Phase 8 - Crawler Worker - status: completed

1. [x] Implement worker fetch loop
   => WorkerService with goroutine pool via semaphore + WaitGroup
2. [x] Implement per-domain rate limiter
   => Adaptive: adjusts on 429/500/latency signals, token bucket per domain
   => 5 tests
3. [x] Wire up cmd/crawler/main.go
   => NATS queue subscription, heartbeat every 10s

### Phase 9 - CLI (First End-to-End Milestone) - status: completed

1. [x] Implement urfave/cli v3 app scaffold
2. [x] Implement crawl commands (start/status/stop/pause/resume/list)
3. [x] Implement config commands (init/show)
4. [x] Implement db commands (migrate/status)
5. [x] Implement export CLI command
6. [x] Wire up all three entry points

### Phase 10 - Anti-Bot Detection - status: completed

1. [x] Implement anti-bot detector
   => checkStatusCode, checkCaptchaProviders (6 providers), checkJSChallenges (4 systems)
   => 2+ signals required for JS challenges to reduce false positives
   => 8 tests
2. [x] Implement response strategies
   => SkipStrategy, RotateStrategy, SolverStrategy with action types (retry/skip/backoff/solve)
   => 6 strategy tests
3. [x] Implement proxy pool
   => Round-robin, random, least-used rotation; MarkDead/MarkAlive; Transport()
   => 8 proxy pool tests
4. [x] Integrate anti-bot into worker fetch loop
   => Detector check after body read, before extraction

### Phase 11 - Data Export - status: completed

1. [x] Implement JSON exporter
2. [x] Implement CSV exporter
3. [x] Implement Sitemap XML generator
   => Standard XML sitemap with only "done" status URLs
4. [x] Implement ExportService
   => Format routing via registered exporters map
5. [x] Wire export into core NATS handler + CLI
   => 3 export tests

### Phase 12 - GUI Dashboard - status: completed

1. [x] Expand REST API endpoints
   => POST /api/jobs, POST /api/jobs/{id}/stop|pause|resume, GET /api/jobs/{id}/export
2. [x] Implement SSE endpoint
   => SSEBroker with fan-out to connected clients, NATS→SSE bridge
   => 2 SSE tests
3. [x] Build static frontend
   => Embedded HTML dashboard with job management, stats, events log, export
4. [x] Router tests
   => 4 router tests: health, empty list, not found, CORS
5. [x] Static file serving via embed.FS

## Verification

- [x] All unit tests pass (go test ./...)
- [x] Integration tests pass with embedded NATS + in-memory SQLite
- [x] robots.txt respected — disallowed URLs never fetched
- [x] Anti-bot detection fires on known challenge pages
- [x] Export produces valid JSON/CSV/Sitemap XML
- [x] GUI shows real-time progress via SSE
- [x] go build ./... clean (all three binaries compile)

## Adjustments

- 2026-04-21 21:15: SQLite dump exporter (Phase 11.3) skipped — JSON/CSV/Sitemap cover export needs. Can add later.
- 2026-04-21 21:15: Frontend views simplified to single-page embedded HTML instead of multi-view SPA — spec says "build static first".

## Progress Log

- 2026-04-21 20:37: Plan created
- 2026-04-21 20:40: Phase 1 completed — go.mod, dir structure, config, ULID
- 2026-04-21 20:45: Phase 2 completed — all domain entities, value objects, ports, services, events
- 2026-04-21 20:50: Phase 3 completed — 5 migrations, GORM models, 3 repositories, 12 integration tests
- 2026-04-21 20:55: Phase 4 completed — NATS broker adapter, subject builder, 5 tests
- 2026-04-21 21:00: Phase 5 completed — HTTP fetcher, robots.txt parser+checker+cache
- 2026-04-21 21:02: Phase 6 completed — link extractor, content extraction profiles
- 2026-04-21 21:05: Phase 7 completed — JobService, CrawlService, core orchestrator
- 2026-04-21 21:07: Phase 8 completed — WorkerService with goroutine pool, adaptive rate limiter
- 2026-04-21 21:08: Phase 9 completed — CLI app with crawl/config/db/export commands, all entry points
- 2026-04-21 21:11: Phase 10 completed — anti-bot detector, strategies, proxy pool, worker integration
- 2026-04-21 21:12: Phase 11 completed — JSON/CSV/Sitemap exporters, ExportService, NATS handler
- 2026-04-21 21:15: Phase 12 completed — SSE broker, expanded REST API, embedded frontend dashboard
- 2026-04-21 21:16: All 12 phases complete. Full test suite passes (14 packages, 80+ tests).
