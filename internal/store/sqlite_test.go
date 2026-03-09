package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/fanzy618/pop/internal/config"
	"github.com/fanzy618/pop/internal/rules"
)

func seedBackupData(t *testing.T, db *SQLite) {
	t.Helper()
	up := config.UpstreamConfig{Name: "backup-up", URL: "http://127.0.0.1:18080", Enabled: true}
	if err := db.CreateUpstream(&up); err != nil {
		t.Fatalf("create upstream: %v", err)
	}
	r := config.RuleConfig{Enabled: true, Pattern: "backup.local", Action: rules.ActionProxy, UpstreamID: up.ID}
	if err := db.CreateRule(&r); err != nil {
		t.Fatalf("create rule: %v", err)
	}
}

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
	rule := config.RuleConfig{Enabled: true, Pattern: "example.com", Action: rules.ActionProxy, UpstreamID: upstreams[0].ID}
	if err := db.CreateRule(&rule); err != nil {
		t.Fatalf("create rule: %v", err)
	}

	if err := db.DeleteUpstream(upstreams[0].ID); err == nil {
		t.Fatalf("expected foreign key constraint error")
	}
}

func TestExportRestoreBackupRoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "pop.sqlite")
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	seedBackupData(t, db)

	backup, err := db.ExportBackup()
	if err != nil {
		t.Fatalf("export backup: %v", err)
	}
	if backup.DataFormatVersion != CurrentDataFormatVersion {
		t.Fatalf("version=%s, want=%s", backup.DataFormatVersion, CurrentDataFormatVersion)
	}
	if len(backup.Upstreams) != 1 || len(backup.Rules) != 1 {
		t.Fatalf("unexpected backup sizes: upstreams=%d rules=%d", len(backup.Upstreams), len(backup.Rules))
	}

	if err := db.DeleteRule(backup.Rules[0].ID); err != nil {
		t.Fatalf("delete rule: %v", err)
	}
	if err := db.DeleteUpstream(backup.Upstreams[0].ID); err != nil {
		t.Fatalf("delete upstream: %v", err)
	}

	if err := db.RestoreBackup(backup); err != nil {
		t.Fatalf("restore backup: %v", err)
	}

	rulesList, err := db.ListRules()
	if err != nil {
		t.Fatalf("list rules: %v", err)
	}
	upstreams, err := db.ListUpstreams()
	if err != nil {
		t.Fatalf("list upstreams: %v", err)
	}
	if len(upstreams) != 1 || len(rulesList) != 1 {
		t.Fatalf("unexpected restore sizes: upstreams=%d rules=%d", len(upstreams), len(rulesList))
	}
	if rulesList[0].Pattern != "backup.local" {
		t.Fatalf("restored pattern=%s", rulesList[0].Pattern)
	}
}

func TestRestoreBackupRejectsUnsupportedVersion(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "pop.sqlite")
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	err = db.RestoreBackup(&BackupPayload{DataFormatVersion: "999"})
	if err == nil {
		t.Fatalf("expected restore to fail for unsupported version")
	}
}

func TestCreateRuleOverridesSamePatternAndRefreshesCreatedAt(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "pop.sqlite")
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	first := config.RuleConfig{Enabled: true, Pattern: "Example.COM", Action: rules.ActionDirect}
	if err := db.CreateRule(&first); err != nil {
		t.Fatalf("create first rule: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	second := config.RuleConfig{Enabled: true, Pattern: "example.com.", Action: rules.ActionBlock, BlockStatus: 410}
	if err := db.CreateRule(&second); err != nil {
		t.Fatalf("create second rule: %v", err)
	}

	rulesList, err := db.ListRules()
	if err != nil {
		t.Fatalf("list rules: %v", err)
	}
	if len(rulesList) != 1 {
		t.Fatalf("rules length=%d, want=1", len(rulesList))
	}
	if rulesList[0].Action != rules.ActionBlock {
		t.Fatalf("action=%s, want=%s", rulesList[0].Action, rules.ActionBlock)
	}
	if rulesList[0].BlockStatus != 404 {
		t.Fatalf("block_status=%d, want=404", rulesList[0].BlockStatus)
	}
	if rulesList[0].CreatedAt <= first.CreatedAt {
		t.Fatalf("created_at not refreshed: first=%d current=%d", first.CreatedAt, rulesList[0].CreatedAt)
	}
}
