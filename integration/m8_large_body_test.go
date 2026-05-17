package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Upload a known-size body and download a known-size body through the proxy,
// then assert that telemetry's bytes_in / bytes_out reflect the exact sizes.
// Validates the responseRecorder accounting in proxy/server.go.
func TestProxy_LargeBody_BytesAccountedExactly(t *testing.T) {
	t.Parallel()

	consoleURL, proxyURL, _, client := setupConsoleAndProxy(t)

	const downloadSize = 1024 * 1024 // 1 MiB downstream
	const uploadSize = 512 * 1024    // 512 KiB upstream
	downloadPayload := strings.Repeat("d", downloadSize)
	uploadPayload := strings.Repeat("u", uploadSize)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if len(body) != uploadSize {
			http.Error(w, "upload size mismatch", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(downloadSize))
		_, _ = w.Write([]byte(downloadPayload))
	}))
	t.Cleanup(target.Close)

	proxyParsed, _ := url.Parse(proxyURL)
	proxyClient := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyParsed)},
		Timeout:   10 * time.Second,
	}

	req, _ := http.NewRequest(http.MethodPost, target.URL+"/echo", strings.NewReader(uploadPayload))
	req.ContentLength = int64(uploadSize)
	resp, err := proxyClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	got, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if len(got) != downloadSize {
		t.Fatalf("downstream body size=%d, want=%d", len(got), downloadSize)
	}

	// Read /api/stats — telemetry counters must match the on-the-wire sizes.
	statsResp, err := client.Get(consoleURL + "/api/stats")
	if err != nil {
		t.Fatalf("GET stats: %v", err)
	}
	defer statsResp.Body.Close()
	var stats map[string]any
	if err := json.NewDecoder(statsResp.Body).Decode(&stats); err != nil {
		t.Fatalf("decode stats: %v", err)
	}

	bytesIn := int64(stats["bytes_in"].(float64))
	bytesOut := int64(stats["bytes_out"].(float64))
	if bytesIn != int64(uploadSize) {
		t.Fatalf("bytes_in=%d, want=%d", bytesIn, uploadSize)
	}
	if bytesOut != int64(downloadSize) {
		t.Fatalf("bytes_out=%d, want=%d", bytesOut, downloadSize)
	}
}

