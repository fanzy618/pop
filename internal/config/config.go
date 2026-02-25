package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/fanzy618/pop/internal/rules"
	"github.com/fanzy618/pop/internal/upstream"
)

type Config struct {
	ProxyListen   string       `json:"proxy_listen"`
	ConsoleListen string       `json:"console_listen"`
	SQLitePath    string       `json:"sqlite_path,omitempty"`
	Auth          AuthConfig   `json:"auth"`
	DefaultAction rules.Action `json:"default_action"`
}

type AuthConfig struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type UpstreamConfig struct {
	ID      int64  `json:"id"`
	Name    string `json:"name,omitempty"`
	URL     string `json:"url"`
	Enabled bool   `json:"enabled"`
}

type RuleConfig struct {
	ID          int64        `json:"id"`
	Enabled     bool         `json:"enabled"`
	Pattern     string       `json:"pattern"`
	Action      rules.Action `json:"action"`
	UpstreamID  int64        `json:"upstream_id,omitempty"`
	BlockStatus int          `json:"block_status,omitempty"`
	CreatedAt   int64        `json:"created_at,omitempty"`
}

func Default() *Config {
	return &Config{
		ProxyListen:   "127.0.0.1:8080",
		ConsoleListen: "127.0.0.1:9090",
		Auth: AuthConfig{
			Username: "admin",
			Password: "admin",
		},
		DefaultAction: rules.ActionDirect,
	}
}

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Default(), nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := Default()
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func Save(path string, cfg *Config) error {
	if cfg == nil {
		return errors.New("config cannot be nil")
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("ensure config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write temp config: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("commit config atomically: %w", err)
	}

	return nil
}

func (c *Config) Validate() error {
	if c == nil {
		return errors.New("config cannot be nil")
	}

	if c.ProxyListen == "" {
		return errors.New("proxy_listen cannot be empty")
	}
	if c.Auth.Username == "" {
		c.Auth.Username = "admin"
	}
	if c.Auth.Password == "" {
		c.Auth.Password = "admin"
	}

	if c.DefaultAction == "" {
		c.DefaultAction = rules.ActionDirect
	}

	if c.DefaultAction != rules.ActionDirect && c.DefaultAction != rules.ActionProxy && c.DefaultAction != rules.ActionBlock {
		return fmt.Errorf("unsupported default action: %q", c.DefaultAction)
	}

	return nil
}

func ValidateRuntime(upstreams []UpstreamConfig, rulesCfg []RuleConfig) error {
	upstreamIDs := make(map[int64]struct{}, len(upstreams))
	for _, up := range upstreams {
		if up.ID <= 0 {
			return errors.New("upstream id must be positive")
		}
		if up.URL == "" {
			return fmt.Errorf("upstream %d URL cannot be empty", up.ID)
		}
		if !strings.HasPrefix(strings.ToLower(up.URL), "http://") {
			return fmt.Errorf("upstream %d must use http://", up.ID)
		}
		upstreamIDs[up.ID] = struct{}{}
	}

	for _, rule := range rulesCfg {
		if rule.ID <= 0 {
			return errors.New("rule id must be positive")
		}
		if rule.Pattern == "" {
			return fmt.Errorf("rule %d pattern cannot be empty", rule.ID)
		}
		switch rule.Action {
		case rules.ActionDirect:
		case rules.ActionBlock:
			if rule.BlockStatus == 0 {
				rule.BlockStatus = 404
			}
			if rule.BlockStatus < 0 || rule.BlockStatus > 599 {
				return fmt.Errorf("rule %d has invalid block_status", rule.ID)
			}
		case rules.ActionProxy:
			if rule.UpstreamID <= 0 {
				return fmt.Errorf("rule %d must set upstream_id for PROXY action", rule.ID)
			}
			if _, ok := upstreamIDs[rule.UpstreamID]; !ok {
				return fmt.Errorf("rule %d references unknown upstream %d", rule.ID, rule.UpstreamID)
			}
		default:
			return fmt.Errorf("rule %d has unsupported action %q", rule.ID, rule.Action)
		}
	}

	return nil
}

func (c *Config) BuildMatcher(rulesCfg []RuleConfig) *rules.Matcher {
	ordered := make([]RuleConfig, 0, len(rulesCfg))
	ordered = append(ordered, rulesCfg...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].CreatedAt == ordered[j].CreatedAt {
			return ordered[i].ID > ordered[j].ID
		}
		return ordered[i].CreatedAt > ordered[j].CreatedAt
	})

	ruleSet := make([]rules.Rule, 0, len(ordered))
	for _, r := range ordered {
		upstreamID := ""
		if r.UpstreamID > 0 {
			upstreamID = strconv.FormatInt(r.UpstreamID, 10)
		}
		ruleSet = append(ruleSet, rules.Rule{
			ID:          strconv.FormatInt(r.ID, 10),
			Enabled:     r.Enabled,
			Pattern:     r.Pattern,
			Action:      r.Action,
			UpstreamID:  upstreamID,
			BlockStatus: r.BlockStatus,
		})
	}

	return rules.NewMatcher(ruleSet, rules.Decision{Action: c.DefaultAction})
}

func BuildUpstreamConfigs(upstreamsCfg []UpstreamConfig) []upstream.Config {
	out := make([]upstream.Config, 0, len(upstreamsCfg))
	for _, up := range upstreamsCfg {
		out = append(out, upstream.Config{
			ID:      strconv.FormatInt(up.ID, 10),
			URL:     up.URL,
			Enabled: up.Enabled,
		})
	}
	return out
}
