# Proxy of Proxy (POP)

POP is a local HTTP proxy with a web console for domain-based routing.

## Features

- HTTP proxy endpoint for normal HTTP and `CONNECT` traffic
- Domain rules with actions:
  - `DIRECT`
  - `PROXY` (via selected upstream)
  - `BLOCK` (web console behavior fixed to `404`)
- Rules are matched by newest first (`created_at` desc)
- Creating a rule with the same domain pattern overrides the existing rule and refreshes its `created_at`
- Live telemetry: in-flight, totals, bandwidth, activities, SSE stream
- Web console pages:
  - Stats
  - Activities (supports quick "add rule" from an activity row)
  - Rules
  - Upstreams
  - Data management
- SQLite persistence for rules/upstreams
- Data management:
  - Full backup/restore of SQLite data
  - ABP text import (`Adblock Plus` syntax subset) with unified route target (`DIRECT` or selected `UPSTREAM`)

## Requirements

- Go `1.25.5` (see `go.mod`)

## Run

```bash
make run
```

Default listen addresses:

- Proxy: `0.0.0.0:5128`
- Console: `127.0.0.1:5080`

## Configuration

Configuration sources and priority:

1. CLI flags
2. Environment variables
3. Built-in defaults

Priority is always: `CLI > ENV > default`.

Environment variables:

- `POP_PROXY_LISTEN`
- `POP_CONSOLE_LISTEN`
- `POP_DEFAULT_ACTION` (`DIRECT` / `PROXY` / `BLOCK`)
- `POP_SQLITE_PATH`

CLI flags (GNU style):

- `--proxy-listen`, `-p`
- `--console-listen`, `-c`
- `--default-action`, `-a`
- `--sqlite-path`, `-s`

Example:

```bash
POP_PROXY_LISTEN=127.0.0.1:18080 make run ARGS="--console-listen 127.0.0.1:19090"
```

`make run` / `make run-bg` 会自动注入当前 git 版本，web console 顶部可直接显示版本号。

## Console API

- `GET /api/config`
- `PUT /api/config`
- `GET /api/upstreams`
- `POST /api/upstreams`
- `PUT /api/upstreams/:id`
- `DELETE /api/upstreams/:id`
- `GET /api/rules?page=1&page_size=20&keyword=ads`
- `POST /api/rules`
- `PUT /api/rules/:id`
- `DELETE /api/rules/:id`
- `POST /api/rules/reorder` (currently returns `reorder_disabled`)
- `GET /api/data/backup`
- `POST /api/data/restore`
- `POST /api/data/import-abp`
- `GET /api/version`
- `GET /api/stats`
- `GET /api/activities?limit=100`
- `GET /api/activities/stream`

Notes:

- `GET /api/rules` returns `{items,total,page,page_size,keyword}` and supports keyword search on `pattern`.
- Backup payload includes `data_format_version`; restore requires compatible version.
- ABP import skips comments, exception rules (`@@`), and element hiding rules.

## Verify

```bash
go test ./...
```

```bash
curl -x http://127.0.0.1:5128 http://example.com -I
```

```bash
curl http://127.0.0.1:5080/api/stats
```

## License

Licensed under the Apache License, Version 2.0. See `LICENSE`.
