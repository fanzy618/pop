package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// Verify the ABP parser rejects element hiding (##, #@#, #?#), regex rules
// (/.../), exception rules (@@), and that duplicates within the same file
// collapse to one row.
func TestImportABP_SkipsRegexExceptionAndElementHiding(t *testing.T) {
	t.Parallel()

	consoleURL, _, _, client := setupConsoleAndProxy(t)

	abpText := strings.Join([]string{
		"! comment line",
		"[Adblock Plus 2.0]",
		"||good.example.com^",
		"||also-good.example.com^",
		"@@||exception.example.com^",       // exception
		"example.org##.ad-banner",          // element hiding
		"example.org#@#.unblock",           // element-hiding exception
		"example.org#?#.css-selector",      // extended element hiding
		"/regex-pattern\\d+/",              // regex
		"||dup.example.com^",
		"||dup.example.com^",               // duplicate
		"dup.example.com",                  // same host another syntax
		"",                                 // blank
	}, "\n")

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "rules.txt")
	_, _ = part.Write([]byte(abpText))
	_ = writer.WriteField("route_target", "DIRECT")
	_ = writer.WriteField("enabled", "true")
	_ = writer.Close()

	req, _ := http.NewRequest(http.MethodPost, consoleURL+"/api/data/import-abp", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("import status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	resp2, err := client.Get(consoleURL + "/api/rules?page=1&page_size=100")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer resp2.Body.Close()
	var payload rulesListResponse
	if err := json.NewDecoder(resp2.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}

	patterns := make(map[string]bool, len(payload.Items))
	for _, r := range payload.Items {
		patterns[r.Pattern] = true
	}

	wantPresent := []string{"good.example.com", "also-good.example.com", "dup.example.com"}
	for _, p := range wantPresent {
		if !patterns[p] {
			t.Fatalf("expected pattern %q in imported rules, got: %+v", p, patterns)
		}
	}

	wantAbsent := []string{
		"exception.example.com", // @@
		"regex-pattern",         // /.../ regex
		"example.org",           // only element-hiding for this host — host alone should not appear
	}
	for _, p := range wantAbsent {
		if patterns[p] {
			t.Fatalf("pattern %q should NOT be imported (it's an exception/regex/element-hiding)", p)
		}
	}

	// dedupe: only one row for dup.example.com
	dupCount := 0
	for _, r := range payload.Items {
		if r.Pattern == "dup.example.com" {
			dupCount++
		}
	}
	if dupCount != 1 {
		t.Fatalf("dup.example.com appears %d times, want 1", dupCount)
	}
}

// route_target=UPSTREAM:<id> must produce PROXY rules pointing at that
// upstream, not DIRECT rules.
func TestImportABP_RouteTargetUpstreamProducesProxyRules(t *testing.T) {
	t.Parallel()

	consoleURL, _, _, client := setupConsoleAndProxy(t)

	postJSON(t, client, consoleURL+"/api/upstreams", map[string]any{
		"name": "abp-target", "url": "http://127.0.0.1:18080", "enabled": true,
	}, http.StatusCreated)

	upResp, _ := client.Get(consoleURL + "/api/upstreams")
	var upstreams []map[string]any
	_ = json.NewDecoder(upResp.Body).Decode(&upstreams)
	_ = upResp.Body.Close()
	upID := int64(upstreams[0]["id"].(float64))

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "rules.txt")
	_, _ = part.Write([]byte("||routed.example.com^\n"))
	_ = writer.WriteField("route_target", "UPSTREAM:"+strconv.FormatInt(upID, 10))
	_ = writer.WriteField("enabled", "true")
	_ = writer.Close()

	req, _ := http.NewRequest(http.MethodPost, consoleURL+"/api/data/import-abp", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	resp2, err := client.Get(consoleURL + "/api/rules")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer resp2.Body.Close()
	var payload rulesListResponse
	if err := json.NewDecoder(resp2.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("rules=%d, want 1", len(payload.Items))
	}
	if payload.Items[0].Action != "PROXY" {
		t.Fatalf("action=%s, want PROXY", payload.Items[0].Action)
	}
	if payload.Items[0].UpstreamID != upID {
		t.Fatalf("upstream_id=%d, want %d", payload.Items[0].UpstreamID, upID)
	}
}

