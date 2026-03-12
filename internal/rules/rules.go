package rules

import (
	"fmt"
	"strings"
)

type Action string

const (
	ActionDirect Action = "DIRECT"
	ActionProxy  Action = "PROXY"
	ActionBlock  Action = "BLOCK"
)

type Rule struct {
	ID          string
	Enabled     bool
	Order       int
	Pattern     string
	Action      Action
	UpstreamID  string
	BlockStatus int
}

type Decision struct {
	Action      Action
	RuleID      string
	UpstreamID  string
	BlockStatus int
	Matched     bool
}

type Matcher struct {
	rules           []Rule
	defaultDecision Decision
}

func NewMatcher(rules []Rule, defaultDecision Decision) *Matcher {
	if defaultDecision.Action == "" {
		defaultDecision.Action = ActionDirect
	}
	return &Matcher{rules: rules, defaultDecision: defaultDecision}
}

func (m *Matcher) Decide(rawHost string) Decision {
	host := normalizePattern(rawHost)
	bestLen := -1
	bestOrder := len(m.rules)
	var best Decision
	for i, rule := range m.rules {
		if !rule.Enabled {
			continue
		}
		pattern := normalizePattern(rule.Pattern)
		if pattern == "" {
			continue
		}
		if matchPattern(host, pattern) {
			patternLen := len(strings.TrimPrefix(pattern, "*."))
			if patternLen < bestLen || (patternLen == bestLen && i > bestOrder) {
				continue
			}
			decision := Decision{
				Action:      rule.Action,
				RuleID:      rule.ID,
				UpstreamID:  rule.UpstreamID,
				BlockStatus: rule.BlockStatus,
				Matched:     true,
			}
			if decision.Action == ActionBlock && decision.BlockStatus == 0 {
				decision.BlockStatus = 404
			}
			if decision.Action == "" {
				decision.Action = ActionDirect
			}
			best = decision
			bestLen = patternLen
			bestOrder = i
		}
	}

	if bestLen >= 0 {
		return best
	}
	return m.defaultDecision
}

func (m *Matcher) GeneratePAC(proxyAddr string) string {
	var b strings.Builder
	b.WriteString("function FindProxyForURL(url, host) {\n")
	b.WriteString("  host = host.toLowerCase();\n")

	for _, rule := range m.rules {
		if !rule.Enabled {
			continue
		}
		pattern := normalizePattern(rule.Pattern)
		if pattern == "" {
			continue
		}

		result := "DIRECT"
		switch rule.Action {
		case ActionProxy:
			result = "PROXY " + proxyAddr
		case ActionBlock:
			result = "PROXY 127.0.0.1:65535"
		case ActionDirect:
			result = "DIRECT"
		}

		// pop matches host or suffix (e.g., google.com matches google.com and sub.google.com)
		b.WriteString(fmt.Sprintf("  if (host === %q || host.endsWith(%q)) return %q;\n", pattern, "."+pattern, result))
	}

	defaultResult := "DIRECT"
	switch m.defaultDecision.Action {
	case ActionProxy:
		defaultResult = "PROXY " + proxyAddr
	case ActionBlock:
		defaultResult = "PROXY 127.0.0.1:65535"
	}
	b.WriteString(fmt.Sprintf("  return %q;\n", defaultResult))
	b.WriteString("}\n")
	return b.String()
}

func matchPattern(host, pattern string) bool {
	pattern = strings.TrimPrefix(pattern, "*.")
	if pattern == "" || strings.Contains(pattern, "*") {
		return false
	}
	return host == pattern || strings.HasSuffix(host, "."+pattern)
}

func normalizePattern(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	v = strings.TrimSuffix(v, ".")
	return v
}
