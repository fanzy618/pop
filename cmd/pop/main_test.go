package main

import "testing"

func TestResolveRuntimeConfigDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := resolveRuntimeConfig(nil, func(string) string { return "" })
	if err != nil {
		t.Fatalf("resolve config: %v", err)
	}

	if cfg.ProxyListen != "0.0.0.0:5128" {
		t.Fatalf("proxy_listen=%q", cfg.ProxyListen)
	}
	if cfg.ConsoleListen != "127.0.0.1:5080" {
		t.Fatalf("console_listen=%q", cfg.ConsoleListen)
	}
	if string(cfg.DefaultAction) != "DIRECT" {
		t.Fatalf("default_action=%q", cfg.DefaultAction)
	}
}

func TestResolveRuntimeConfigEnvOverridesDefault(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envProxyListen:   "127.0.0.1:7000",
		envConsoleListen: "127.0.0.1:7001",
		envSQLitePath:    "/tmp/pop.sqlite",
		envDefaultAction: "block",
	}

	cfg, err := resolveRuntimeConfig(nil, func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("resolve config: %v", err)
	}

	if cfg.ProxyListen != "127.0.0.1:7000" {
		t.Fatalf("proxy_listen=%q", cfg.ProxyListen)
	}
	if cfg.ConsoleListen != "127.0.0.1:7001" {
		t.Fatalf("console_listen=%q", cfg.ConsoleListen)
	}
	if cfg.SQLitePath != "/tmp/pop.sqlite" {
		t.Fatalf("sqlite_path=%q", cfg.SQLitePath)
	}
	if string(cfg.DefaultAction) != "BLOCK" {
		t.Fatalf("default_action=%q", cfg.DefaultAction)
	}
}

func TestResolveRuntimeConfigCLIOverridesEnv(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envProxyListen:   "127.0.0.1:7000",
		envConsoleListen: "127.0.0.1:7001",
		envSQLitePath:    "/tmp/pop.sqlite",
		envDefaultAction: "DIRECT",
	}

	args := []string{
		"--proxy-listen", "0.0.0.0:8000",
		"-c", "127.0.0.1:8001",
		"--sqlite-path", "/var/lib/pop.sqlite",
		"-a", "proxy",
	}

	cfg, err := resolveRuntimeConfig(args, func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("resolve config: %v", err)
	}

	if cfg.ProxyListen != "0.0.0.0:8000" {
		t.Fatalf("proxy_listen=%q", cfg.ProxyListen)
	}
	if cfg.ConsoleListen != "127.0.0.1:8001" {
		t.Fatalf("console_listen=%q", cfg.ConsoleListen)
	}
	if cfg.SQLitePath != "/var/lib/pop.sqlite" {
		t.Fatalf("sqlite_path=%q", cfg.SQLitePath)
	}
	if string(cfg.DefaultAction) != "PROXY" {
		t.Fatalf("default_action=%q", cfg.DefaultAction)
	}
}
