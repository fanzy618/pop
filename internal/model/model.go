// Package model holds persistence-facing domain types shared by store,
// console, and assembly code. It also exposes the small assembly helpers
// that translate these types into the in-memory runtime structures
// (rules.Matcher, upstream.Config) so the leaf packages stay free of
// persistence-shape concerns.
package model

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/fanzy618/pop/internal/rules"
	"github.com/fanzy618/pop/internal/upstream"
)

// Upstream is the persistence-facing shape of an upstream HTTP proxy entry.
type Upstream struct {
	ID      int64  `json:"id"`
	Name    string `json:"name,omitempty"`
	URL     string `json:"url"`
	Enabled bool   `json:"enabled"`
}

// Rule is the persistence-facing shape of a routing rule.
type Rule struct {
	ID          int64        `json:"id"`
	Enabled     bool         `json:"enabled"`
	Pattern     string       `json:"pattern"`
	Action      rules.Action `json:"action"`
	UpstreamID  int64        `json:"upstream_id,omitempty"`
	BlockStatus int          `json:"block_status,omitempty"`
	CreatedAt   int64        `json:"created_at,omitempty"`
}

// ValidateRuntime checks the cross-entity invariants required to run pop:
// upstream URLs are HTTP; PROXY rules point at a known upstream id; BLOCK
// rules have a sane status code.
func ValidateRuntime(upstreams []Upstream, rulesCfg []Rule) error {
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

// BuildMatcher orders rules by newest-first (created_at desc, id desc) and
// returns a Matcher with the given default action.
func BuildMatcher(items []Rule, defaultAction rules.Action) *rules.Matcher {
	ordered := make([]Rule, 0, len(items))
	ordered = append(ordered, items...)
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
	return rules.NewMatcher(ruleSet, rules.Decision{Action: defaultAction})
}

// BuildUpstreamConfigs converts persistence-facing upstreams into the runtime
// shape consumed by upstream.Manager.
func BuildUpstreamConfigs(items []Upstream) []upstream.Config {
	out := make([]upstream.Config, 0, len(items))
	for _, up := range items {
		out = append(out, upstream.Config{
			ID:      strconv.FormatInt(up.ID, 10),
			URL:     up.URL,
			Enabled: up.Enabled,
		})
	}
	return out
}
