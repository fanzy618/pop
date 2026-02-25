package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fanzy618/pop/internal/rules"
)

func TestSaveAndLoadRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "pop.json")

	cfg := &Config{
		ProxyListen:   "127.0.0.1:18080",
		ConsoleListen: "127.0.0.1:19090",
		SQLitePath:    "./data/pop.sqlite",
		DefaultAction: rules.ActionDirect,
	}

	if err := Save(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if loaded.ProxyListen != cfg.ProxyListen {
		t.Fatalf("proxy listen = %q, want %q", loaded.ProxyListen, cfg.ProxyListen)
	}
	if loaded.SQLitePath != cfg.SQLitePath {
		t.Fatalf("sqlite path = %q, want %q", loaded.SQLitePath, cfg.SQLitePath)
	}
}

func TestValidateRejectsNonHTTPUpstream(t *testing.T) {
	t.Parallel()

	if err := ValidateRuntime(
		[]UpstreamConfig{{ID: "A", URL: "socks5://127.0.0.1:1080", Enabled: true}},
		nil,
	); err == nil {
		t.Fatalf("expected validate to reject non-http upstream")
	}
}

func TestSaveAtomicLeavesNoTempFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "pop.json")

	if err := Save(path, Default()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp config file should not remain, stat err=%v", err)
	}
}
