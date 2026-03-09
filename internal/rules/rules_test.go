package rules

import "testing"

func TestMatcherLongestPatternWins(t *testing.T) {
	t.Parallel()

	matcher := NewMatcher([]Rule{
		{ID: "short", Enabled: true, Pattern: "example.com", Action: ActionProxy, UpstreamID: "A"},
		{ID: "long", Enabled: true, Pattern: "abc.example.com", Action: ActionBlock, BlockStatus: 451},
	}, Decision{Action: ActionDirect})

	decision := matcher.Decide("def.abc.example.com")
	if decision.RuleID != "long" {
		t.Fatalf("rule id = %q, want %q", decision.RuleID, "long")
	}
	if decision.Action != ActionBlock {
		t.Fatalf("action = %q, want %q", decision.Action, ActionBlock)
	}
}

func TestMatcherPatterns(t *testing.T) {
	t.Parallel()

	matcher := NewMatcher([]Rule{
		{ID: "exact", Enabled: true, Pattern: "example.com", Action: ActionBlock},
		{ID: "sub", Enabled: true, Pattern: "foo.com", Action: ActionProxy, UpstreamID: "B"},
		{ID: "legacy-sub", Enabled: true, Pattern: "*.legacy.com", Action: ActionProxy, UpstreamID: "C"},
	}, Decision{Action: ActionDirect})

	if d := matcher.Decide("example.com"); d.RuleID != "exact" || d.BlockStatus != 404 {
		t.Fatalf("unexpected exact decision: %+v", d)
	}

	if d := matcher.Decide("a.foo.com"); d.RuleID != "sub" || d.UpstreamID != "B" {
		t.Fatalf("unexpected sub decision: %+v", d)
	}

	if d := matcher.Decide("foo.com"); d.RuleID != "sub" || d.UpstreamID != "B" {
		t.Fatalf("root domain should match foo.com rule: %+v", d)
	}

	if d := matcher.Decide("abc-example.com"); d.Matched {
		t.Fatalf("hyphenated sibling should not match example.com: %+v", d)
	}

	if d := matcher.Decide("deep.legacy.com"); d.RuleID != "legacy-sub" || d.UpstreamID != "C" {
		t.Fatalf("legacy wildcard prefix should still work as suffix match: %+v", d)
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
