---
tldr: Implementation plan for CrawlerDB — modular recursive web crawler with hexagonal architecture, NATS, SQLite, TDD
status: active
---

# Plan: Implement CrawlerDB

## Context

- Spec: [[spec - crawlerdb - modular recursive web crawler with NATS orchestration]]
- Module: `github.com/atvirokodosprendimai/crawlerdb`
- Go 1.25.0, TDD-first, hexagonal architecture, DDD

## Phases

### Phase 1 - Project Foundation - status: open

Skeleton: deps, dir structure, config system, shared utilities.

1. [ ] Set up go.mod with all dependencies
   - modernc.org/sqlite, gorm.io/gorm, gorm.io/driver/sqlite (modernc wrapper)
   - github.com/nats-io/nats.go
   - github.com/go-chi/chi/v5
   - github.com/urfave/cli/v3
   - github.com/pressly/goose/v3
   - github.com/oklog/ulid/v2
   - github.com/pelletier/go-toml/v2 (config)
   - github.com/stretchr/testify (test assertions)
2. [ ] Create full directory structure per spec
   - internal/domain/{entities,valueobj,ports,services,events}
   - internal/app/{commands,queries,services}
   - internal/adapters/{db/gorm,db/migrations,nats,http,robots,antibot,export,cli,gui}
   - cmd/{core,crawler,gui}
   - configs/
3. [ ] Implement config system
   - TOML config file with defaults
   - `configs/default.toml` template
   - Config struct in `internal/domain/valueobj/config.go`
   - Config loader in `internal/adapters/config/`
   - Tests first
4. [ ] Implement ULID generator utility
   - `pkg/uid/uid.go` — NewID() returning ULID string
   - Thread-safe entropy source
   - Tests first

### Phase 2 - Domain Model (TDD) - status: open

Pure Go domain layer — zero external deps. All tests written first.

1. [ ] Define domain entities: Job
   - `internal/domain/entities/job.go`
   - Job struct with ID, SeedURL, Config, Status, timestamps, Stats
   - Job state machine: pending -> running -> {paused, completed, failed, stopped}
   - State transition methods with validation
   - Tests first: all valid/invalid transitions
2. [ ] Define domain entities: URL
   - `internal/domain/entities/url.go`
   - URL struct with ID, JobID, RawURL, Normalized, Hash, Depth, Status, RetryCount, RevisitAt, FoundOn
   - Status: pending -> crawling -> {done, blocked, error}
   - Tests first
3. [ ] Define domain entities: Page, CrawlResult
   - `internal/domain/entities/page.go`
   - `internal/domain/entities/crawl_result.go`
   - Page: HTTPStatus, ContentType, Headers, Title, MetaTags, HTMLBody, TextContent, StructuredData, Links
   - CrawlResult: wraps Page + discovered URLs + fetch metadata
   - Tests first
4. [ ] Define value objects
   - `internal/domain/valueobj/crawl_config.go` — scope, depth, rate limits, extraction, anti-bot
   - `internal/domain/valueobj/extraction_profile.go` — minimal/standard/full
   - `internal/domain/valueobj/normalized_url.go` — normalized URL + SHA-256 hash
   - `internal/domain/valueobj/antibot_strategy.go` — detection + response config
   - `internal/domain/valueobj/robots_policy.go` — parsed rules per domain
   - Validation methods on each
   - Tests first for all
5. [ ] Define all port interfaces
   - `internal/domain/ports/repositories.go` — JobRepository, URLRepository, PageRepository
   - `internal/domain/ports/fetcher.go` — Fetcher interface
   - `internal/domain/ports/broker.go` — MessageBroker (publish, subscribe, request)
   - `internal/domain/ports/robots.go` — RobotsChecker
   - `internal/domain/ports/antibot.go` — AntiBotDetector, CaptchaSolver
   - `internal/domain/ports/exporter.go` — Exporter
   - `internal/domain/ports/ratelimiter.go` — RateLimiter
6. [ ] Implement URLNormalizer domain service
   - `internal/domain/services/url_normalizer.go`
   - Lowercase host, sort query params, strip fragments, resolve relative, trailing slashes
   - SHA-256 hash computation
   - Tests first: comprehensive edge cases (IDN, encoding, relative paths, anchors, empty query)
