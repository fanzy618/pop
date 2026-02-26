package config

import (
	"testing"

	"github.com/fanzy618/pop/internal/rules"
)

func TestDefaultValues(t *testing.T) {
	t.Parallel()

	cfg := Default()
	if cfg.ProxyListen != "0.0.0.0:5000" {
		t.Fatalf("proxy_listen=%q", cfg.ProxyListen)
	}
	if cfg.ConsoleListen != "127.0.0.1:5080" {
		t.Fatalf("console_listen=%q", cfg.ConsoleListen)
	}
	if cfg.DefaultAction != rules.ActionDirect {
		t.Fatalf("default_action=%q", cfg.DefaultAction)
	}
}

func TestValidateRejectsNonHTTPUpstream(t *testing.T) {
	t.Parallel()

	if err := ValidateRuntime(
		[]UpstreamConfig{{ID: 1, URL: "socks5://127.0.0.1:1080", Enabled: true}},
		nil,
	); err == nil {
		t.Fatalf("expected validate to reject non-http upstream")
	}
}
