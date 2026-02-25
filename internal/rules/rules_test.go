package rules

import "testing"

func TestMatcherFirstMatchWins(t *testing.T) {
	t.Parallel()

	matcher := NewMatcher([]Rule{
		{ID: "r1", Enabled: true, Pattern: "*.example.com", Action: ActionProxy, UpstreamID: "A"},
		{ID: "r2", Enabled: true, Pattern: "api.example.com", Action: ActionBlock, BlockStatus: 451},
	}, Decision{Action: ActionDirect})

	decision := matcher.Decide("api.example.com")
	if decision.RuleID != "r1" {
		t.Fatalf("rule id = %q, want %q", decision.RuleID, "r1")
	}
	if decision.Action != ActionProxy {
		t.Fatalf("action = %q, want %q", decision.Action, ActionProxy)
	}
}

func TestMatcherPatterns(t *testing.T) {
	t.Parallel()

	matcher := NewMatcher([]Rule{
		{ID: "exact", Enabled: true, Pattern: "example.com", Action: ActionBlock},
		{ID: "sub", Enabled: true, Pattern: "*.foo.com", Action: ActionProxy, UpstreamID: "B"},
		{ID: "contains", Enabled: true, Pattern: "*ads*", Action: ActionBlock, BlockStatus: 410},
	}, Decision{Action: ActionDirect})

	if d := matcher.Decide("example.com"); d.RuleID != "exact" || d.BlockStatus != 404 {
		t.Fatalf("unexpected exact decision: %+v", d)
	}

	if d := matcher.Decide("a.foo.com"); d.RuleID != "sub" || d.UpstreamID != "B" {
		t.Fatalf("unexpected sub decision: %+v", d)
	}

	if d := matcher.Decide("foo.com"); d.Matched {
		t.Fatalf("root domain should not match *.foo.com: %+v", d)
	}

	if d := matcher.Decide("myadsdomain.net"); d.RuleID != "contains" || d.BlockStatus != 410 {
		t.Fatalf("unexpected contains decision: %+v", d)
	}
}

func TestMatcherDefaultDecision(t *testing.T) {
	t.Parallel()

	matcher := NewMatcher(nil, Decision{Action: ActionDirect})
	decision := matcher.Decide("unknown-domain.test")
	if decision.Action != ActionDirect {
		t.Fatalf("action = %q, want %q", decision.Action, ActionDirect)
	}
	if decision.Matched {
		t.Fatalf("matched = true, want false")
	}
}
