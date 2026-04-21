---
tldr: Modular recursive web crawler with hexagonal architecture, NATS orchestration, SQLite storage, and configurable extraction/anti-bot strategies
---

# CrawlerDB

Modular recursive web crawler built in Go. Three binaries: **core** (orchestrator), **crawler** (worker), **gui** (web dashboard). Communicates via NATS pub/sub and request/reply. Stores everything in SQLite (no CGO) via GORM. Organized with hexagonal architecture and DDD. Built TDD-first.

## Target

Crawl a website recursively from a seed URL — discover all internal/external links, extract configurable content, respect robots.txt, handle anti-bot detection, and provide real-time visibility into crawl progress. Scale horizontally by adding crawler workers.

## Behaviour

### Job Lifecycle

- User submits seed URL + job config via CLI or GUI
- Core validates config, creates job in DB, publishes `job.created` on NATS
- Core dispatches seed URL as first crawl task via NATS queue group
- Crawler workers claim URLs from NATS queue, fetch, extract, report results
- Core receives results, enqueues discovered URLs, updates DB
- Job completes when URL frontier is empty and all workers idle
- Jobs can be paused, resumed, stopped via CLI/GUI → NATS commands

### Crawl Scope (configurable per job)

- **Same-domain only** — crawl URLs matching seed domain exactly
- **Include subdomains** — crawl seed domain + all subdomains
- **Follow externals** — follow external links to configurable depth N
- External links always recorded in DB regardless of crawl scope
- Scope evaluated per-URL before dispatch

### Content Extraction (configurable per job)

- **Minimal** — URLs, HTTP status, headers, title, meta tags
- **Standard** — above + full HTML body + all discovered links
- **Full** — above + extracted plain text + structured data (JSON-LD, OpenGraph, microdata)
- Extraction profile set per job, can be overridden per URL pattern
- All extracted data stored in SQLite, queryable

### URL Management

- URL normalization: lowercase host, sort query params, strip fragments, resolve relative paths, handle trailing slashes
- SHA-256 hash of normalized URL for dedup
- Configurable revisit TTL per URL — re-crawl after expiry
- URL frontier priority queue: breadth-first default, configurable

### robots.txt Compliance

- Fetch and parse robots.txt before crawling any domain
- Cache robots.txt per domain with configurable TTL
- Respect `Disallow`, `Allow`, `Crawl-delay` directives
- Match against configured User-Agent string
- Sitemap directives parsed → seed URLs added to frontier

### Rate Limiting & Politeness

- Per-domain rate limiting, configurable per job
- Adaptive throttling: start conservative, adjust based on:
  - robots.txt `Crawl-delay`
  - HTTP 429 responses (back off exponentially)
  - Response latency trends (slow down if server stressed)
- Default: 1 request/second per domain if no signals
- Concurrent domain limit configurable

### Anti-Bot / Captcha Detection

- Detection layer identifies bot walls and captcha challenges:
  - HTTP status patterns (403, 429, 503 with challenge body)
  - Known captcha provider signatures (reCAPTCHA, hCaptcha, Cloudflare Turnstile, DataDome, etc.)
  - JavaScript challenge detection (Cloudflare UAM, Akamai, etc.)
  - Response body heuristics (challenge page fingerprints)
- Response strategy configurable per job:
  - **Skip + flag** — mark URL as blocked, emit `url.blocked` event, move on
  - **Rotate + retry** — rotate User-Agent and/or proxy, retry with backoff
  - **Plugin solver** — emit `captcha.detected` event with type info, external solver plugin subscribes, solves, returns solution, crawler retries with solution
- All detection events logged and queryable
- Proxy pool support: list of proxies, rotation strategy (round-robin, random, least-used)

### JavaScript Rendering

- **v1**: static HTML only — parse raw HTTP response
- **Future**: optional headless browser adapter via CDP (Chrome DevTools Protocol)
  - Configurable per job or per URL pattern
  - Adapter implements same port interface as static fetcher
  - Resource filtering (block images/fonts/tracking for speed)

### NATS Messaging

Subjects and patterns:

| Subject | Pattern | Purpose |
|---------|---------|---------|
| `job.created` | Pub/Sub | Job lifecycle — new job announced |
| `job.updated` | Pub/Sub | Job state change (paused, resumed, stopped, completed) |
| `crawl.dispatch.{job_id}` | Queue Group | URL dispatch to crawler workers |
| `crawl.result.{job_id}` | Pub/Sub | Worker reports crawl result to core |
| `url.discovered` | Pub/Sub | New URLs found on a page |
| `url.blocked` | Pub/Sub | URL hit anti-bot / captcha |
| `captcha.detected` | Pub/Sub | Captcha detected, solver plugin can subscribe |
| `captcha.solved` | Pub/Sub | Solution returned from solver |
| `metrics.{job_id}` | Pub/Sub | Real-time metrics (pages/sec, queue depth, errors) |
| `gui.push.{job_id}` | Pub/Sub | SSE bridge for GUI real-time updates |
| `webhook.{event}` | Pub/Sub | Webhook dispatcher subscribes, forwards to configured endpoints |
| `worker.heartbeat` | Req/Reply | Worker health check |
| `job.command.{job_id}` | Req/Reply | Pause/resume/stop commands |

### CLI (urfave/cli v3)

Commands:

```
crawlerdb crawl start --url <seed> --config <file>    # Start new crawl job
crawlerdb crawl status --job <id>                      # Job status + progress
crawlerdb crawl stop --job <id>                        # Stop crawl job
crawlerdb crawl pause --job <id>                       # Pause crawl job
crawlerdb crawl resume --job <id>                      # Resume crawl job
crawlerdb crawl list                                   # List all jobs
crawlerdb export --job <id> --format json|csv|sqlite|sitemap --output <path>
crawlerdb config init                                  # Generate default config file
crawlerdb config show                                  # Show active config
crawlerdb db migrate                                   # Run goose migrations
crawlerdb db status                                    # Migration status
```

CLI communicates with core via NATS req/reply.

### GUI (Web Dashboard)

- Served by `cmd/gui` binary — Go HTTP server + static frontend
- Connects to core via NATS for real-time data
- SSE (Server-Sent Events) push to browser via `gui.push.*` subjects

Views:

- **Dashboard** — active jobs, aggregate stats, system health
- **Job List** — all jobs with status, progress bars, timestamps
- **Job Detail** — live crawl progress, URL frontier stats, error log, discovered links tree
- **Site Map Visualization** — interactive graph of crawled site structure (internal/external links)
- **URL Inspector** — detail view for any crawled URL: headers, extracted data, linked from/to
- **Settings** — manage configs, proxy pools, User-Agent lists
- **Logs** — real-time event stream filtered by job/severity

Real-time updates via SSE, backed by NATS subscription in GUI server.

### Data Export

- **JSON** — structured export of all crawl data per job
- **CSV** — flat export: URL, status, title, links count, timestamps
- **SQLite dump** — copy of job's data as standalone SQLite file
- **Sitemap XML** — generate standard XML sitemap from crawled internal URLs
- Export filterable by status, domain, date range

## Design

### Hexagonal Architecture

```
┌─────────────────────────────────────────────────┐
│                   cmd/                           │
│         core/    crawler/    gui/                │
│              (entry points)                      │
└──────────────────┬──────────────────────────────┘
                   │
┌──────────────────▼──────────────────────────────┐
│              internal/app/                       │
│         (application services)                   │
│   JobService  CrawlService  ExportService       │
└──────────────────┬──────────────────────────────┘
                   │
┌──────────────────▼──────────────────────────────┐
│             internal/domain/                     │
│          (entities, value objects,               │
│           ports/interfaces, domain events)       │
│   Job  URL  Page  CrawlResult  RobotsPolicy     │
│   CrawlConfig  ExtractionProfile  AntiBot       │
└──────────────────┬──────────────────────────────┘
                   │
┌──────────────────▼──────────────────────────────┐
│            internal/adapters/                    │
│    ┌──────────┐ ┌──────────┐ ┌──────────┐      │
│    │   db/    │ │  nats/   │ │  http/   │      │
│    │  (gorm)  │ │(nats.io) │ │(fetcher) │      │
│    └──────────┘ └──────────┘ └──────────┘      │
│    ┌──────────┐ ┌──────────┐ ┌──────────┐      │
│    │ robots/  │ │ antibot/ │ │  export/ │      │
│    │(parser)  │ │(detector)│ │(formats) │      │
│    └──────────┘ └──────────┘ └──────────┘      │
└─────────────────────────────────────────────────┘
```

### Domain Model (DDD)

**Aggregates:**
- `Job` — root aggregate. Owns config, state machine, URL frontier reference
- `CrawlResult` — page data, extracted content, discovered links

**Entities:**
- `URL` — normalized URL with hash, state (pending/crawling/done/blocked/error), revisit TTL
- `Page` — HTTP response data, extracted content, belongs to CrawlResult

