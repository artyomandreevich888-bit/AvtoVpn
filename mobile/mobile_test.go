package mobile

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mewmewmemw/autovpn/internal/app"
	"github.com/mewmewmemw/autovpn/internal/engine"
)

// vlessLine returns a valid VLESS+Reality URI for testing.
func vlessLine(host string) string {
	return fmt.Sprintf("vless://test-uuid@%s:443?security=reality&type=tcp&fp=chrome&pbk=testkey&sid=aa#%s", host, host)
}

func configServer() *httptest.Server {
	body := vlessLine("s1.example.com") + "\n" + vlessLine("s2.example.com")
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
}

func failServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
}

type mockStatusListener struct {
	mu     sync.Mutex
	states []int
}

func (m *mockStatusListener) OnStatusChanged(state int, server string, delayMs int, aliveCount int, totalCount int, errorMsg string) {
	m.mu.Lock()
	m.states = append(m.states, state)
	m.mu.Unlock()
}

type mockVPNService struct{}

func (m *mockVPNService) Protect(fd int32) bool { return true }

// reset clears global state between tests.
func reset() {
	// Cancel any in-flight context, stop engine if running.
	mu.Lock()
	if cancelFn != nil {
		cancelFn()
		cancelFn = nil
	}
	if mgr != nil && mgr.Engine.IsRunning() {
		mu.Unlock()
		mgr.Engine.Stop()
		mu.Lock()
	}
	mgr = nil
	lsnr = nil
	preparedConfig = nil
	preparedServerCount = 0
	preparedSource = ""
	preparedCacheAge = 0
	prepared = false
	mu.Unlock()
}

// --- Prepare tests ---

func TestPrepare_Success(t *testing.T) {
	reset()
	srv := configServer()
	defer srv.Close()

	// Override ConfigSources for test.
	origSources := overrideConfigSources(srv.URL)
	defer restoreConfigSources(origSources)

	listener := &mockStatusListener{}
	data, err := Prepare(t.TempDir(), listener)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("Prepare returned empty config")
	}

	// Should have received fetching state.
	listener.mu.Lock()
	hasStates := len(listener.states) > 0
	listener.mu.Unlock()
	if !hasStates {
		t.Error("no status callbacks received")
	}
}

func TestPrepare_NoDoubleMutexUnlock(t *testing.T) {
	reset()
	srv := configServer()
	defer srv.Close()

	origSources := overrideConfigSources(srv.URL)
	defer restoreConfigSources(origSources)

	// This would panic with "sync: unlock of unlocked mutex" if the bug exists.
	// The test passing means the mutex logic is correct.
	_, err := Prepare(t.TempDir(), &mockStatusListener{})
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
}

func TestPrepare_FetchError_NoDoubleMutexUnlock(t *testing.T) {
	reset()
	// Use a server that blocks until cancelled to test cancellation path.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	origSources := overrideConfigSources(srv.URL)
	defer restoreConfigSources(origSources)

	// This should not panic even on error path.
	// With embedded fallback configs, this may succeed.
	_, _ = Prepare(t.TempDir(), &mockStatusListener{})
}

func TestPrepare_AlreadyRunning(t *testing.T) {
	reset()
	srv := configServer()
	defer srv.Close()

	origSources := overrideConfigSources(srv.URL)
	defer restoreConfigSources(origSources)

	_, err := Prepare(t.TempDir(), &mockStatusListener{})
	if err != nil {
		t.Fatalf("first Prepare failed: %v", err)
	}

	// Second Prepare should work (not running, just prepared).
	_, err = Prepare(t.TempDir(), &mockStatusListener{})
	if err != nil {
		t.Fatalf("second Prepare failed: %v", err)
	}
}

// --- Start tests ---

func TestStart_WithoutPrepare(t *testing.T) {
	reset()
	err := Start(0, nil, "wlan0", 1, &mockVPNService{}, &mockStatusListener{})
	if err == nil {
		t.Fatal("Start without Prepare should fail")
	}
	if !strings.Contains(err.Error(), "Prepare") {
		t.Errorf("error should mention Prepare, got: %v", err)
	}
}

