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

`task build` runs `go fmt`, `gofumpt`, `go vet`, and `go fix` before building — formatting is handled for you, no need to run them separately. Tools (`sqlc`, `templ`, `gofumpt`) are versioned as Go `tool` directives and invoked via `go tool <name>`; do not `go install` them. Go version is pinned in `go.mod` (1.26).

**CLI flags** (binary is `./websitewatcher`): `-config` (required), `-mode cron|once`, `-debug`, `-json` (JSON logs), `-dry-run` (send no mail/webhooks), `-dump-diff-html` (write diff to file), `-configcheck` (validate config and exit), `-version`.

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

## Conventions

Follow the existing patterns rather than introducing new ones:

- **Logging:** structured `log/slog` only. Pass a `*slog.Logger` into constructors; never use the global logger, `fmt.Print*`, or `log.Print*`. Attach context with typed attrs (`slog.String("name", w.Name)`, `slog.Duration(...)`), not interpolated strings.
- **Errors:** wrap with `fmt.Errorf("could not <verb> <subject>: %w", err)` (lowercase, no trailing punctuation) so callers can `errors.Is`/`errors.As`. Use `errors.New` for static messages. Return errors up the stack; only `main.go` decides to log-and-exit.
- **Context:** every I/O-bound function takes `ctx context.Context` as its first parameter and honors cancellation — thread the existing ctx through, don't create `context.Background()` except at top-level entry points and `Close`.
- **Database:** go through the `database.Database` methods (sqlc-generated queries); never hand-write SQL in business logic. Reads use the reader pool, writes the single writer connection — keep it that way. Add new queries to `internal/database/sqlc/` and run `task generate`.
- **Resource cleanup:** always `defer` closing response bodies and other closers; log close errors rather than ignoring them (see `watch.go`).

## Robustness

When adding or changing behavior:

- Validate new config options in `internal/config/` via struct tags / validator and reject mutually-exclusive combinations at load time, so failures surface at startup (`-configcheck`), not mid-run.
- Network and parsing code must assume hostile/malformed input: check status codes, guard against nil (e.g. nil RSS feed), and bound work — reuse the existing retry/timeout/proxy machinery in `internal/http/` instead of calling `net/http` directly.
- A new watch must never notify on first sight (baseline-only); preserve this when touching the compare/notify path in `internal/watch/`.
- Add a test for every behavior change. Tests are table-driven and `t.Parallel()`; use `net/http/httptest` for HTTP, not live URLs. Run `task test` (needs `CGO_ENABLED=1` for `-race`).
- Respect `gosec`: no unparameterized SQL, no unchecked file paths from input, no weak crypto.

## Linting

golangci-lint is configured strictly in `.golangci.yml` with 58+ enabled linters. Notable constraints:

- Use `github.com/google/uuid` (not `satori/go.uuid`)
- Use `google.golang.org/protobuf` (not `golang/protobuf`)
- `gosec` is enabled — avoid common security anti-patterns