**Value Objects:**
- `CrawlConfig` — scope, depth, rate limits, extraction profile, anti-bot strategy
- `ExtractionProfile` — what to extract (minimal/standard/full + custom patterns)
- `RobotsPolicy` — parsed robots.txt rules for a domain
- `AntiBotStrategy` — detection + response config
- `NormalizedURL` — URL after normalization, with hash

**Ports (interfaces in `internal/domain/ports/`):**
- `JobRepository` — CRUD jobs
- `URLRepository` — URL frontier operations (enqueue, claim, complete, find)
- `PageRepository` — store/query crawled pages
- `Fetcher` — fetch URL, return response (static HTTP or headless browser)
- `MessageBroker` — publish/subscribe/request-reply abstraction over NATS
- `RobotsChecker` — check URL against robots.txt policy
- `AntiBotDetector` — analyze response for bot detection signals
- `CaptchaSolver` — solve detected captcha (plugin interface)
- `Exporter` — export data in specific format
- `RateLimiter` — per-domain rate limiting

### SQLite Schema (key tables)

```sql
-- jobs
CREATE TABLE jobs (
    id          TEXT PRIMARY KEY,  -- ULID
    seed_url    TEXT NOT NULL,
    config      TEXT NOT NULL,     -- JSON blob
    status      TEXT NOT NULL,     -- pending|running|paused|completed|failed|stopped
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL,
    started_at  DATETIME,
    finished_at DATETIME,
    stats       TEXT               -- JSON: pages_crawled, errors, etc.
);

-- urls
CREATE TABLE urls (
    id          TEXT PRIMARY KEY,  -- ULID
    job_id      TEXT NOT NULL REFERENCES jobs(id),
    raw_url     TEXT NOT NULL,
    normalized  TEXT NOT NULL,
    url_hash    TEXT NOT NULL,     -- SHA-256 of normalized
    depth       INTEGER NOT NULL DEFAULT 0,
    status      TEXT NOT NULL,     -- pending|crawling|done|blocked|error
    retry_count INTEGER NOT NULL DEFAULT 0,
    revisit_at  DATETIME,
    found_on    TEXT,              -- URL where this was discovered
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL,
    UNIQUE(job_id, url_hash)
);

-- pages
CREATE TABLE pages (
    id              TEXT PRIMARY KEY,
    url_id          TEXT NOT NULL REFERENCES urls(id),
    job_id          TEXT NOT NULL REFERENCES jobs(id),
    http_status     INTEGER,
    content_type    TEXT,
    headers         TEXT,          -- JSON
    title           TEXT,
    meta_tags       TEXT,          -- JSON
    html_body       TEXT,
    text_content    TEXT,
    structured_data TEXT,          -- JSON (JSON-LD, OG, etc.)
    links           TEXT,          -- JSON array of discovered links
    fetch_duration  INTEGER,       -- milliseconds
    fetched_at      DATETIME NOT NULL,
    created_at      DATETIME NOT NULL
);

-- robots_cache
CREATE TABLE robots_cache (
    domain      TEXT PRIMARY KEY,
    content     TEXT NOT NULL,     -- raw robots.txt
    parsed      TEXT NOT NULL,     -- JSON parsed rules
    fetched_at  DATETIME NOT NULL,
    expires_at  DATETIME NOT NULL
);

-- antibot_events
CREATE TABLE antibot_events (
    id          TEXT PRIMARY KEY,
    url_id      TEXT NOT NULL REFERENCES urls(id),
    job_id      TEXT NOT NULL REFERENCES jobs(id),
    event_type  TEXT NOT NULL,     -- captcha|challenge|block|rate_limit
    provider    TEXT,              -- recaptcha|hcaptcha|cloudflare|etc
    strategy    TEXT NOT NULL,     -- skip|retry|solve
    resolved    BOOLEAN NOT NULL DEFAULT FALSE,
    details     TEXT,              -- JSON
    created_at  DATETIME NOT NULL
);
```

Migrations managed by goose in `internal/adapters/db/migrations/`.

### Concurrency Model

- Each crawler worker runs goroutine pool (configurable size, default 10)
- Workers subscribe to NATS queue group `crawl.dispatch.{job_id}` — NATS distributes across workers
- Horizontal scaling: add more crawler instances, NATS load-balances
- Per-domain semaphore within each worker — no domain gets more than N concurrent requests
- Worker heartbeat via NATS req/reply — core tracks active workers

### Project Structure

