# Proxy of Proxy (POP) - Engineering Guide

## Product goals

- POP is a local HTTP proxy for personal use.
- POP decides whether to connect directly, forward via a configured upstream proxy, or block.
- Priorities are, in order: usability, maintainability, low resource usage, then performance for personal traffic.

## Confirmed MVP decisions

- Default action for unmatched traffic: `DIRECT`.
- Upstream proxy support in MVP: HTTP proxy only.
- Block default status code: `404`.
- Rule matching semantics: a rule matches its domain and subdomains; the longest matching pattern wins.

## Required behavior

- User can configure OS/system proxy to POP.
- Internal domains can be routed directly.
- External domains can be routed to different upstream proxies (A/B).
- Ad domains can be blocked with status code `404` from web console rules.
- Unmatched domains use configurable default behavior.
- Web console does not require auth in current version.
- Web console shows live activity and simple runtime stats.
- Rules are persisted and restored after restart.

## Rule model

- Rule fields include: enabled flag, order, domain pattern, action, optional upstream id, optional block status.
- Domain patterns are domain suffix rules such as `example.com`, which match the root domain and all subdomains; legacy `*.example.com` is treated compatibly.

## Runtime data constraints

- Runtime activity data must not grow unbounded.
- Keep activities in a bounded in-memory ring buffer with TTL eviction.
- Stats are runtime-only and do not need persistence.

## Engineering and Quality Gate Policy

All modifications (including new features, bug fixes, and refactorings) must adhere to these rules:

- **Comprehensive Testing**: Any code change must be accompanied by relevant tests (excluding console UI tests).
- **Mandatory Validation**: Before any git commit or image push, run `go test ./...` and ensure **all** tests pass. No regressions are allowed.
- **Continuous Compliance**: PAC generation and HTTP proxy compliance must be part of the regular test suite.

For Milestones, follow this strict order:

1. Implement milestone scope.
2. Add/update tests for that scope.
3. Run `go test ./...` and confirm all tests pass.
4. Commit changes to local git.

## Milestones

- M0: Project baseline docs (`AGENT.md`).
- M1: Core proxy skeleton with DIRECT default routing.
- M2: Ordered domain rules and BLOCK action.
- M3: Upstream HTTP proxy routing.
- M4: Config persistence and restart restore.
- M5: Runtime telemetry (activity stream, stats, TTL, bounded memory).
- M6: Web console API (no UI test automation required).