7. [ ] Implement RobotsEvaluator domain service
   - `internal/domain/services/robots_evaluator.go`
   - Evaluate URL against RobotsPolicy
   - Match user-agent, check Allow/Disallow precedence, extract Crawl-delay
   - Tests first: real-world robots.txt samples
8. [ ] Define domain events
   - `internal/domain/events/events.go`
   - JobCreated, JobUpdated, URLDiscovered, URLBlocked, CaptchaDetected, CaptchaSolved, MetricsUpdated
   - Event structs with timestamps + payload

### Phase 3 - Database Layer - status: open

GORM + goose migrations + repository implementations.

1. [ ] Create goose SQL migrations
   - `internal/adapters/db/migrations/00001_create_jobs.sql`
   - `internal/adapters/db/migrations/00002_create_urls.sql`
   - `internal/adapters/db/migrations/00003_create_pages.sql`
   - `internal/adapters/db/migrations/00004_create_robots_cache.sql`
   - `internal/adapters/db/migrations/00005_create_antibot_events.sql`
   - All per spec SQL schema, with indexes
2. [ ] Implement GORM models
   - `internal/adapters/db/gorm/models.go`
   - GORM structs mapping to SQL tables
   - JSON field handling for config, headers, links, etc.
3. [ ] Implement DB connection + migration runner
   - `internal/adapters/db/gorm/database.go`
   - Open SQLite via modernc driver, GORM setup
   - Goose migration runner integration
   - Tests: migration up/down cycle
4. [ ] Implement JobRepository
   - `internal/adapters/db/gorm/job_repository.go`
   - Create, FindByID, Update, List, FindByStatus
   - Tests first: against in-memory SQLite
5. [ ] Implement URLRepository
   - `internal/adapters/db/gorm/url_repository.go`
   - Enqueue, Claim (atomic status update), Complete, FindPending, FindByHash, CountByStatus
   - Tests first: concurrent claim safety, dedup by hash
6. [ ] Implement PageRepository
   - `internal/adapters/db/gorm/page_repository.go`
   - Store, FindByURLID, FindByJobID, Search
   - Tests first

### Phase 4 - NATS Messaging Adapter - status: open

MessageBroker implementation over NATS.

1. [ ] Implement NATS connection manager
   - `internal/adapters/nats/connection.go`
   - Connect, reconnect handling, graceful shutdown
   - Tests with embedded NATS server (github.com/nats-io/nats-server/v2/test)
2. [ ] Implement MessageBroker adapter
   - `internal/adapters/nats/broker.go`
   - Publish, Subscribe, QueueSubscribe, Request, Reply
   - Subject constants from spec table
   - JSON serialization of domain events
   - Tests first: pub/sub round-trip, queue group distribution, request/reply
3. [ ] Implement NATS subject builder
   - `internal/adapters/nats/subjects.go`
   - Type-safe subject construction: crawl.dispatch.{job_id}, metrics.{job_id}, etc.
   - Tests

### Phase 5 - HTTP Fetcher + robots.txt - status: open

Static HTTP fetching and robots.txt compliance.

1. [ ] Implement static HTTP fetcher
   - `internal/adapters/http/fetcher.go`
   - Implements Fetcher port
   - Configurable User-Agent, timeouts, redirect policy
   - Response wrapping into domain types
   - Tests first: against httptest.Server
2. [ ] Implement robots.txt fetcher + parser
   - `internal/adapters/robots/fetcher.go` — fetch robots.txt for domain
   - `internal/adapters/robots/parser.go` — parse into RobotsPolicy
   - Handle missing robots.txt (allow all), malformed (best effort)
   - Extract Sitemap directives
   - Tests first: real-world robots.txt samples (Google, Amazon, etc.)
3. [ ] Implement robots.txt cache
   - `internal/adapters/robots/cache.go`
   - DB-backed cache using robots_cache table
   - TTL-based expiry
   - Tests: cache hit, miss, expiry
4. [ ] Implement RobotsChecker adapter
   - `internal/adapters/robots/checker.go`
   - Implements RobotsChecker port
   - Combines fetcher + parser + cache + evaluator
   - Tests first

### Phase 6 - Link Extraction + Content Extraction - status: open

HTML parsing, link discovery, content extraction.

1. [ ] Implement HTML link extractor
   - `internal/adapters/http/extractor.go`
   - Parse HTML, extract all <a href>, <link>, <script src>, <img src>
   - Resolve relative URLs using URLNormalizer
   - Classify internal vs external
   - Tests first: various HTML structures, edge cases
