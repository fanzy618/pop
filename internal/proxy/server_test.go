package proxy

import (
	"net/http"
	"testing"
)

func TestNormalizeHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "domain keeps lowercase", in: "Example.COM", want: "example.com"},
		{name: "domain with port", in: "Example.COM:443", want: "example.com:443"},
		{name: "trailing dot", in: "example.com.", want: "example.com"},
		{name: "invalid host port", in: "example.com:", want: ""},
		{name: "ipv6 host and port", in: "[::1]:8080", want: "[::1]:8080"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeHost(tc.in)
			if got != tc.want {
				t.Fatalf("normalizeHost(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRequestHost(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequest(http.MethodGet, "http://Example.com/path", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	if got, want := requestHost(req), "example.com"; got != want {
		t.Fatalf("requestHost(http req) = %q, want %q", got, want)
	}

	connectReq := &http.Request{Method: http.MethodConnect, Host: "Foo.Bar:443"}
	if got, want := requestHost(connectReq), "foo.bar:443"; got != want {
		t.Fatalf("requestHost(connect req) = %q, want %q", got, want)
	}
}
