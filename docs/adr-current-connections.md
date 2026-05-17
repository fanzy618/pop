# ADR: Live "Current Connections" view

**Status:** Proposed
**Date:** 2026-05-17
**Scope:** new console tab + backend wiring + data-path byte instrumentation

## Context

The existing console exposes only *post-mortem* observability:

- `/api/stats` — aggregate counters (`in_flight` is just a number; no per-connection detail).
- `/api/activities[/stream]` — events emitted **on Finish**; an in-flight request is invisible until it ends.
- `bytes_in/out` for HTTP is captured by `responseRecorder` after the response completes; **`CONNECT` tunnels (i.e. all HTTPS) are not byte-counted at all** ([proxy/server.go:397](internal/proxy/server.go:397) `tunnel()` does plain `io.Copy` without instrumentation).

The user wants a live view of in-flight requests, with per-connection transfer speed, total bytes, and duration. That requires:

1. A live registry of in-flight requests, queryable mid-request.
2. Per-connection byte counters updated *during* transfer (not at the end), including CONNECT tunnels.
3. A console endpoint and UI tab that reflect the registry in (near) real time.

Pop is a personal-use proxy. We can prioritize simplicity over scale; concurrency in the hundreds is the working assumption.

## Decision

Add a **`telemetry.Connections`** in-memory registry, wire the proxy to register/deregister each request and update atomic byte counters as bytes flow, expose a **REST polling endpoint** `GET /api/connections`, and render a new **`/connections` console tab** that polls every 1 s and computes speeds client-side as `Δbytes / Δt`.

## Options considered

### Option A — REST polling with server-held registry  *(chosen)*

| Dimension | Assessment |
|---|---|
| Complexity | Low |
| Latency to UI | 1 s (poll interval) |
| Server cost | Per-request: 1 map insert + N atomic adds during transfer + 1 map delete |
| Memory | O(active connections) — bounded by a cap |
| Reuses existing patterns | Yes — matches `/api/stats/history` polling shape |

**Pros**: simplest end-to-end. UI is a static page that does fetch + diff. Polling is also the existing pattern for stats/history, so users get a consistent mental model. Backend changes are local to telemetry + proxy.

**Cons**: 1 s lag for new/closed connections; UI must compute speed itself (~5 lines of JS).

### Option B — SSE event stream extension

Extend `/api/activities/stream` with new event types `started`, `tick`, `ended`. UI maintains a connection table from the stream.

| Dimension | Assessment |
|---|---|
| Complexity | Medium |
| Latency to UI | Near-zero for open/close; tick interval for speed |
| Server cost | Same registry plus periodic tick goroutine pushing to N subscribers |
| Reuses existing patterns | Partial — same SSE channel, but a new event-type vocabulary |

**Pros**: instant feedback for new and closed connections.

**Cons**: UI state machine more complex (handle three event types + reconcile dropped messages); periodic ticks for speed still required, so the "real-time" advantage is mostly cosmetic; SSE buffer overflow under burst could lose `ended` events and leak rows. The wire format becomes harder to evolve once two consumers (activities + connections) share it.

### Option C — WebSocket bidirectional

Push events and accept subscription filters from client.

| Dimension | Assessment |
|---|---|
| Complexity | High |
| Server cost | New ws lib or hand-rolled framing |
| Reuses existing patterns | No |

**Pros**: most flexible for future features.

**Cons**: zero current need for bidirectionality; new transport for a feature that polling solves; pulls in either a dep or a hand-rolled frame parser.

## Trade-off analysis

Polling (A) loses ~1 s of latency vs SSE (B) but saves the complexity of a second event vocabulary, dropped-event reconciliation, and an additional tick goroutine. For a human-facing dashboard updated by the user's eyes, 1 s is invisible.

The registry + atomic-counter design is identical across A and B, so the choice is purely the wire format. We can swap to SSE later without touching the registry if needed; nothing locks in.

## Data model

```go
// internal/telemetry/connections.go
type ConnState struct {
    ID         uint64       // monotonic, assigned on open
    StartedAt  time.Time
    Client     string       // remote addr
    Method     string       // GET / CONNECT / ...
    Host       string       // normalized
    Action     string       // DIRECT / PROXY / BLOCK
    RuleID     string       // matched rule, "" if default
    UpstreamID string       // for PROXY, "" otherwise
    BytesIn    atomic.Int64 // client → upstream / target (live)
    BytesOut   atomic.Int64 // upstream / target → client (live)
}

type Connections struct {
    cap    int                      // bounded; default 4096
    nextID atomic.Uint64
    mu     sync.RWMutex
    active map[uint64]*ConnState
}

func (c *Connections) Open(seed ConnState) *ConnState
func (c *Connections) Close(id uint64)
func (c *Connections) Snapshot() []ConnSnapshot   // copies fields incl. atomics → plain ints
```

