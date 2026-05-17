package routing

import (
	"testing"

	"github.com/fanzy618/pop/internal/rules"
)

func TestParseTarget_Direct(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "DIRECT", "  DIRECT  "} {
		got, err := ParseTarget(raw)
		if err != nil {
			t.Fatalf("ParseTarget(%q): %v", raw, err)
		}
		if got.Action != rules.ActionDirect || got.UpstreamID != 0 {
			t.Fatalf("ParseTarget(%q)=%+v", raw, got)
		}
	}
}

func TestParseTarget_Upstream(t *testing.T) {
	t.Parallel()

	got, err := ParseTarget("UPSTREAM:42")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.Action != rules.ActionProxy || got.UpstreamID != 42 {
		t.Fatalf("got=%+v", got)
	}
}

func TestParseTarget_Invalid(t *testing.T) {
	t.Parallel()

	cases := []string{"UPSTREAM:", "UPSTREAM:-1", "UPSTREAM:abc", "BLOCK", "FOO"}
	for _, raw := range cases {
		if _, err := ParseTarget(raw); err == nil {
			t.Fatalf("ParseTarget(%q) accepted invalid input", raw)
		}
	}
}
