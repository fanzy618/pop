package integration

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fanzy618/pop/internal/config"
	"github.com/fanzy618/pop/internal/console"
	"github.com/fanzy618/pop/internal/proxy"
	"github.com/fanzy618/pop/internal/telemetry"
)

func TestConsoleAuthRequired(t *testing.T) {
	t.Parallel()

	consoleURL, _, _, _ := setupConsoleAndProxy(t)

	resp, err := http.Get(consoleURL + "/api/stats")
	if err != nil {
		t.Fatalf("GET stats: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d, want=%d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestConsoleRulesCRUDAndReorder(t *testing.T) {
	t.Parallel()

	consoleURL, _, cfgPath, authClient := setupConsoleAndProxy(t)

	rule1 := map[string]any{"id": "r1", "enabled": true, "order": 2, "pattern": "alpha.test", "action": "DIRECT"}
	rule2 := map[string]any{"id": "r2", "enabled": true, "order": 1, "pattern": "*ads*", "action": "BLOCK", "block_status": 410}

	postJSON(t, authClient, consoleURL+"/api/rules", rule1, http.StatusCreated)
	postJSON(t, authClient, consoleURL+"/api/rules", rule2, http.StatusCreated)
	postJSON(t, authClient, consoleURL+"/api/rules/reorder", map[string]any{"ids": []string{"r1", "r2"}}, http.StatusOK)

	resp, err := authClient.Get(consoleURL + "/api/rules")
	if err != nil {
		t.Fatalf("GET rules: %v", err)
	}
	defer resp.Body.Close()

	var rulesList []config.RuleConfig
	if err := json.NewDecoder(resp.Body).Decode(&rulesList); err != nil {
		t.Fatalf("decode rules: %v", err)
	}

	orders := map[string]int{}
	for _, item := range rulesList {
		orders[item.ID] = item.Order
	}
	if orders["r1"] != 1 || orders["r2"] != 2 {
		t.Fatalf("unexpected rules order map: %+v", orders)
	}

	persisted, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load persisted config: %v", err)
	}
	if len(persisted.Rules) != 2 {
		t.Fatalf("persisted rules length=%d, want 2", len(persisted.Rules))
	}
}

func TestConsoleStatsActivitiesAndSSE(t *testing.T) {
	t.Parallel()

	consoleURL, proxyURL, _, authClient := setupConsoleAndProxy(t)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(target.Close)

	proxyParsed, err := url.Parse(proxyURL)
	if err != nil {
		t.Fatalf("parse proxy url: %v", err)
	}
	proxyClient := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyParsed)}}

	eventSeen := make(chan struct{}, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodGet, consoleURL+"/api/activities/stream", nil)
		req.SetBasicAuth("admin", "admin")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		br := bufio.NewReader(resp.Body)
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			if strings.HasPrefix(line, "data: ") {
				select {
				case eventSeen <- struct{}{}:
				default:
				}
				return
			}
		}
	}()

	resp, err := proxyClient.Get(target.URL + "/telemetry")
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	_ = resp.Body.Close()

	select {
	case <-eventSeen:
	case <-time.After(3 * time.Second):
		t.Fatalf("did not receive SSE activity event")
	}

	statsResp, err := authClient.Get(consoleURL + "/api/stats")
	if err != nil {
		t.Fatalf("GET stats: %v", err)
	}
	defer statsResp.Body.Close()

	var stats map[string]any
	if err := json.NewDecoder(statsResp.Body).Decode(&stats); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if stats["total_requests"].(float64) < 1 {
		t.Fatalf("expected total_requests >= 1, got %v", stats["total_requests"])
	}

	actsResp, err := authClient.Get(consoleURL + "/api/activities?limit=5")
	if err != nil {
		t.Fatalf("GET activities: %v", err)
	}
	defer actsResp.Body.Close()

	var events []map[string]any
	if err := json.NewDecoder(actsResp.Body).Decode(&events); err != nil {
		t.Fatalf("decode activities: %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("expected at least one activity event")
	}
}

func setupConsoleAndProxy(t *testing.T) (consoleURL string, proxyURL string, configPath string, authClient *http.Client) {
	t.Helper()

	cfgPath := filepath.Join(t.TempDir(), "pop.json")
	cfg := config.Default()
	cfg.Auth.Username = "admin"
	cfg.Auth.Password = "admin"

	proxyServer := proxy.NewServer()
	store := telemetry.NewStore(1000, time.Minute)
	proxyServer.SetTelemetry(store)

	consoleServer, err := console.NewServer(cfg, cfgPath, proxyServer, store)
	if err != nil {
		t.Fatalf("new console server: %v", err)
	}

	consoleHTTP := httptest.NewServer(consoleServer)
	t.Cleanup(consoleHTTP.Close)

	proxyHTTP := httptest.NewServer(proxyServer)
	t.Cleanup(proxyHTTP.Close)

	transport := &http.Transport{}
	authClient = &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		req2 := req.Clone(req.Context())
		req2.SetBasicAuth("admin", "admin")
		return transport.RoundTrip(req2)
	})}

	return consoleHTTP.URL, proxyHTTP.URL, cfgPath, authClient
}

func postJSON(t *testing.T, client *http.Client, url string, payload any, wantStatus int) {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != wantStatus {
		t.Fatalf("post %s status=%d want=%d", url, resp.StatusCode, wantStatus)
	}
}

type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