A bounded cap (default 4096) prevents pathological growth: if the registry is full, the proxy still serves the request — it's just untracked. Logged once when first hit.

## Wire format

```
GET /api/connections
200 OK
[
  { "id": 12, "client": "10.0.0.4:51230",
    "method": "CONNECT", "host": "youtube.com:443",
    "action": "PROXY", "upstream_id": "1",
    "started_at": "2026-05-17T10:32:11Z",
    "duration_ms": 4321,
    "bytes_in": 5210, "bytes_out": 384112 }
]
```

UI computes:
- `duration` is server-supplied (avoid clock skew on client)
- `speed_in / speed_out` = `(now.bytes - prev.bytes) / (now.duration - prev.duration)` per id

## Data-path instrumentation

Two new small wrappers in `internal/proxy/`:

```go
type countedReader struct { r io.Reader; n *atomic.Int64 }
func (c *countedReader) Read(p []byte) (int, error) { n, err := c.r.Read(p); c.n.Add(int64(n)); return n, err }

type countedConn struct { net.Conn; in, out *atomic.Int64 }
func (c *countedConn) Read(p []byte) (int, error)  { n, e := c.Conn.Read(p);  c.in.Add(int64(n));  return n, e }
func (c *countedConn) Write(p []byte) (int, error) { n, e := c.Conn.Write(p); c.out.Add(int64(n)); return n, e }
```

Wiring points in `proxy.Server.ServeHTTP`:

1. Open the registry entry **after** `decide()`, **before** dispatch. `defer Close(id)`.
2. `handleHTTP`: wrap `upReq.Body` with `countedReader` pointing at `conn.BytesIn`; reuse `responseRecorder` but point its byte counter at `conn.BytesOut`.
3. `handleConnect` and `handleConnectViaUpstream`: wrap the **client conn** with `countedConn` whose `in/out` map to `conn.BytesIn/BytesOut` before passing to `tunnel()`.

Side benefit: `telemetry.Store.bytesIn/bytesOut` global counters can finally include CONNECT traffic by reading from the connection counters at `Close` time (a one-line addition).

## Console wiring

- New interface in `internal/console/deps.go`:
  ```go
  type ConnectionsFeed interface { Snapshot() []ConnSnapshot }
  ```
  `*telemetry.Connections` satisfies it. Threaded through `console.NewServer`.
- New file `internal/console/connections.go` with `handleConnections` (REST GET) wired at `/api/connections` in `server.go` mux.
- `assets/connections.html` + a small JS block (or extension to `app.js`) for the table and 1-s poll loop.
- `pages.go`: add `/connections` → `assets/connections.html`.
- Nav links in existing pages updated.

## Tests

| Layer | Cases |
|---|---|
| `telemetry.Connections` unit | Open/Close happy path; cap eviction policy; concurrent Open/Close + Snapshot race-clean |
| `proxy` integration | HTTP POST with 5 MB body → in-flight Snapshot shows non-zero `bytes_in` mid-request; finished request disappears |
| `proxy` integration | CONNECT tunnel ferries N bytes both ways → Snapshot reflects byte counts as they grow; closes correctly |
| `console` integration | `GET /api/connections` shape; empty when idle; populated during overlapping requests |
| Bound | Open beyond cap → 4097th request still served but not in Snapshot |

## Consequences

**Easier:**
- Observe HTTPS traffic per-flow (currently invisible).
- Future "kill connection" feature has a registry to act on.
- Global byte counters can become accurate for CONNECT traffic.

**Harder / risks:**
- Per-byte path now goes through one extra atomic add per `io.Copy` chunk (~32 KiB). Negligible (<0.5 ns).
- The cap means in pathological cases, some connections are silently untracked. Logged once.
- `ConnState` is a public type — changing fields is a wire-format break for the console UI; treat as a versioned API.

**To revisit:**
- If browser tabs accumulate poll load, switch to SSE (Option B) without touching the registry.
- "Kill connection" / "block this client" UI actions can land on top of the registry.

## Action items

1. [ ] Branch already created: `feat/current-connections`.
2. [ ] Backend: `telemetry.Connections` registry + unit tests.
3. [ ] Backend: proxy counted wrappers + integration tests for HTTP body and CONNECT tunnel.
4. [ ] Backend: `console` interface, handler, route.
5. [ ] Frontend: `connections.html` + JS; nav link in all pages; route in `pages.go`.
6. [ ] Doc tweak: append a one-liner to `README.md` and `CLAUDE.md` mentioning the new tab and endpoint.
7. [ ] Open PR; run `make test` (with `-race`).
