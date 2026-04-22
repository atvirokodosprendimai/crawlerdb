# CrawlerDB

CrawlerDB is a modular Go crawler for collecting pages, files, extracted text, crawl metadata, and anti-bot signals into SQLite.

It is split into small runtime processes:

- `cmd/nats`: embedded NATS server
- `cmd/core`: crawl coordinator, job lifecycle, dispatch, exports, retries, revisit scheduling
- `cmd/crawler`: worker process that fetches URLs, extracts content, detects anti-bot pages, and publishes results
- `cmd/gui`: HTML dashboard and maintenance UI

SQLite stores crawl state. NATS moves commands and crawl results between processes.

## Features

- Domain-aware crawling with one shared queue per job
- Per-domain dispatch pacing and revisit scheduling
- HTML extraction: title, meta, visible text, links, structured data
- Searchable text extraction for:
  - HTML
  - text files
  - CSV
  - JSON
  - XML
  - PDF
- File-backed content storage in `data/`
- GUI for job monitoring, settings, dedupe, mark-for-delete, and exports
- Mark-and-sweep deletion with delayed cleanup
- Optional Chromium fallback for native fetch failures / anti-bot pages

## Architecture

High-level flow:

1. GUI or CLI creates a job
2. Core stores job and enqueues seed URL
3. Core dispatches eligible pending URLs to workers over NATS
4. Worker fetches URL, optionally falls back to Chromium, extracts content, then publishes `crawl.result.<job>`
5. Core updates URL status, stores page metadata/files, and enqueues discovered links
6. GUI reads SQLite directly and subscribes to NATS-backed updates for live state

Main data tables:

- `jobs`
- `urls`
- `pages`
- `antibot_events`
- `workers`
- `domain_assignments`
- `robots_cache`

## Repository Layout

```text
cmd/
  core/       coordinator
  crawler/    crawl worker
  gui/        dashboard server + deletion sweeper
  nats/       embedded NATS server

configs/
  default.toml

internal/
  adapters/   HTTP, DB, GUI, NATS, robots, CLI package
  app/        application services
  domain/     entities, ports, value objects, domain services
```

## Requirements

- Go `1.25+`
- SQLite (embedded through Go driver)
- `templ` for code generation

Optional:

- Chromium/browserless-compatible container for fallback rendering

## Quick Start

Generate templ files, build all binaries, and run whole stack:

```bash
make run
```

This starts:

- NATS on `127.0.0.1:4222`
- Core
- One crawler worker
- GUI on `:8081`

Open:

```text
http://localhost:8081
```

## Build

Generate templ output:

```bash
make generate
```

Build all binaries:

```bash
make build
```

Built binaries land in `.bin/`:

- `.bin/crawlerdb-nats`
- `.bin/crawlerdb-core`
- `.bin/crawlerdb-crawler`
- `.bin/crawlerdb-gui`

Run tests:

```bash
make test
```

## Running Processes Manually

Start NATS:

```bash
go run ./cmd/nats
```

Start core:

```bash
go run ./cmd/core
```

Start one crawler:

```bash
go run ./cmd/crawler --debug
```

Start GUI:

```bash
go run ./cmd/gui
```

All processes honor `CRAWLERDB_CONFIG` when set.

Example:

```bash
CRAWLERDB_CONFIG=configs/default.toml go run ./cmd/core
```

## Configuration

Default config file in repo:

```text
configs/default.toml
```

Generate a fresh config from code defaults with:

```bash
go run ./cmd/cli config init
```

Key config sections:

### NATS

```toml
[nats]
url = "nats://localhost:4222"
max_reconnects = 60
reconnect_wait = "2s"
request_timeout = "10s"
```

### Database

```toml
[database]
path = "crawlerdb.sqlite"
```

### Crawler

Important fields in current code:

```toml
[crawler]
user_agent = "CrawlerDB/1.0"
max_depth = 10
pool_size = 10
request_timeout = "30s"
default_rate_limit = "1s"
max_retries = 3
robots_ttl = "24h"
heartbeat_interval = "5s"
heartbeat_ttl = "15s"
crawl_stuck_timeout = "10m"
domain_concurrency = 2
data_dir = ".crawlerdb"
content_dir = "data"
chromium_url = ""
```

### GUI server

```toml
[server]
addr = ":8081"
```

## GUI Workflows

The GUI is the main operator interface.

### Start a Crawl

Use the dashboard form to create a job.

Each job stores:

- seed URL
- scope
- extraction mode
- rate limit
- revisit TTL
- concurrency and anti-bot settings

### Edit Site Settings

Selected job card has a `Settings` link.

Path:

```text
/jobs/<job_id>/settings
```

You can edit existing jobs without creating a new one.

### Dedupe URLs

Selected job card has `Dedupe URLs`.

It merges duplicate URL rows by normalized URL and rewires related `pages` / `antibot_events` before deleting duplicates.

### Mark Delete

Selected job card has `Mark Delete`.

This does not delete immediately.

It:

- marks `jobs.delete_marked_at`
- stops active jobs logically
- keeps data visible until sweep

### Automatic Sweep

`cmd/gui` runs a sweep loop:

- once on startup
- then every hour

It deletes jobs marked more than 24 hours ago using cascade cleanup for rows and stored files.

## Crawl Semantics

### URL uniqueness

CrawlerDB keeps one URL row per `(job_id, normalized)`.

New discoveries upsert into the existing URL record instead of creating duplicates.

### Revisit / Recrawl

When `revisit_ttl` is configured for a job:

- successful URLs get `revisit_at`
- core requeues them later as `pending`
- same URL row is reused

Current page storage behavior:

- `pages` can accumulate multiple snapshots per URL across recrawls
- content file path is deterministic by normalized URL, so stored file is overwritten by latest crawl of that URL/content type

### Rate limiting

Core dispatch pacing enforces per-domain timing before work is sent to workers.

### Stuck crawl recovery

Core requeues `crawling` URLs that exceed `crawl_stuck_timeout`.

## Chromium Fallback

Crawler workers can optionally retry through a remote Chromium endpoint.

Current behavior:

1. native Go HTTP fetch first
2. if native fetch fails, try Chromium
3. if native fetch succeeds but anti-bot challenge is detected, try Chromium
4. if Chromium fails too, leave as failure / blocked

Config:

```toml
[crawler]
chromium_url = "http://localhost:3000"
```

Expected API:

- Browserless-compatible `POST <chromium_url>/content`

## Text Extraction and Searchable Content

CrawlerDB stores searchable plain text in `pages.text_content`.

Current extraction supports:

- HTML stripped to visible text
- `text/*`
- CSV
- JSON
- XML
- JavaScript text payloads
- PDF

For already-stored files, you can backfill searchable text later.

## Maintenance Commands

CLI maintenance commands are available through:

```bash
go run ./cmd/cli
```

### Sweep deleted jobs

```bash
crawlerdb db sweep-deleted
```

Optional custom age:

```bash
crawlerdb db sweep-deleted --before 48h
```

### Backfill searchable text from stored files

```bash
crawlerdb db backfill-text
```

Backfill only one job:

```bash
crawlerdb db backfill-text --job <JOB_ID>
```

Process only some pages:

```bash
crawlerdb db backfill-text --limit 500
```

Example:

```bash
go run ./cmd/cli --db-path crawlerdb.sqlite db backfill-text
```

## Exports

Core supports export by job.

Formats:

- `json`
- `csv`
- `sitemap`

CLI export supports:

```bash
crawlerdb export --job <JOB_ID> --format json --output out.json
```

## Current Fetch / Parsing Rules

### Link extraction

Crawler currently walks only:

- `a[href]`
- `area[href]`
- `link[href]` for navigational relations such as `alternate`, `canonical`, `next`, `prev`
- `iframe[src]` and `frame[src]`
- `embed[src]` and `object[data]` when the target looks like a document

It does not enqueue static asset references such as CSS, JS, images, fonts, or media files just because they are referenced by the page.

### Malformed old-site links

Normalizer repairs some broken legacy URLs, for example:

- `HTTP//example.com/file.pdf` -> `http://example.com/file.pdf`
- `HTTPS//example.com/file.pdf` -> `https://example.com/file.pdf`

## Database Notes

Migrations are managed with Goose.

Run migrations through the DB command or on service startup:

- `cmd/core` migrates on startup
- `cmd/crawler` migrates on startup
- GUI opens DB directly and expects current schema

Recent important schema features:

- URL dedupe / uniqueness by normalized URL per job
- `last_error` on URLs
- `content_path` on pages
- delayed deletion marker `jobs.delete_marked_at`

## Troubleshooting

### GUI build fails with missing templ handlers

Regenerate templ output first:

```bash
templ generate ./internal/adapters/gui
```

Then rebuild:

```bash
go build -o .bin/crawlerdb-gui ./cmd/gui
```

### Stuck `crawling` URLs

Possible causes:

- old binary still running
- worker crashed mid-flight
- crawl timeout too small for large files
- wrong DB path inspected versus runtime DB path

Relevant settings:

- `crawler.request_timeout`
- `crawler.crawl_stuck_timeout`

### PDFs or text files already stored but not searchable

Run text backfill:

```bash
crawlerdb db backfill-text
```

### Marked jobs are not disappearing

Remember:

- mark only sets `delete_marked_at`
- actual deletion happens in `cmd/gui`
- sweep cutoff is 24h

If needed, run manual sweep command after exposing the CLI wrapper.

## Development Tips

- Generate templ files before GUI builds
- Prefer `make build` or `make run`
- Use `go test ./...` before pushing broad changes
- If changing extraction/storage logic, also verify migrations and backfill commands still make sense

## Status

CrawlerDB is evolving quickly. The codebase already supports real crawling, maintenance, dedupe, delayed deletion, PDF/text backfill, and GUI operations, but some operator workflows still depend on which binaries you choose to expose in deployment.