2. [ ] Implement content extraction profiles
   - `internal/adapters/http/extraction/minimal.go` — title, meta, status, headers
   - `internal/adapters/http/extraction/standard.go` — above + HTML body + links
   - `internal/adapters/http/extraction/full.go` — above + text + structured data (JSON-LD, OG, microdata)
   - Factory selecting profile from ExtractionProfile value object
   - Tests first for each profile

### Phase 7 - Core Orchestrator - status: open

Job management, URL dispatch, result processing — the brain.

1. [ ] Implement JobService
   - `internal/app/services/job_service.go`
   - CreateJob: validate config, persist, publish job.created
   - PauseJob, ResumeJob, StopJob: state transitions + NATS commands
   - GetJobStatus, ListJobs: queries
   - Tests first: mock all ports
2. [ ] Implement CrawlService (URL dispatch + result processing)
   - `internal/app/services/crawl_service.go`
   - DispatchURLs: pull pending URLs, check robots, publish to NATS queue
   - ProcessResult: receive crawl result, store page, enqueue discovered URLs (dedup)
   - Job completion detection: frontier empty + workers idle
   - Tests first: mock all ports
3. [ ] Implement core orchestrator main loop
   - `internal/app/services/orchestrator.go`
   - Subscribe to NATS subjects, coordinate JobService + CrawlService
   - Worker heartbeat tracking
   - Metrics aggregation + publishing
   - Tests first
4. [ ] Wire up cmd/core/main.go
   - Initialize config, DB, NATS, all services
   - Start orchestrator
   - Graceful shutdown on SIGINT/SIGTERM

### Phase 8 - Crawler Worker - status: open

Fetch-extract-report worker with goroutine pool.

1. [ ] Implement goroutine pool
   - `internal/adapters/http/pool.go`
   - Configurable size, graceful drain
   - Per-domain semaphore
   - Tests first
2. [ ] Implement worker fetch loop
   - `internal/app/services/worker_service.go`
   - Subscribe to NATS queue group for URL dispatch
   - For each URL: check robots -> fetch -> extract -> report result via NATS
   - Handle errors, retries, timeouts
   - Tests first: mock fetcher + broker
3. [ ] Implement per-domain rate limiter
   - `internal/adapters/http/ratelimiter.go`
   - Token bucket per domain
   - Adaptive: adjust based on Crawl-delay, 429 responses, latency
   - Implements RateLimiter port
   - Tests first: rate enforcement, adaptive behavior
4. [ ] Wire up cmd/crawler/main.go
   - Initialize config, NATS, fetcher, rate limiter, worker pool
   - Start worker service
   - Heartbeat reporting
   - Graceful shutdown

### Phase 9 - CLI (First End-to-End Milestone) - status: open

User can start crawl from terminal. First visible result.

1. [ ] Implement urfave/cli v3 app scaffold
   - `internal/adapters/cli/app.go`
   - Root command, version, global flags (--config, --nats-url, --db-path)
2. [ ] Implement crawl commands
   - `internal/adapters/cli/crawl.go`
   - crawl start: send CreateJob via NATS req/reply
   - crawl stop/pause/resume: send commands via NATS
   - crawl status: query job status via NATS
   - crawl list: list all jobs via NATS
   - Tests first: mock NATS broker
3. [ ] Implement config commands
   - `internal/adapters/cli/config.go`
   - config init: generate default.toml
   - config show: display active config
4. [ ] Implement db commands
   - `internal/adapters/cli/db.go`
   - db migrate: run goose up
   - db status: show migration status
5. [ ] Wire up cmd/core CLI entry point
6. [ ] End-to-end integration test
   - Start embedded NATS + in-memory SQLite
   - CLI start crawl -> core processes -> crawler fetches -> results stored
   - Verify URLs discovered, pages stored, job completes

### Phase 10 - Anti-Bot Detection - status: open

Detection layer + configurable response strategies.

1. [ ] Implement anti-bot detector
   - `internal/adapters/antibot/detector.go`
   - HTTP status analysis (403, 429, 503 patterns)
   - Captcha provider signatures (reCAPTCHA, hCaptcha, Cloudflare Turnstile, DataDome)
   - JavaScript challenge detection (Cloudflare UAM, Akamai)
   - Response body heuristics
   - Implements AntiBotDetector port
   - Tests first: known challenge page HTML snapshots
