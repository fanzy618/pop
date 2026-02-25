# Proxy of Proxy (POP)

POP is a local HTTP proxy for personal use. It decides request handling by ordered domain rules:

- `DIRECT`: connect directly
- `PROXY`: forward through a configured upstream HTTP proxy
- `BLOCK`: reject with fixed status code `404` in web console

If no rule matches, POP uses `default_action` (currently recommended and default: `DIRECT`).

## Features

- Local HTTP proxy endpoint (supports normal HTTP and `CONNECT` tunnels)
- Ordered rule matching (`first match wins`)
- Domain pattern support:
  - exact domain: `example.com`
  - wildcard subdomain: `*.example.com`
  - wildcard host contains: `*ads*`
- Multiple upstream HTTP proxies (A/B style routing)
- Runtime telemetry:
  - live activity events
  - in-flight requests
  - request/error counters
  - bandwidth counters
- Web console API with Basic Auth
- Config persistence (rules and upstreams survive restart)

## Requirements

- Go `1.25.5` (per `go.mod`)

## Quick Start

1. Create a config file as `./pop.json` (you can copy from `./pop.example.json`).
2. Run POP:

```bash
go run ./cmd/pop -config ./pop.json
```

3. Configure your OS/browser HTTP proxy to POP's `proxy_listen` (for example `127.0.0.1:8080`).
4. Access console API with Basic Auth at `http://<console_listen>/`.

## Example Config

```json
{
  "proxy_listen": "127.0.0.1:8080",
  "console_listen": "127.0.0.1:9090",
  "auth": {
    "username": "admin",
    "password": "admin"
  },
  "default_action": "DIRECT"
}
```

Rules and upstreams are persisted in SQLite and managed through Console API/UI.

## Console Data Model

- Upstream object:
  - `id` (database generated integer)
  - `name` (optional)
  - `url`
  - `enabled`
- Rule object:
  - `id` (database generated integer)
  - `pattern`
  - `enabled`
  - `action` (`DIRECT` / `PROXY` / `BLOCK`)
  - `upstream_id` (required only when action is `PROXY`, references upstream numeric id)
  - `block_status` is fixed to `404` for `BLOCK` rules created/updated via web console

## Console API

All endpoints require Basic Auth.

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
- `GET /api/stats`
- `GET /api/activities?limit=100`
- `GET /api/activities/stream` (SSE)

## Verify Locally

Run all tests:

```bash
go test ./...
```

Run a quick proxy check with curl:

```bash
curl -x http://127.0.0.1:8080 http://example.com -I
```

Check console stats (with auth):

```bash
curl -u admin:admin http://127.0.0.1:9090/api/stats
```

## Common Issues

- `407` or upstream failures:
  - Ensure upstream URL is valid and reachable.
  - MVP supports only `http://` upstream proxies.
- Requests not matching expected rule:
  - Rule order matters; first match wins.
  - Verify host pattern and whether root domain should or should not match `*.` patterns.
- No console access:
  - Confirm `console_listen` address and Basic Auth credentials.

## Notes

- Telemetry is runtime-only and not persisted.
- Activity memory is bounded by capacity and TTL eviction.

## Design Docs

- `docs/README.md`
- `docs/requirements.md`
- `docs/architecture.md`
- `docs/rules-and-routing.md`
- `docs/milestones.md`
- `docs/testing-strategy.md`
- `docs/operations.md`