func TestStart_NoNilPointerPanic(t *testing.T) {
	if testing.Short() {
		t.Skip("requires TUN privileges")
	}
	reset()
	srv := configServer()
	defer srv.Close()

	origSources := overrideConfigSources(srv.URL)
	defer restoreConfigSources(origSources)

	_, err := Prepare(t.TempDir(), &mockStatusListener{})
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- Start(-1, nil, "lo", 1, &mockVPNService{}, &mockStatusListener{})
	}()

	select {
	case err = <-done:
		_ = err
	case <-time.After(5 * time.Second):
	}

	Stop()
}

// --- Stop tests ---

func TestStop_WhenNotRunning(t *testing.T) {
	reset()
	err := Stop()
	if err != nil {
		t.Errorf("Stop on idle should not error: %v", err)
	}
}

func TestStop_AfterPrepare(t *testing.T) {
	reset()
	srv := configServer()
	defer srv.Close()

	origSources := overrideConfigSources(srv.URL)
	defer restoreConfigSources(origSources)

	_, err := Prepare(t.TempDir(), &mockStatusListener{})
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	err = Stop()
	if err != nil {
		t.Errorf("Stop after Prepare should not error: %v", err)
	}
}

func TestStop_MultipleTimesNoPanic(t *testing.T) {
	reset()
	Stop()
	Stop()
	Stop()
	// No panic = pass.
}

// --- IsRunning tests ---

func TestIsRunning_Default(t *testing.T) {
	reset()
	if IsRunning() {
		t.Error("should not be running initially")
	}
}

// --- GetConfigInfo tests ---

func TestGetConfigInfo_AfterPrepare(t *testing.T) {
	reset()
	srv := configServer()
	defer srv.Close()

	origSources := overrideConfigSources(srv.URL)
	defer restoreConfigSources(origSources)

	_, err := Prepare(t.TempDir(), &mockStatusListener{})
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	info := GetConfigInfo()
	parts := strings.Split(info, ",")
	if len(parts) != 3 {
		t.Fatalf("GetConfigInfo = %q, want 3 parts", info)
	}
	if parts[0] != "network" && parts[0] != "embedded" {
		t.Errorf("source = %q, want network or embedded", parts[0])
	}
}

// --- Concurrent access tests ---

func TestConcurrent_PrepareAndStop(t *testing.T) {
	reset()
	srv := configServer()
	defer srv.Close()

	origSources := overrideConfigSources(srv.URL)
	defer restoreConfigSources(origSources)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			Prepare(t.TempDir(), &mockStatusListener{})
		}()
		go func() {
			defer wg.Done()
			time.Sleep(time.Millisecond)
			Stop()
		}()
	}
	wg.Wait()
	// No deadlock, no panic = pass.
}

func TestConcurrent_MultipleStops(t *testing.T) {
	reset()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Stop()
		}()
	}
	wg.Wait()
}

// --- Listener tests ---

func TestNotify_NilListener(t *testing.T) {
	reset()
	// Should not panic with nil listener.
	notify(StateConnected, "test", 42, 5, 10, "")
}

func TestNotify_ConcurrentSafe(t *testing.T) {
	reset()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			notify(StateConnected, "test", 42, 5, 10, "")
		}()
		go func() {
			defer wg.Done()
			mu.Lock()
			setListener(&mockStatusListener{})
			mu.Unlock()
		}()
	}
	wg.Wait()
}

// --- CheckServices / GetTraffic / GetServerList when not connected ---

func TestCheckServices_NotConnected(t *testing.T) {
	reset()
	result := CheckServices()
	if result != "not connected" {
		t.Errorf("CheckServices = %q, want 'not connected'", result)
	}
}

func TestGetTraffic_NotConnected(t *testing.T) {
	reset()
	result := GetTraffic()
	if result != "0,0" {
		t.Errorf("GetTraffic = %q, want '0,0'", result)
	}
}

func TestGetServerList_NotConnected(t *testing.T) {
	reset()
	result := GetServerList()
	if result != "" {
		t.Errorf("GetServerList = %q, want empty", result)
	}
}

// --- mobile functions with mock ClashAPI ---

