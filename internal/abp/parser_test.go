package abp

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseDomains_SkipsUnsupportedAndDedupes(t *testing.T) {
	t.Parallel()

	content := strings.Join([]string{
		"! comment",
		"[Adblock Plus 2.0]",
		"||one.example.com^",
		"two.example.org",
		"||one.example.com^",       // duplicate
		"@@||allowed.example.net^", // exception
		"foo.example.org##.banner", // element hiding
		"foo.example.org#@#.ok",
		"foo.example.org#?#.css",
		"/regex.*pattern/",
	}, "\n")

	domains, total, skipped := ParseDomains(content)
	if total != 10 {
		t.Fatalf("total=%d, want 10", total)
	}
	if skipped != 5 {
		t.Fatalf("skipped=%d, want 5", skipped)
	}
	want := []string{"one.example.com", "two.example.org"}
	if !reflect.DeepEqual(domains, want) {
		t.Fatalf("domains=%v, want %v", domains, want)
	}
}

func TestParseDomains_RejectsBadHosts(t *testing.T) {
	t.Parallel()

	domains, _, skipped := ParseDomains(strings.Join([]string{
		"-leading.dash",
		"trailing-",
		"double..dot",
		"no-tld",
	}, "\n"))

	if len(domains) != 0 {
		t.Fatalf("domains=%v, want empty", domains)
	}
	if skipped != 4 {
		t.Fatalf("skipped=%d, want 4", skipped)
	}
}
