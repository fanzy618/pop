package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// PUT /api/config changing default_action must take effect for traffic that
// matches no rule: subsequent unmatched requests now hit the new default.
func TestConfigPUT_ChangesDefaultActionAffectsProxy(t *testing.T) {
	t.Parallel()

	consoleURL, proxyURL, _, client := setupConsoleAndProxy(t)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(target.Close)

	proxyParsed, err := url.Parse(proxyURL)
	if err != nil {
		t.Fatalf("parse proxy url: %v", err)
	}
	proxyClient := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyParsed)}}

	// Baseline: default action is DIRECT, request succeeds.
	resp, err := proxyClient.Get(target.URL + "/baseline")
	if err != nil {
		t.Fatalf("baseline GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("baseline status=%d, want=200 (DIRECT default)", resp.StatusCode)
	}

	// Flip default_action to BLOCK via PUT /api/config.
	putBody, _ := json.Marshal(map[string]any{
		"proxy_listen":   "127.0.0.1:5128",
		"console_listen": "127.0.0.1:5080",
		"sqlite_path":    "./pop.sqlite",
		"default_action": "BLOCK",
	})
	req, _ := http.NewRequest(http.MethodPut, consoleURL+"/api/config", strings.NewReader(string(putBody)))
	req.Header.Set("Content-Type", "application/json")
	putResp, err := client.Do(req)
	if err != nil {
		t.Fatalf("PUT config: %v", err)
	}
	if putResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(putResp.Body)
		t.Fatalf("PUT status=%d body=%s", putResp.StatusCode, strings.TrimSpace(string(raw)))
	}
	_ = putResp.Body.Close()

	// Now unmatched request hits BLOCK → 404.
	resp2, err := proxyClient.Get(target.URL + "/after-flip")
	if err != nil {
		t.Fatalf("after-flip GET: %v", err)
	}
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Fatalf("after-flip status=%d, want=404 (BLOCK default)", resp2.StatusCode)
	}
}

// PUT /api/config with pac_proxy_addr makes /proxy.pac advertise that addr
// instead of the auto-inferred host:port.
func TestConfigPUT_PACOverride(t *testing.T) {
	t.Parallel()

	consoleURL, _, _, client := setupConsoleAndProxy(t)

	// At least one PROXY rule is needed so PAC includes a "PROXY <addr>" line.
	postJSON(t, client, consoleURL+"/api/upstreams", map[string]any{
		"name": "u1", "url": "http://127.0.0.1:18080", "enabled": true,
	}, http.StatusCreated)
	upResp, err := client.Get(consoleURL + "/api/upstreams")
	if err != nil {
		t.Fatalf("GET upstreams: %v", err)
	}
	var upstreams []map[string]any
	_ = json.NewDecoder(upResp.Body).Decode(&upstreams)
	_ = upResp.Body.Close()
	if len(upstreams) == 0 {
		t.Fatalf("expected at least one upstream")
	}
	upID := int64(upstreams[0]["id"].(float64))
	postJSON(t, client, consoleURL+"/api/rules", map[string]any{
		"enabled": true, "pattern": "needs-proxy.test", "action": "PROXY", "upstream_id": upID,
	}, http.StatusCreated)

	const overrideAddr = "10.99.0.1:7777"
	putBody, _ := json.Marshal(map[string]any{
		"proxy_listen":   "127.0.0.1:5128",
		"console_listen": "127.0.0.1:5080",
		"sqlite_path":    "./pop.sqlite",
		"default_action": "DIRECT",
		"pac_proxy_addr": overrideAddr,
	})
	req, _ := http.NewRequest(http.MethodPut, consoleURL+"/api/config", strings.NewReader(string(putBody)))
	req.Header.Set("Content-Type", "application/json")
	putResp, err := client.Do(req)
	if err != nil {
		t.Fatalf("PUT config: %v", err)
	}
	if putResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(putResp.Body)
		t.Fatalf("PUT status=%d body=%s", putResp.StatusCode, strings.TrimSpace(string(raw)))
	}
	_ = putResp.Body.Close()

	pacResp, err := client.Get(consoleURL + "/proxy.pac")
	if err != nil {
		t.Fatalf("GET pac: %v", err)
	}
	defer pacResp.Body.Close()
	body, _ := io.ReadAll(pacResp.Body)
	if !strings.Contains(string(body), overrideAddr) {
		t.Fatalf("pac body missing override addr %q:\n%s", overrideAddr, string(body))
	}
}

// PUT /api/config with an invalid default_action must be rejected and must
// NOT mutate runtime state.
func TestConfigPUT_RejectsInvalidDefaultAction(t *testing.T) {
	t.Parallel()

	consoleURL, _, _, client := setupConsoleAndProxy(t)

	putBody, _ := json.Marshal(map[string]any{
		"proxy_listen":   "127.0.0.1:5128",
		"console_listen": "127.0.0.1:5080",
		"sqlite_path":    "./pop.sqlite",
		"default_action": "WAT",
	})
	req, _ := http.NewRequest(http.MethodPut, consoleURL+"/api/config", strings.NewReader(string(putBody)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("PUT config: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT status=%d, want=400", resp.StatusCode)
	}
}