func clashWithServers() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/proxies" {
			json.NewEncoder(w).Encode(map[string]any{
				"proxies": map[string]any{
					"proxy":     map[string]any{"now": "srv-0", "all": []string{"srv-0", "srv-1"}},
					"srv-0":    map[string]any{"name": "srv-0", "history": []any{map[string]int{"delay": 50}}},
					"srv-1":    map[string]any{"name": "srv-1", "history": []any{map[string]int{"delay": 0}}},
				},
			})
			return
		}
		if r.URL.Path == "/traffic" {
			json.NewEncoder(w).Encode(map[string]int64{"up": 512, "down": 2048})
			return
		}
		if strings.Contains(r.URL.Path, "/delay") {
			if strings.Contains(r.URL.Path, "srv-1") {
				w.WriteHeader(408)
				return
			}
			json.NewEncoder(w).Encode(map[string]int{"delay": 50})
			return
		}
		w.WriteHeader(204)
	}))
}

func setupMockMgr(clashURL string) {
	mu.Lock()
	mgr = &app.Manager{
		ClashAPI: &engine.ClashAPIClient{BaseURL: clashURL},
		Engine:   &engine.Engine{},
	}
	mu.Unlock()
}

func TestCheckServices_Connected_MockClash(t *testing.T) {
	svcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer svcSrv.Close()

	clashSrv := clashWithServers()
	defer clashSrv.Close()

	reset()
	setupMockMgr(clashSrv.URL)

	// Override service URLs
	origServices := app.ExportServices()
	app.SetServices([]app.ServiceDef{{Name: "YouTube", URL: svcSrv.URL}})
	defer app.SetServices(origServices)

	result := CheckServices()
	if result != "" {
		t.Errorf("CheckServices = %q, want empty (all ok)", result)
	}
}

func TestGetServerList_Connected_MockClash(t *testing.T) {
	clashSrv := clashWithServers()
	defer clashSrv.Close()

	reset()
	setupMockMgr(clashSrv.URL)

	result := GetServerList()
	if result == "" {
		t.Fatal("GetServerList should return data")
	}
	lines := strings.Split(strings.TrimSpace(result), "\n")
	if len(lines) != 2 {
		t.Errorf("got %d lines, want 2 servers", len(lines))
	}
	// srv-0 should be alive, srv-1 dead
	for _, line := range lines {
		parts := strings.Split(line, ",")
		if parts[0] == "srv-0" && parts[2] != "1" {
			t.Errorf("srv-0 should be alive")
		}
		if parts[0] == "srv-1" && parts[2] != "0" {
			t.Errorf("srv-1 should be dead")
		}
	}
}

func TestGetTraffic_Connected_MockClash(t *testing.T) {
	clashSrv := clashWithServers()
	defer clashSrv.Close()

	reset()
	setupMockMgr(clashSrv.URL)

	result := GetTraffic()
	if result == "0,0" {
		t.Error("GetTraffic should return real values")
	}
	parts := strings.Split(result, ",")
	if parts[0] != "512" || parts[1] != "2048" {
		t.Errorf("traffic = %q, want 512,2048", result)
	}
}

func TestGetExternalIP_Returns(t *testing.T) {
	// Just verify it doesn't panic. May return empty if no network.
	_ = GetExternalIP()
}

// --- ValidateServers / VerifyConnection tests ---

func TestValidateServers_NotConnected(t *testing.T) {
	reset()
	result := ValidateServers(10)
	if !strings.HasPrefix(result, "0,0,0,,") {
		t.Errorf("ValidateServers when not connected = %q, want 0,0,0,,", result)
	}
}

