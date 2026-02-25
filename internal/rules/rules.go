package rules

import "strings"

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
	for _, rule := range m.rules {
		if !rule.Enabled {
			continue
		}
		pattern := normalizePattern(rule.Pattern)
		if pattern == "" {
			continue
		}
		if matchPattern(host, pattern) {
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
			return decision
		}
	}

	return m.defaultDecision
}

func matchPattern(host, pattern string) bool {
	if pattern == "*" {
		return true
	}

	if strings.HasPrefix(pattern, "*.") {
		suffix := strings.TrimPrefix(pattern, "*.")
		if suffix == "" || host == suffix {
			return false
		}
		return strings.HasSuffix(host, "."+suffix)
	}

	if strings.Contains(pattern, "*") {
		return wildcardContainsMatch(host, pattern)
	}

	return host == pattern
}

func wildcardContainsMatch(host, pattern string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return host == pattern
	}

	idx := 0
	if !strings.HasPrefix(pattern, "*") {
		prefix := parts[0]
		if !strings.HasPrefix(host, prefix) {
			return false
		}
		idx = len(prefix)
		parts = parts[1:]
	}

	for i, part := range parts {
		if part == "" {
			continue
		}
		found := strings.Index(host[idx:], part)
		if found == -1 {
			return false
		}
		idx += found + len(part)

		if i == len(parts)-1 && !strings.HasSuffix(pattern, "*") {
			return idx == len(host)
		}
	}

	if !strings.HasSuffix(pattern, "*") {
		last := parts[len(parts)-1]
		if last == "" {
			return true
		}
		return strings.HasSuffix(host, last)
	}

	return true
}

func normalizePattern(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	v = strings.TrimSuffix(v, ".")
	return v
}
