package console

import (
	"time"

	"github.com/fanzy618/pop/internal/model"
	"github.com/fanzy618/pop/internal/proxy"
	"github.com/fanzy618/pop/internal/store"
	"github.com/fanzy618/pop/internal/telemetry"
)

// Store is the persistence surface the console depends on. Backed in
// production by *store.SQLite; can be replaced by an in-memory fake in
// tests without dragging the whole package along.
type Store interface {
	ListUpstreams() ([]model.Upstream, error)
	CreateUpstream(*model.Upstream) error
	UpdateUpstream(int64, model.Upstream) error
	DeleteUpstream(int64) error

	ListRules() ([]model.Rule, error)
	ListRulesPage(store.RuleListOptions) (store.RuleListPage, error)
	CreateRule(*model.Rule) error
	UpdateRule(int64, model.Rule) error
	DeleteRule(int64) error

	ExportBackup() (*store.BackupPayload, error)
	RestoreBackup(*store.BackupPayload) error
}

// RouteSink is the routing-update surface the console pushes snapshots into.
// Backed by *proxy.Server. Snapshot() is used by the PAC handler to read the
// matcher; Publish() by the reloader to swap the snapshot atomically.
type RouteSink interface {
	Publish(*proxy.RouteSnapshot)
	Snapshot() *proxy.RouteSnapshot
}

// TelemetryFeed is the live-stats surface backing /api/stats and SSE. The
// production type telemetry.Store satisfies it; a fake can stand in for
// tests that don't need real counters.
type TelemetryFeed interface {
	Snapshot() telemetry.Stats
	Recent(int) []telemetry.Event
	Subscribe(int) (<-chan telemetry.Event, func())
}

// SysHistoryFeed is optional. When console.NewServer receives nil for it,
// /api/stats/history returns an empty array.
type SysHistoryFeed interface {
	History(time.Duration) []telemetry.Sample
}

// ConnectionsFeed exposes the in-flight connection registry used by
// /api/connections. Optional — when nil, the endpoint returns an empty
// array.
type ConnectionsFeed interface {
	Snapshot() []telemetry.ConnSnapshot
}

// Compile-time checks that the production types satisfy the interfaces.
// Update these if the interfaces grow.
var (
	_ Store           = (*store.SQLite)(nil)
	_ RouteSink       = (*proxy.Server)(nil)
	_ TelemetryFeed   = (*telemetry.Store)(nil)
	_ SysHistoryFeed  = (*telemetry.SysStatsCollector)(nil)
	_ ConnectionsFeed = (*telemetry.Connections)(nil)
)
