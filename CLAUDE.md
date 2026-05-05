# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

This project uses [Task](https://taskfile.dev) as the build tool (`task` command).

```bash
task build        # fmt + vet + build binary (CGO_ENABLED=0)
task test         # go test -race -cover ./... (requires CGO_ENABLED=1)
task lint         # run golangci-lint
task generate     # run sqlc and templ code generators
task deps         # go mod tidy
task update       # update all dependencies + regenerate
task configcheck  # validate config file and exit
task run          # dry-run with HTML diff output (debug mode)
task run-once     # run all checks immediately without cron scheduling
```

To run a single test:
```bash
CGO_ENABLED=1 go test -race -run TestName ./internal/package/...
```

After editing `.templ` files or SQL queries, run `task generate` to regenerate derived code.

## Architecture

Websitewatcher fetches URLs on a schedule, compares content against what's stored in SQLite, and sends email/webhook notifications when content changes.

**Execution modes** (set via `-mode` flag):
- `cron` (default): runs watches on their configured cron schedules using gocron
- `once`: runs all watches sequentially and exits — used for Kubernetes CronJobs

**Core processing pipeline** (in `internal/watch/`):
1. HTTP fetch with configurable retries (`internal/http/`)
2. Optional content extraction: CSS selector (`goquery`), JQ filter (`gojq`), or RSS parsing (`gofeed`)
3. Optional transformations: regex replacements, HTML→text, whitespace trimming
4. Compare with last content from SQLite database
5. On change: generate git-based diff → render to HTML via templ → send email + webhooks

**Key packages:**
- `internal/config/` — JSON config loading via koanf, validation via go-playground/validator. Validates cron expressions, emails, URLs, JQ syntax at startup. Mutually exclusive options are rejected at load time (see Configuration section).
- `internal/database/` — SQLite with WAL mode; separate reader (100) and writer (1) connections; goose migrations; sqlc-generated query code
- `internal/diff/` — calls `git diff` for unified diff, renders HTML with templ (`diff_templ.go` is generated)
- `internal/helper/` — CSS selector extraction (goquery), HTML→text conversion
- `internal/taskmanager/` — gocron v2 wrapper with singleton mode (no overlapping job runs)
- `internal/mail/` — SMTP email via go-mail; sends both change diffs and error alerts
- `internal/webhook/` — HTTP webhooks supporting GET/POST/PUT/PATCH/DELETE

**New watch baseline behavior:** The first time a watch is encountered (not yet in DB), its content is stored without sending any notification. Diffs only fire on subsequent runs when content changes.

**Database schema:**
```sql
CREATE TABLE watches (
  id INTEGER NOT NULL PRIMARY KEY,
  name TEXT NOT NULL,
  url TEXT NOT NULL,
  last_fetch DATETIME NOT NULL,
  last_content BLOB NOT NULL
);
CREATE UNIQUE INDEX idx_name_url ON watches (name, url);
```

## Code Generation

Two generators are used — always run `task generate` after changing their inputs:
- **sqlc**: SQL → Go query code. Config in `sqlc.yml`, SQL in `internal/database/sqlc/`, output is `internal/database/sqlc/*.go`
- **templ**: `.templ` → `_templ.go`. Used for HTML diff email rendering in `internal/diff/`

## Configuration

Config is JSON (`-config` flag required). See `config.sample.json` for all options.

Global options: `timeout`, `useragent`, `proxy`, `retry` (`count`/`delay`), `database`, `location` (IANA timezone), `graceful_timeout`, `no_errormail_on_statuscode`, `retry_on_match`.

Key watch-level options:
- `cron`: schedule (required; defaults to `@hourly` if omitted)
- `method`/`header`/`body`: HTTP request customization
- `pattern`: CSS selector to extract content
- `extract_body`: shorthand for `pattern: "body"`
- `jq`: JQ filter for JSON responses
- `parse_rss`: parse as RSS feed
- `html2text`: convert HTML to plain text
- `replaces`: list of `{pattern, replace_with}` regex replacements
- `remove_empty_lines` / `trim_whitespace`: post-processing cleanup
- `retry_on_match`: retry if response body matches any of these patterns
- `skip_soft_error_patterns`: disable built-in nginx soft-error retry patterns
- `additional_to`: extra email recipients for this watch
- `disabled`: skip this watch without removing it from config
- `webhooks`: list of `{url, method, header}` webhook targets

Mutually exclusive combinations rejected at startup:
- `pattern` + `extract_body`
- `jq` + `extract_body`
- `parse_rss` + `jq`
- `parse_rss` + `html2text`
- `jq` + `html2text`

## Linting

golangci-lint is configured strictly in `.golangci.yml` with 58+ enabled linters. Notable constraints:
- Use `github.com/google/uuid` (not `satori/go.uuid`)
- Use `google.golang.org/protobuf` (not `golang/protobuf`)
- `gosec` is enabled — avoid common security anti-patterns
