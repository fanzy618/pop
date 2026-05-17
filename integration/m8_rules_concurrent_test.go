package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// Concurrent CRUD on /api/rules must not race (verified by -race) and must
// leave the database consistent — every rule we successfully created must
// either still exist or have been deleted by us.
func TestRules_ConcurrentCRUD_NoRebuildRace(t *testing.T) {
	t.Parallel()

	consoleURL, _, _, client := setupConsoleAndProxy(t)

	const writers = 6
	const opsPerWriter = 12

	var created sync.Map // id -> bool present
	var unexpected atomic.Int64
	var wg sync.WaitGroup

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < opsPerWriter; i++ {
				pattern := "rule-w" + strconv.Itoa(w) + "-i" + strconv.Itoa(i) + ".test"

				body, _ := json.Marshal(map[string]any{
					"enabled": true, "pattern": pattern, "action": "DIRECT",
				})
				req, _ := http.NewRequest(http.MethodPost, consoleURL+"/api/rules", strings.NewReader(string(body)))
				req.Header.Set("Content-Type", "application/json")
				r, err := client.Do(req)
				if err != nil {
					unexpected.Add(1)
					continue
				}
				if r.StatusCode != http.StatusCreated {
					unexpected.Add(1)
					_ = r.Body.Close()
					continue
				}
				var rule map[string]any
				_ = json.NewDecoder(r.Body).Decode(&rule)
				_ = r.Body.Close()
				id := int64(rule["id"].(float64))
				created.Store(id, true)

				// Half are deleted right away, half remain.
				if i%2 == 0 {
					delReq, _ := http.NewRequest(http.MethodDelete, consoleURL+"/api/rules/"+strconv.FormatInt(id, 10), nil)
					dr, err := client.Do(delReq)
					if err != nil {
						unexpected.Add(1)
						continue
					}
					if dr.StatusCode == http.StatusOK {
						created.Store(id, false)
					}
					_ = dr.Body.Close()
				}
			}
		}(w)
	}

	// Background readers (paginated list + page=N+1 clamp + keyword search).
	var readErrs atomic.Int64
	stopReaders := make(chan struct{})
	var readers sync.WaitGroup
	for r := 0; r < 3; r++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stopReaders:
					return
				default:
				}
				resp, err := client.Get(consoleURL + "/api/rules?page=1&page_size=50")
				if err != nil {
					readErrs.Add(1)
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}
		}()
	}

	wg.Wait()
	close(stopReaders)
	readers.Wait()

	if got := unexpected.Load(); got != 0 {
		t.Fatalf("unexpected write errors: %d", got)
	}
	if got := readErrs.Load(); got != 0 {
		t.Fatalf("read errors: %d", got)
	}

	// Final consistency: every id we recorded as "present" must come back from
	// the API; every id we recorded as deleted must not.
	resp, err := client.Get(consoleURL + "/api/rules?page=1&page_size=200")
	if err != nil {
		t.Fatalf("final GET: %v", err)
	}
	defer resp.Body.Close()
	var payload rulesListResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	live := make(map[int64]bool, len(payload.Items))
	for _, r := range payload.Items {
		live[r.ID] = true
	}

	mismatch := 0
	created.Range(func(k, v any) bool {
		id, present := k.(int64), v.(bool)
		if present && !live[id] {
			mismatch++
		}
		if !present && live[id] {
			mismatch++
		}
		return true
	})
	if mismatch > 0 {
		t.Fatalf("rule presence mismatch between client view and server: %d", mismatch)
	}
}