func TestValidateServers_WithMockClashAPI(t *testing.T) {
	// Mock Clash API that returns proxy list with delay test endpoint.
	clashSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/proxies" {
			json.NewEncoder(w).Encode(map[string]any{
				"proxies": map[string]any{
					"proxy": map[string]any{
						"now": "server-0",
						"all": []string{"server-0", "server-1", "server-2"},
					},
					"server-0": map[string]any{"name": "server-0", "history": []any{}},
					"server-1": map[string]any{"name": "server-1", "history": []any{}},
					"server-2": map[string]any{"name": "server-2", "history": []any{}},
				},
			})
			return
		}
		// Proxy delay test: server-0 alive, server-1 alive, server-2 dead
		if strings.Contains(r.URL.Path, "/delay") {
			if strings.Contains(r.URL.Path, "server-2") {
				w.WriteHeader(408) // timeout
				return
			}
			delay := 100
			if strings.Contains(r.URL.Path, "server-1") {
				delay = 200
			}
			json.NewEncoder(w).Encode(map[string]int{"delay": delay})
			return
		}
		// SelectProxy
		if r.Method == "PUT" {
			w.WriteHeader(204)
			return
		}
		w.WriteHeader(404)
	}))
	defer clashSrv.Close()

	reset()
	// Set up mgr with mock ClashAPI
	mu.Lock()
	mgr = &app.Manager{
		ClashAPI: &engine.ClashAPIClient{BaseURL: clashSrv.URL},
		Engine:   &engine.Engine{},
	}
	mu.Unlock()

	result := ValidateServers(10)
	lines := strings.Split(strings.TrimSpace(result), "\n")

	// First line: "alive,dead,total,bestServer,bestDelay"
	header := lines[0]
	parts := strings.Split(header, ",")
	if len(parts) != 5 {
		t.Fatalf("header = %q, want 5 parts", header)
	}
	if parts[0] != "2" { // 2 alive
		t.Errorf("alive = %s, want 2", parts[0])
	}
	if parts[1] != "1" { // 1 dead
		t.Errorf("dead = %s, want 1", parts[1])
	}
	if parts[2] != "3" { // 3 total
		t.Errorf("total = %s, want 3", parts[2])
	}
	if parts[3] != "server-0" { // best = lowest delay
		t.Errorf("best = %s, want server-0", parts[3])
	}

	// Should have per-server detail lines
	if len(lines) != 4 { // header + 3 servers
		t.Errorf("got %d lines, want 4", len(lines))
	}
}

func TestValidateServers_AllDead(t *testing.T) {
	clashSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/proxies" {
			json.NewEncoder(w).Encode(map[string]any{
				"proxies": map[string]any{
					"proxy": map[string]any{
						"now": "server-0",
						"all": []string{"server-0", "server-1"},
					},
					"server-0": map[string]any{"name": "server-0"},
					"server-1": map[string]any{"name": "server-1"},
				},
			})
			return
		}
		// All proxies timeout
		w.WriteHeader(408)
	}))
	defer clashSrv.Close()

	reset()
	mu.Lock()
	mgr = &app.Manager{
		ClashAPI: &engine.ClashAPIClient{BaseURL: clashSrv.URL},
		Engine:   &engine.Engine{},
	}
	mu.Unlock()

	result := ValidateServers(10)
	parts := strings.Split(strings.Split(result, "\n")[0], ",")
	if parts[0] != "0" {
		t.Errorf("alive = %s, want 0 (all dead)", parts[0])
	}
	if parts[2] != "2" {
		t.Errorf("total = %s, want 2", parts[2])
	}
}

func TestVerifyConnection_NotConnected(t *testing.T) {
	reset()
	result := VerifyConnection(10)
	if result != "" {
		t.Errorf("VerifyConnection when not connected = %q, want empty", result)
	}
}

func TestVerifyConnection_WithMockServers(t *testing.T) {
	// Mock IP API
	ipSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "1.2.3.4")
	}))
	defer ipSrv.Close()

	// Mock Clash API
	clashSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/proxies" {
			json.NewEncoder(w).Encode(map[string]any{
				"proxies": map[string]any{
					"proxy": map[string]any{
						"now": "server-0",
						"all": []string{"server-0"},
					},
					"server-0": map[string]any{"name": "server-0"},
				},
			})
			return
		}
		if strings.Contains(r.URL.Path, "/delay") {
			json.NewEncoder(w).Encode(map[string]int{"delay": 42})
			return
		}
		w.WriteHeader(204) // SelectProxy
	}))
	defer clashSrv.Close()

	reset()
	mu.Lock()
	mgr = &app.Manager{
		ClashAPI: &engine.ClashAPIClient{BaseURL: clashSrv.URL},
		Engine:   &engine.Engine{},
	}
	mu.Unlock()

	// Can't fully test VerifyConnection because it calls the real ipify API.
	// But ValidateServers flow is fully mocked above.
	// Test that it doesn't panic and returns something reasonable.
	result := ValidateServers(5)
	if !strings.Contains(result, "server-0") {
		t.Errorf("ValidateServers should contain server-0, got: %q", result)
	}
}