2. [ ] Implement response strategies
   - `internal/adapters/antibot/strategy_skip.go` — flag + move on
   - `internal/adapters/antibot/strategy_rotate.go` — rotate UA/proxy + retry with backoff
   - `internal/adapters/antibot/strategy_solver.go` — emit captcha.detected, wait for solution
   - Strategy selection from AntiBotStrategy value object
   - Tests first
3. [ ] Implement proxy pool
   - `internal/adapters/http/proxy_pool.go`
   - Load proxies from config
   - Rotation strategies: round-robin, random, least-used
   - Health tracking (mark dead proxies)
   - Tests first
4. [ ] Integrate anti-bot into crawler worker fetch loop
   - Post-fetch detection check
   - Execute configured strategy
   - Store antibot_events in DB
   - Tests

### Phase 11 - Data Export - status: open

Export crawl results in multiple formats.

1. [ ] Implement JSON exporter
   - `internal/adapters/export/json.go`
   - Structured export of all crawl data per job
   - Filterable by status, domain, date range
   - Tests first
2. [ ] Implement CSV exporter
   - `internal/adapters/export/csv.go`
   - Flat export: URL, status, title, links count, timestamps
   - Tests first
3. [ ] Implement SQLite dump exporter
   - `internal/adapters/export/sqlite.go`
   - Copy job data to standalone SQLite file
   - Tests first
4. [ ] Implement Sitemap XML generator
   - `internal/adapters/export/sitemap.go`
   - Generate standard XML sitemap from crawled internal URLs
   - Tests first
5. [ ] Implement ExportService
   - `internal/app/services/export_service.go`
   - Route export requests to correct exporter
   - Wire into CLI export command
   - Tests first
6. [ ] Add export CLI command
   - `internal/adapters/cli/export.go`
   - crawlerdb export --job <id> --format json|csv|sqlite|sitemap --output <path>

### Phase 12 - GUI Dashboard - status: open

Web dashboard with real-time updates.

1. [ ] Set up chi router + middleware
   - `internal/adapters/gui/router.go`
   - Chi router with logging, recovery, CORS middleware
   - Static file serving
   - Tests first
2. [ ] Implement REST API endpoints
   - `internal/adapters/gui/api/jobs.go` — GET /api/jobs, GET /api/jobs/:id, POST /api/jobs
   - `internal/adapters/gui/api/urls.go` — GET /api/jobs/:id/urls
   - `internal/adapters/gui/api/pages.go` — GET /api/pages/:id
   - `internal/adapters/gui/api/export.go` — POST /api/export
   - `internal/adapters/gui/api/settings.go` — GET/PUT /api/settings
   - Tests first: against httptest
3. [ ] Implement SSE endpoint
   - `internal/adapters/gui/sse/handler.go`
   - GET /api/sse/:job_id — stream real-time updates
   - NATS subscription on gui.push.{job_id} -> SSE push to browser
   - Tests first
4. [ ] Build frontend views
   - Dashboard: active jobs, aggregate stats, system health
   - Job List: all jobs with status, progress bars
   - Job Detail: live progress, frontier stats, error log, links tree
   - Site Map Visualization: interactive graph (internal/external links)
   - URL Inspector: detail view per URL
   - Settings: configs, proxy pools, UA lists
   - Logs: real-time event stream
5. [ ] Wire up cmd/gui/main.go
   - Initialize config, DB, NATS, chi router
   - Start HTTP server
   - Graceful shutdown
6. [ ] End-to-end GUI integration test
   - Start all three binaries
   - Create job via GUI -> verify real-time updates -> inspect results

## Verification

- [ ] All unit tests pass (go test ./...)
- [ ] Integration tests pass with embedded NATS + in-memory SQLite
- [ ] End-to-end: seed URL crawled, all internal links discovered within scope
- [ ] robots.txt respected — disallowed URLs never fetched
- [ ] Rate limiting honored — no burst beyond limits
- [ ] Anti-bot detection fires on known challenge pages
- [ ] Job pause/resume preserves frontier state
- [ ] Export produces valid JSON/CSV/Sitemap XML
- [ ] GUI shows real-time progress via SSE
- [ ] Multiple crawler workers distribute load via NATS queue groups
- [ ] go vet and golangci-lint clean

## Adjustments

<!-- Plans evolve. Document changes with timestamps. -->

## Progress Log

<!-- Timestamped entries tracking work done. Updated after every action. -->
