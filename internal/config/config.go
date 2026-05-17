// Package config holds the runtime parameters that govern process startup —
// listen addresses, sqlite path, default action, PAC override. Persistence
// DTOs and assembly helpers live in internal/model.
package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/fanzy618/pop/internal/rules"
)

type Config struct {
	ProxyListen   string       `json:"proxy_listen"`
	ConsoleListen string       `json:"console_listen"`
	SQLitePath    string       `json:"sqlite_path,omitempty"`
	PACProxyAddr  string       `json:"pac_proxy_addr,omitempty"`
	DefaultAction rules.Action `json:"default_action"`
}

func Default() *Config {
	return &Config{
		ProxyListen:   "0.0.0.0:5128",
		ConsoleListen: "127.0.0.1:5080",
		SQLitePath:    "./pop.sqlite",
		DefaultAction: rules.ActionDirect,
	}
}

func (c *Config) Validate() error {
	if c == nil {
		return errors.New("config cannot be nil")
	}

	c.ProxyListen = strings.TrimSpace(c.ProxyListen)
	if c.ProxyListen == "" {
		return errors.New("proxy_listen cannot be empty")
	}

	c.ConsoleListen = strings.TrimSpace(c.ConsoleListen)
	if c.ConsoleListen == "" {
		return errors.New("console_listen cannot be empty")
	}

	c.SQLitePath = strings.TrimSpace(c.SQLitePath)
	if c.SQLitePath == "" {
		c.SQLitePath = Default().SQLitePath
	}

	if c.DefaultAction == "" {
		c.DefaultAction = rules.ActionDirect
	}

	if c.DefaultAction != rules.ActionDirect && c.DefaultAction != rules.ActionProxy && c.DefaultAction != rules.ActionBlock {
		return fmt.Errorf("unsupported default action: %q", c.DefaultAction)
	}

	return nil
}