func TestValidateServers_Concurrency(t *testing.T) {
	// Test that concurrent validation doesn't race/deadlock.
	var reqCount int64
	clashSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/proxies" {
			names := make([]string, 50)
			proxies := map[string]any{}
			for i := 0; i < 50; i++ {
				n := fmt.Sprintf("srv-%d", i)
				names[i] = n
				proxies[n] = map[string]any{"name": n}
			}
			proxies["proxy"] = map[string]any{"now": "srv-0", "all": names}
			json.NewEncoder(w).Encode(map[string]any{"proxies": proxies})
			return
		}
		if strings.Contains(r.URL.Path, "/delay") {
			atomic.AddInt64(&reqCount, 1)
			time.Sleep(50 * time.Millisecond) // simulate latency
			json.NewEncoder(w).Encode(map[string]int{"delay": 50})
			return
		}
		w.WriteHeader(204)
	}))
	defer clashSrv.Close()

	reset()
	mu.Lock()
	mgr = &app.Manager{
		ClashAPI: &engine.ClashAPIClient{BaseURL: clashSrv.URL},
		Engine:   &engine.Engine{},
	}
	mu.Unlock()

	start := time.Now()
	result := ValidateServers(20) // 20 concurrent
	elapsed := time.Since(start)

	lines := strings.Split(strings.TrimSpace(result), "\n")
	header := strings.Split(lines[0], ",")
	if header[0] != "50" {
		t.Errorf("alive = %s, want 50", header[0])
	}
	if header[2] != "50" {
		t.Errorf("total = %s, want 50", header[2])
	}

	// With 50 servers and 20 concurrency, 50ms each:
	// should take ~150ms (3 batches), not 2500ms (sequential).
	if elapsed > 2*time.Second {
		t.Errorf("took %v, too slow — concurrency not working?", elapsed)
	}
	if atomic.LoadInt64(&reqCount) != 50 {
		t.Errorf("requests = %d, want 50", reqCount)
	}
}

// --- E2E: VerifyConnection progress states ---

// detailedStatusListener records full state+server pairs for assertions.
type detailedStatusListener struct {
	mu      sync.Mutex
	entries []statusEntry
}

type statusEntry struct {
	state  int
	server string
	alive  int
	total  int
}

func (d *detailedStatusListener) OnStatusChanged(state int, server string, delayMs int, aliveCount int, totalCount int, errorMsg string) {
	d.mu.Lock()
	d.entries = append(d.entries, statusEntry{state: state, server: server, alive: aliveCount, total: totalCount})
	d.mu.Unlock()
}

func (d *detailedStatusListener) snapshot() []statusEntry {
	d.mu.Lock()
	cp := make([]statusEntry, len(d.entries))
	copy(cp, d.entries)
	d.mu.Unlock()
	return cp
}

// TestVerifyConnection_ProgressStates verifies that notify() is called with
// StateConnected (3) during validation progress, NOT StateStarting (2).
// This was a bug: pollStatus sent StateStarting for "Retrying IP check".
func TestVerifyConnection_ProgressStates(t *testing.T) {
	// Mock IP API — returns external IP.
	ipSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "203.0.113.42")
	}))
	defer ipSrv.Close()
	origIPURL := ipCheckURL
	ipCheckURL = ipSrv.URL
	defer func() { ipCheckURL = origIPURL }()

	// Mock Clash API with two servers; server-0 fast, server-1 faster.
	clashSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/proxies" {
			json.NewEncoder(w).Encode(map[string]any{
				"proxies": map[string]any{
					"proxy": map[string]any{
						"now": "server-0",
						"all": []string{"server-0", "server-1"},
					},
					"server-0": map[string]any{"name": "server-0"},
					"server-1": map[string]any{"name": "server-1"},
				},
			})
			return
		}
		if strings.Contains(r.URL.Path, "/delay") {
			delay := 100
			if strings.Contains(r.URL.Path, "server-1") {
				delay = 50
			}
			json.NewEncoder(w).Encode(map[string]int{"delay": delay})
			return
		}
		w.WriteHeader(204) // SelectProxy PUT
	}))
	defer clashSrv.Close()

	reset()
	listener := &detailedStatusListener{}
	mu.Lock()
	setListener(listener)
	mgr = &app.Manager{
		ClashAPI: &engine.ClashAPIClient{BaseURL: clashSrv.URL},
		Engine:   &engine.Engine{},
	}
	mu.Unlock()

	result := VerifyConnection(10)
	if result == "" {
		t.Fatal("VerifyConnection returned empty, expected ip,server,delay")
	}

	entries := listener.snapshot()
	if len(entries) == 0 {
		t.Fatal("no status callbacks received")
	}

	// KEY ASSERTION: every callback during verification must use StateConnected (3),
	// never StateStarting (2). The old bug sent StateStarting for "Retrying IP check".
	for i, e := range entries {
		if e.state == StateStarting {
			t.Errorf("entry[%d] state=%d (StateStarting), want StateConnected(3); server=%q", i, e.state, e.server)
		}
		if e.state != StateConnected {
			t.Errorf("entry[%d] unexpected state=%d, want StateConnected(3); server=%q", i, e.state, e.server)
		}
	}

	// Verify progress callbacks had correct alive/total counts.
	hasProgress := false
	for _, e := range entries {
		if e.total > 0 {
			hasProgress = true
			if e.total != 2 {
				t.Errorf("progress total=%d, want 2", e.total)
			}
		}
	}
	if !hasProgress {
		t.Error("no progress callbacks with total>0 received during validation")
	}
}