```
crawlerdb/
├── cmd/
│   ├── core/           # Orchestrator binary
│   │   └── main.go
│   ├── crawler/        # Worker binary
│   │   └── main.go
│   └── gui/            # Web dashboard binary
│       └── main.go
├── internal/
│   ├── domain/
│   │   ├── entities/   # Job, URL, Page, CrawlResult
│   │   ├── valueobj/   # CrawlConfig, ExtractionProfile, NormalizedURL, etc.
│   │   ├── ports/      # Interfaces (repositories, fetcher, broker, etc.)
│   │   ├── services/   # Domain services (URLNormalizer, RobotsEvaluator)
│   │   └── events/     # Domain events
│   ├── app/
│   │   ├── commands/   # Command handlers (StartCrawl, StopCrawl, etc.)
│   │   ├── queries/    # Query handlers (GetJobStatus, ListJobs, etc.)
│   │   └── services/   # Application services (JobService, CrawlService, ExportService)
│   └── adapters/
│       ├── db/
│       │   ├── gorm/       # GORM models + repository implementations
│       │   └── migrations/ # Goose migration files
│       ├── nats/           # NATS adapter — MessageBroker implementation
│       ├── http/           # HTTP fetcher adapter
│       ├── robots/         # robots.txt parser adapter
│       ├── antibot/        # Anti-bot detection adapter
│       ├── export/         # Exporter implementations (JSON, CSV, SQLite, Sitemap)
│       ├── cli/            # urfave/cli command definitions
│       └── gui/            # GUI HTTP handlers + SSE + static assets
├── pkg/                    # Shared utilities (if any)
├── configs/                # Default config files
├── eidos/                  # Specs
├── go.mod
└── go.sum
```

### Chi Router

- GUI uses `go-chi/chi` for HTTP routing
- RESTful API endpoints for GUI frontend
- SSE endpoint for real-time updates
- Middleware: logging, recovery, CORS

## Verification

### Unit Tests (TDD — written first)

- Domain entities: Job state machine transitions, URL normalization, hash computation
- Value objects: CrawlConfig validation, ExtractionProfile parsing
- Domain services: URLNormalizer edge cases, RobotsEvaluator rule matching
- Application services: mock all ports, test command/query handlers
- Adapters: each adapter tested against its port interface

### Integration Tests

- GORM repositories against real SQLite (in-memory)
- NATS messaging with embedded NATS server
- robots.txt parser against real-world robots.txt samples
- Anti-bot detector against known challenge page snapshots
- Full crawl pipeline: core + crawler + embedded NATS + in-memory SQLite

### Acceptance Criteria

- Seed URL crawled, all internal links discovered within configured scope
- robots.txt respected — disallowed URLs never fetched
- Rate limiting honored — no burst beyond configured limits
- Captcha detection fires on known challenge pages
- Job pause/resume preserves frontier state
- Export produces valid JSON/CSV/Sitemap XML
- GUI shows real-time progress updates
- Multiple crawler workers distribute load via NATS

## Friction

- **SQLite + no CGO**: must use `modernc.org/sqlite` (pure Go) — slower than CGO sqlite3, watch for performance on large crawls (100k+ URLs)
- **NATS single point**: if NATS goes down, crawl halts. Consider embedded NATS in core as fallback
- **URL normalization edge cases**: internationalized domain names, URL encoding variants, JavaScript-generated URLs — never perfect
- **Anti-bot arms race**: detection signatures need regular updates as providers change
- **Headless browser future**: CDP integration adds complexity — resource management, crash recovery, memory pressure

## Interactions

- Depends on: NATS server (external or embedded), SQLite database file
- Affects: target websites (must be polite), proxy services (if configured)

## Mapping

> [[cmd/core/main.go]]
> [[cmd/crawler/main.go]]
> [[cmd/gui/main.go]]
> [[internal/domain/ports/]]
> [[internal/domain/entities/]]
> [[internal/adapters/db/migrations/]]
> [[internal/adapters/nats/]]

## Future

{[!] Headless browser adapter via CDP for JavaScript-rendered pages}
{[!] Plugin system for custom extractors (CSS selectors, XPath, regex patterns)}
{[?] Distributed SQLite or switch to PostgreSQL for very large scale}
{[?] Machine learning-based content classification during crawl}
{[?] Scheduled/recurring crawls with diff detection}
{[?] API-first mode — core exposes REST/gRPC API directly, not just NATS}

## Notes

- Go 1.25.0 — use latest features (range-over-func, etc.)
- SQLite driver: `modernc.org/sqlite` for CGO-free operation
- GORM with SQLite dialect — watch for GORM quirks with SQLite (no ALTER COLUMN, etc.)
- goose migrations: use SQL format for portability, not Go format
- ULID for all IDs — sortable, URL-safe, no coordination needed
- Config format: TOML or YAML — pick one, expose via CLI flags too
