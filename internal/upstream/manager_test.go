package upstream

import "testing"

func TestManagerReplaceAndGet(t *testing.T) {
	t.Parallel()

	mgr, err := NewManager([]Config{{ID: "A", URL: "http://127.0.0.1:10080", Enabled: true}})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	if _, ok := mgr.Get("A"); !ok {
		t.Fatalf("expected upstream A to exist")
	}

	if err := mgr.Replace([]Config{{ID: "B", URL: "http://127.0.0.1:10081", Enabled: true}}); err != nil {
		t.Fatalf("replace manager configs: %v", err)
	}

	if _, ok := mgr.Get("A"); ok {
		t.Fatalf("expected upstream A to be removed")
	}
	if _, ok := mgr.Get("B"); !ok {
		t.Fatalf("expected upstream B to exist")
	}
}

func TestManagerRejectsUnsupportedScheme(t *testing.T) {
	t.Parallel()

	_, err := NewManager([]Config{{ID: "S", URL: "https://proxy.example.com:443", Enabled: true}})
	if err == nil {
		t.Fatalf("expected error for unsupported scheme")
	}
}