// TestPrepare_ProgressCallbacks verifies OnProgress callback fires with
// source count during fetch.
func TestPrepare_ProgressCallbacks(t *testing.T) {
	// Set up two config sources.
	body := vlessLine("s1.example.com") + "\n" + vlessLine("s2.example.com")
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer srv1.Close()
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer srv2.Close()

	reset()
	origSources := overrideConfigSources(srv1.URL)
	// Use two sources to verify progress reports total correctly.
	setConfigSources([]string{srv1.URL, srv2.URL})
	defer restoreConfigSources(origSources)

	listener := &detailedStatusListener{}
	_, err := Prepare(t.TempDir(), listener)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	entries := listener.snapshot()
	if len(entries) == 0 {
		t.Fatal("no status callbacks received during Prepare")
	}

	// All callbacks during fetch should be StateFetching (1).
	for i, e := range entries {
		if e.state != StateFetching {
			t.Errorf("entry[%d] state=%d, want StateFetching(1)", i, e.state)
		}
	}

	// Server string should contain "Source X/2" indicating total sources.
	foundSourceProgress := false
	for _, e := range entries {
		if strings.Contains(e.server, "/2") {
			foundSourceProgress = true
		}
	}
	if !foundSourceProgress {
		t.Errorf("no progress callback mentioned total sources; entries=%v", entries)
	}
}

// TestVerifyConnection_SwitchesToBestServer verifies that VerifyConnection
// picks the server with the lowest delay and calls SelectProxy with it.
func TestVerifyConnection_SwitchesToBestServer(t *testing.T) {
	// Mock IP API.
	ipSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "198.51.100.1")
	}))
	defer ipSrv.Close()
	origIPURL := ipCheckURL
	ipCheckURL = ipSrv.URL
	defer func() { ipCheckURL = origIPURL }()

	// Track which server was selected.
	var selectedMu sync.Mutex
	var selectedServer string

	// server-0 = 200ms, server-1 = 30ms => should pick server-1.
	clashSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/proxies" {
			json.NewEncoder(w).Encode(map[string]any{
				"proxies": map[string]any{
					"proxy": map[string]any{
						"now": "server-0",
						"all": []string{"server-0", "server-1"},
					},
					"server-0": map[string]any{"name": "server-0"},
					"server-1": map[string]any{"name": "server-1"},
				},
			})
			return
		}
		if r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/proxies/") {
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			selectedMu.Lock()
			selectedServer = body["name"]
			selectedMu.Unlock()
			w.WriteHeader(204)
			return
		}
		if strings.Contains(r.URL.Path, "/delay") {
			delay := 200
			if strings.Contains(r.URL.Path, "server-1") {
				delay = 30
			}
			json.NewEncoder(w).Encode(map[string]int{"delay": delay})
			return
		}
		w.WriteHeader(204)
	}))
	defer clashSrv.Close()

	reset()
	setupMockMgr(clashSrv.URL)

	result := VerifyConnection(10)
	if result == "" {
		t.Fatal("VerifyConnection returned empty")
	}

	// Must contain server-1 (lower delay).
	if !strings.Contains(result, "server-1") {
		t.Errorf("result = %q, want to contain server-1 (best delay)", result)
	}

	selectedMu.Lock()
	sel := selectedServer
	selectedMu.Unlock()
	if sel != "server-1" {
		t.Errorf("SelectProxy called with %q, want server-1", sel)
	}
}

