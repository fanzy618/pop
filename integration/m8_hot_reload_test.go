package integration

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// While traffic is flowing, repeatedly toggle the matcher in proxy.Server.
// Every response must be either the upstream's 200 (DIRECT) or 404 (BLOCK):
// any 5xx, panic, or race detector hit means the snapshot swap is unsafe.
func TestProxy_HotReloadDuringTraffic(t *testing.T) {
	t.Parallel()

	consoleURL, proxyURL, _, client := setupConsoleAndProxy(t)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(target.Close)

	targetParsed, _ := url.Parse(target.URL)
	targetHost, _, _ := net.SplitHostPort(targetParsed.Host)
	if targetHost == "" {
		targetHost = targetParsed.Host
	}

	proxyParsed, _ := url.Parse(proxyURL)
	proxyClient := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyParsed)}, Timeout: 5 * time.Second}

	// Initial state: no rules → DIRECT default. Verify baseline succeeds.
	resp, err := proxyClient.Get(target.URL + "/seed")
	if err != nil {
		t.Fatalf("seed GET: %v", err)
	}
	_ = resp.Body.Close()

	stop := make(chan struct{})

	// Toggler: alternate between creating and deleting a BLOCK rule for the
	// target host. Each create/delete triggers rebuildRuntime in console.
	var toggleErrs atomic.Int64
	go func() {
		var ruleID int64
		for {
			select {
			case <-stop:
				return
			default:
			}

			if ruleID == 0 {
				body, _ := json.Marshal(map[string]any{
					"enabled": true, "pattern": targetHost, "action": "BLOCK",
				})
				req, _ := http.NewRequest(http.MethodPost, consoleURL+"/api/rules", strings.NewReader(string(body)))
				req.Header.Set("Content-Type", "application/json")
				r, err := client.Do(req)
				if err != nil {
					toggleErrs.Add(1)
					continue
				}
				if r.StatusCode == http.StatusCreated {
					var rule map[string]any
					_ = json.NewDecoder(r.Body).Decode(&rule)
					ruleID = int64(rule["id"].(float64))
				}
				_ = r.Body.Close()
			} else {
				req, _ := http.NewRequest(http.MethodDelete, consoleURL+"/api/rules/"+strconv.FormatInt(ruleID, 10), nil)
				r, err := client.Do(req)
				if err != nil {
					toggleErrs.Add(1)
					continue
				}
				_ = r.Body.Close()
				ruleID = 0
			}
		}
	}()

	// Workers: hammer the proxy. Only sane responses are 200 / 404.
	var wg sync.WaitGroup
	var badStatus atomic.Int64
	var reqDone atomic.Int64
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				r, err := proxyClient.Get(target.URL + "/probe")
				if err != nil {
					continue
				}
				_, _ = io.Copy(io.Discard, r.Body)
				_ = r.Body.Close()
				if r.StatusCode != http.StatusOK && r.StatusCode != http.StatusNotFound {
					badStatus.Add(1)
				}
				reqDone.Add(1)
			}
		}()
	}

	time.Sleep(500 * time.Millisecond)
	close(stop)
	wg.Wait()

	if reqDone.Load() < 10 {
		t.Fatalf("not enough requests completed: %d", reqDone.Load())
	}
	if got := badStatus.Load(); got != 0 {
		t.Fatalf("got %d responses with unexpected status (only 200/404 allowed)", got)
	}
	if got := toggleErrs.Load(); got != 0 {
		t.Fatalf("toggle goroutine reported %d errors", got)
	}
}
