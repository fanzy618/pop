# Proxy of Proxy (POP)

POP is a local HTTP proxy for personal use. It decides request handling by ordered domain rules:

- `DIRECT`: connect directly
- `PROXY`: forward through a configured upstream HTTP proxy
- `BLOCK`: reject with fixed status code `404` in web console

If no rule matches, POP uses `default_action` (default: `DIRECT`).

## Features

- Local HTTP proxy endpoint (supports normal HTTP and `CONNECT` tunnels)
- Ordered rule matching (`first match wins`)
- Runtime telemetry: live activities, in-flight requests, counters, bandwidth
- Web console API and pages (no authentication)
- Rules and upstreams persisted in SQLite
- Full backup/restore for SQLite data

## Requirements

- Go `1.25.5` (per `go.mod`)

## Runtime Config

POP no longer uses JSON config files. Runtime config comes from:

1. CLI arguments (highest priority)
2. Environment variables
3. Built-in defaults

If a key appears in multiple places, resolution order is: `CLI > ENV > default`.

### Defaults

- `proxy_listen`: `0.0.0.0:5128`
- `console_listen`: `127.0.0.1:5080`
- `default_action`: `DIRECT`
- `sqlite_path`: `./pop.sqlite`

### Environment variables

- `POP_PROXY_LISTEN`
- `POP_CONSOLE_LISTEN`
- `POP_DEFAULT_ACTION` (`DIRECT` / `PROXY` / `BLOCK`)
- `POP_SQLITE_PATH`

### CLI (GNU style)

Long and short flags are both supported:

- `--proxy-listen`, `-p`
- `--console-listen`, `-c`
- `--default-action`, `-a`
- `--sqlite-path`, `-s`

## Quick Start

Run with defaults:

```bash
go run ./cmd/pop
```

Run with overrides:

```bash
POP_PROXY_LISTEN=127.0.0.1:8080 go run ./cmd/pop --console-listen 127.0.0.1:9090
```

Configure your OS/browser HTTP proxy to POP's `proxy_listen`.

## Console API

- `GET /api/config`
- `PUT /api/config`
- `GET /api/upstreams`
- `POST /api/upstreams`
- `PUT /api/upstreams/:id`
- `DELETE /api/upstreams/:id`
- `GET /api/rules`
- `POST /api/rules`
- `PUT /api/rules/:id`
- `DELETE /api/rules/:id`
- `POST /api/rules/reorder`
- `GET /api/data/backup`
- `POST /api/data/restore`
- `GET /api/stats`
- `GET /api/activities?limit=100`
- `GET /api/activities/stream` (SSE)

Backup payload includes `data_format_version`; restore currently requires matching version.

## Verify Locally

```bash
go test ./...
```

```bash
curl -x http://127.0.0.1:5128 http://example.com -I
```

```bash
curl http://127.0.0.1:5080/api/stats
```
