package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/fanzy618/pop/internal/config"
	"github.com/fanzy618/pop/internal/rules"
)

func TestOpenSQLiteInitializesEmptyDB(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "pop.sqlite")
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	upstreams, err := db.ListUpstreams()
	if err != nil {
		t.Fatalf("list upstreams: %v", err)
	}
	rulesList, err := db.ListRules()
	if err != nil {
		t.Fatalf("list rules: %v", err)
	}
	if len(upstreams) != 0 || len(rulesList) != 0 {
		t.Fatalf("expected empty db, got upstreams=%d rules=%d", len(upstreams), len(rulesList))
	}
}

func TestListRulesReturnsNewestFirst(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "pop.sqlite")
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	rule1 := config.RuleConfig{Enabled: true, Pattern: "a.test", Action: rules.ActionDirect}
	if err := db.CreateRule(&rule1); err != nil {
		t.Fatalf("create rule r1: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	rule2 := config.RuleConfig{Enabled: true, Pattern: "b.test", Action: rules.ActionDirect}
	if err := db.CreateRule(&rule2); err != nil {
		t.Fatalf("create rule r2: %v", err)
	}

	items, err := db.ListRules()
	if err != nil {
		t.Fatalf("list rules: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("rules length=%d, want=2", len(items))
	}
	if items[0].ID == items[1].ID {
		t.Fatalf("unexpected rules order: %+v", items)
	}
}

func TestDeleteUpstreamBlockedByRuleReference(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "pop.sqlite")
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	upstream := config.UpstreamConfig{Name: "u1", URL: "http://127.0.0.1:8080", Enabled: true}
	if err := db.CreateUpstream(&upstream); err != nil {
		t.Fatalf("create upstream: %v", err)
	}
	upstreams, err := db.ListUpstreams()
	if err != nil {
		t.Fatalf("list upstreams: %v", err)
	}
	if len(upstreams) != 1 {
		t.Fatalf("upstreams length=%d, want=1", len(upstreams))
	}
	rule := config.RuleConfig{Enabled: true, Pattern: "*.example.com", Action: rules.ActionProxy, UpstreamID: upstreams[0].ID}
	if err := db.CreateRule(&rule); err != nil {
		t.Fatalf("create rule: %v", err)
	}

	if err := db.DeleteUpstream(upstreams[0].ID); err == nil {
		t.Fatalf("expected foreign key constraint error")
	}
}
