package config

import (
	"testing"

	"github.com/fanzy618/pop/internal/rules"
)

func TestDefaultValues(t *testing.T) {
	t.Parallel()

	cfg := Default()
	if cfg.ProxyListen != "0.0.0.0:5128" {
		t.Fatalf("proxy_listen=%q", cfg.ProxyListen)
	}
	if cfg.ConsoleListen != "127.0.0.1:5080" {
		t.Fatalf("console_listen=%q", cfg.ConsoleListen)
	}
	if cfg.DefaultAction != rules.ActionDirect {
		t.Fatalf("default_action=%q", cfg.DefaultAction)
	}
}

func TestValidateRejectsUnknownDefaultAction(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.DefaultAction = "WAT"
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected Validate to reject unknown default_action")
	}
}