// TestFullFlow_PrepareStartVerify tests the full mobile flow:
// Prepare (fetching) -> Connected -> validation progress -> final connected.
// Since Start requires TUN, we test Prepare + VerifyConnection states.
func TestFullFlow_PrepareStartVerify(t *testing.T) {
	// Config server.
	body := vlessLine("s1.example.com") + "\n" + vlessLine("s2.example.com")
	configSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer configSrv.Close()

	// IP server.
	ipSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "10.0.0.1")
	}))
	defer ipSrv.Close()
	origIPURL := ipCheckURL
	ipCheckURL = ipSrv.URL
	defer func() { ipCheckURL = origIPURL }()

	// Clash API.
	clashSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/proxies" {
			json.NewEncoder(w).Encode(map[string]any{
				"proxies": map[string]any{
					"proxy": map[string]any{
						"now": "srv-0",
						"all": []string{"srv-0"},
					},
					"srv-0": map[string]any{"name": "srv-0"},
				},
			})
			return
		}
		if strings.Contains(r.URL.Path, "/delay") {
			json.NewEncoder(w).Encode(map[string]int{"delay": 55})
			return
		}
		w.WriteHeader(204)
	}))
	defer clashSrv.Close()

	reset()
	origSources := overrideConfigSources(configSrv.URL)
	defer restoreConfigSources(origSources)

	listener := &detailedStatusListener{}

	// Phase 1: Prepare — should get StateFetching callbacks.
	_, err := Prepare(t.TempDir(), listener)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	entriesAfterPrepare := listener.snapshot()
	hasFetching := false
	for _, e := range entriesAfterPrepare {
		if e.state == StateFetching {
			hasFetching = true
		}
	}
	if !hasFetching {
		t.Error("Prepare phase should have produced StateFetching callbacks")
	}

	// Phase 2: Simulate post-Start by setting up mock mgr with ClashAPI.
	mu.Lock()
	mgr = &app.Manager{
		ClashAPI: &engine.ClashAPIClient{BaseURL: clashSrv.URL},
		Engine:   &engine.Engine{},
	}
	setListener(listener)
	mu.Unlock()

	// Phase 3: VerifyConnection — should get StateConnected callbacks.
	result := VerifyConnection(10)
	if result == "" {
		t.Fatal("VerifyConnection returned empty")
	}

	allEntries := listener.snapshot()
	// Find entries after prepare phase (StateConnected entries from verify).
	hasConnected := false
	for _, e := range allEntries {
		if e.state == StateConnected {
			hasConnected = true
		}
	}
	if !hasConnected {
		t.Error("VerifyConnection phase should have produced StateConnected callbacks")
	}

	// Verify state transition order: first StateFetching, then StateConnected.
	lastFetchingIdx := -1
	firstConnectedIdx := -1
	for i, e := range allEntries {
		if e.state == StateFetching {
			lastFetchingIdx = i
		}
		if e.state == StateConnected && firstConnectedIdx == -1 {
			firstConnectedIdx = i
		}
	}
	if lastFetchingIdx >= 0 && firstConnectedIdx >= 0 && lastFetchingIdx >= firstConnectedIdx {
		t.Errorf("StateFetching (idx %d) appeared after StateConnected (idx %d) — wrong order", lastFetchingIdx, firstConnectedIdx)
	}

	// Result should be "ip,server,delay".
	parts := strings.Split(result, ",")
	if len(parts) != 3 {
		t.Fatalf("result = %q, want 3 comma-separated parts", result)
	}
	if parts[0] != "10.0.0.1" {
		t.Errorf("ip = %q, want 10.0.0.1", parts[0])
	}
}

// --- Helpers ---

func overrideConfigSources(url string) []string {
	origSources := configSourcesSnapshot()
	setConfigSources([]string{url})
	return origSources
}

func restoreConfigSources(orig []string) {
	setConfigSources(orig)
}
