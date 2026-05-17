# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

POP ("Proxy of Proxy") is a single-binary Go HTTP proxy with a web console for domain-based routing. Per-domain rules decide whether traffic goes `DIRECT`, through a selected upstream HTTP `PROXY`, or is `BLOCK`ed.

Authoritative product/engineering notes live in [AGENTS.md](AGENTS.md) and [docs/](docs/) (especially `architecture.md`, `rules-and-routing.md`, `testing-strategy.md`, `operations.md`). Read those before changing behavior — they encode product decisions (e.g. block status fixed at 404, longest-pattern-wins matching, DIRECT default).

Requires Go `1.25.5` (see `go.mod`).

## Common commands

All targets are wrapped in the `Makefile`:

- `make run` — run with default ports (proxy `0.0.0.0:5128`, console `127.0.0.1:5080`). Pass flags via `ARGS=`, e.g. `make run ARGS="--console-listen 127.0.0.1:19090"`.
- `make run-bg` / `make stop` — background run; pid/log in `.tmp/`.
- `make build` — produces `./bin/pop` with git version injected into `internal/buildinfo.Version` (needed for the version shown in the console UI).
- `make release` / `make build-linux-arm64` / `make docker-build` / `make docker-build-push-arm64` — multi-platform / container builds.
- `make test` — runs `go test ./...`. **Required to pass before every commit** (see Quality gate below).
- `make test-integration` — only `./integration` (milestone-numbered files `m1_*.go` … `m7_*.go`).
- `make test-console` — only `TestConsole*` in `./integration`.
- `make fmt` / `make vet` / `make lint` (= fmt + vet + test).

Run a single test: `go test ./integration -run TestConsoleRulesList -v` or `go test ./internal/rules -run TestMatcher_LongestWins`.

## Quality gate (from AGENTS.md)

Before any commit:

1. Add/update tests for the change (UI assets in `internal/console/assets/` are excluded).
2. Run `go test ./...` and ensure **all** tests pass — no regressions.
3. PAC generation and HTTP proxy compliance are part of the regular suite; keep them green.

## Architecture

Single process, two planes. `cmd/pop/main.go` wires everything: open SQLite → load upstreams+rules → validate → build `upstream.Manager` + `rules.Matcher` → start a `telemetry.Store` and `SysStatsCollector` → start `proxy.Server` (data plane) and `console.Server` (control plane).

Modules under `internal/`:

- `config` — `Config` (listen addrs, sqlite path, default action, PAC override) plus `RuleConfig`/`UpstreamConfig` DTOs. `Validate` + `ValidateRuntime` enforce invariants (e.g. PROXY rules must reference an existing enabled upstream).
- `rules` — `Action` enum (`DIRECT`/`PROXY`/`BLOCK`), `Rule`, `Decision`, and `Matcher`. `Matcher.Decide(host)` does domain-suffix matching; **longest pattern wins**, with newer rules breaking ties on equal length. `*.example.com` is treated as `example.com` for back-compat.
- `proxy` — HTTP and `CONNECT` server. `NewServerWithDeps(matcher, upstreams)` is the production constructor; `SetTelemetry` attaches the telemetry store. Holds a direct `http.Transport` and delegates PROXY decisions to `upstream.Manager`. Loop detection uses a randomly generated `loopID` header.
- `upstream` — pool of HTTP-proxy `http.Transport`s keyed by upstream ID, rebuilt when config changes.
- `telemetry` — `Store` is the bounded ring buffer of activities (capacity + TTL eviction) feeding `/api/activities[/stream]` and `/api/stats`. `SysStatsCollector` periodically samples CPU/mem/goroutines/heap plus deltas from `Store.Snapshot` and exposes them via `/api/stats/history`. Linux-specific reads live in `sysstats_linux.go`; other OSes use `sysstats_other.go`.
- `store` — `SQLite` (modernc.org/sqlite, pure-Go, no CGO) persists rules + upstreams and implements backup/restore. `CurrentDataFormatVersion` gates restore compatibility; bump it when the schema changes.
- `console` — HTTP API + embedded static UI (`//go:embed assets/*`). Holds the live `*config.Config` behind a mutex and rebuilds matcher/upstream/proxy runtime when config or rules change (`rebuildRuntimeLocked`). Also serves `/proxy.pac` generated from the current matcher.
- `buildinfo` — `Version` var populated via `-ldflags "-X .../buildinfo.Version=$(GIT_DESCRIBE)"`. Direct `go build` without the ldflag leaves it blank in the UI.

Config precedence in `cmd/pop/main.go::resolveRuntimeConfig`: built-in defaults → `POP_*` env vars → CLI flags. Flags: `--proxy-listen/-p`, `--console-listen/-c`, `--sqlite-path/-s`, `--default-action/-a`.

## Things to know when editing

- **Runtime memory must stay bounded.** Activities use ring buffer + TTL; stats are aggregate counters only — do not introduce unbounded history. If you change telemetry shape, add a bounded-memory test (see `internal/telemetry/store_test.go`).
- **Config hot-reload path:** changes go through `console.Server` which validates, rebuilds matcher + upstream manager, then swaps them into the running `proxy.Server`. Don't bypass this — direct mutation breaks invariants under concurrent traffic.
- **Block status code is fixed at 404** for rules created via the console (product decision in AGENTS.md). The field exists in the model but should not be exposed as user-configurable in the UI.
- **Rule reorder is intentionally disabled** — `POST /api/rules/reorder` returns `reorder_disabled`. Ordering is implicit from `created_at` + pattern length.
- **ABP import** (`POST /api/data/import-abp`) parses an Adblock Plus subset; it skips comments, exception (`@@`), and element-hiding rules. Route target (DIRECT or an upstream ID) is supplied alongside the file.
- **Backup/restore is full-replace.** Restore wipes current rules + upstreams. Bump `store.CurrentDataFormatVersion` on incompatible changes.
- **Console UI tests are out of scope.** Don't add Selenium/Playwright-style suites; cover console behavior via the Go API tests in `integration/m6_console_api_test.go`.
- **Docker image is distroless/nonroot.** `POP_SQLITE_PATH` defaults to `/data/pop.sqlite` in-container; keep file paths writable by uid `65532`.
