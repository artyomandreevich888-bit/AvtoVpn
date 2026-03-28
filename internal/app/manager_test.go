package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	box "github.com/sagernet/sing-box"

	"github.com/mewmewmemw/autovpn/internal/config"
	"github.com/mewmewmemw/autovpn/internal/engine"
)

// vlessLine returns a minimal valid VLESS URI for testing.
func vlessLine(host string) string {
	return fmt.Sprintf("vless://test-uuid@%s:443?security=reality&type=tcp&fp=chrome&pbk=testkey&sid=aa#%s", host, host)
}

func newTestFetcher(url string) *config.Fetcher {
	return &config.Fetcher{
		URLs:     []string{url},
		CacheDir: "/tmp/autovpn-test-" + fmt.Sprint(time.Now().UnixNano()),
	}
}

func TestNewManager(t *testing.T) {
	m := NewManager(&config.Fetcher{CacheDir: "/tmp"})
	if m.Engine == nil {
		t.Fatal("engine is nil")
	}
	if m.ClashAPI == nil {
		t.Fatal("clashAPI is nil")
	}
	s := m.Status()
	if s.State != StateDisconnected {
		t.Errorf("initial state = %v, want disconnected", s.State)
	}
}

func TestOnChange(t *testing.T) {
	m := NewManager(&config.Fetcher{CacheDir: "/tmp"})

	var got Status
	var mu sync.Mutex
	m.OnChange(func(s Status) {
		mu.Lock()
		got = s
		mu.Unlock()
	})

	m.setStatus(Status{State: StateFetching})

	mu.Lock()
	if got.State != StateFetching {
		t.Errorf("callback state = %v, want fetching", got.State)
	}
	mu.Unlock()
}

func TestSetCancel(t *testing.T) {
	m := NewManager(&config.Fetcher{CacheDir: "/tmp"})

	called := false
	m.SetCancel(func() { called = true })
	m.Disconnect()

	if !called {
		t.Error("cancel not called on disconnect")
	}
}

func TestPrepareConfig(t *testing.T) {
	// Serve two VLESS configs.
	body := vlessLine("s1.example.com") + "\n" + vlessLine("s2.example.com")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	m := NewManager(newTestFetcher(srv.URL))
	m.SkipPreValidate = true
	configJSON, count, err := m.PrepareConfig(context.Background())
	if err != nil {
		t.Fatalf("PrepareConfig: %v", err)
	}
	if count != 2 {
		t.Errorf("server count = %d, want 2", count)
	}
	if len(configJSON) == 0 {
		t.Fatal("config JSON is empty")
	}

	// Verify it's valid sing-box JSON with outbounds.
	var cfg map[string]any
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	outbounds, ok := cfg["outbounds"].([]any)
	if !ok || len(outbounds) < 4 {
		t.Errorf("expected at least 4 outbounds, got %d", len(outbounds))
	}
}

func TestPrepareConfig_FetchError_EmbeddedFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	m := NewManager(newTestFetcher(srv.URL))
	// URL fails but embedded fallback has configs → PrepareConfig succeeds.
	configJSON, count, err := m.PrepareConfig(context.Background())
	if err != nil {
		t.Fatalf("expected embedded fallback to save us: %v", err)
	}
	if count == 0 {
		t.Error("expected configs from embedded fallback")
	}
	if len(configJSON) == 0 {
		t.Error("config JSON is empty")
	}
}

func TestPrepareConfig_StatusTransitions(t *testing.T) {
	body := vlessLine("s1.example.com")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	m := NewManager(newTestFetcher(srv.URL))

	var states []State
	var mu sync.Mutex
	m.OnChange(func(s Status) {
		mu.Lock()
		states = append(states, s.State)
		mu.Unlock()
	})

	_, _, _ = m.PrepareConfig(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if len(states) < 1 || states[0] != StateFetching {
		t.Errorf("first state should be fetching, got %v", states)
	}
}

func TestPrepareConfig_Cancelled(t *testing.T) {
	// Server blocks until context cancelled.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	m := NewManager(newTestFetcher(srv.URL))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, _, err := m.PrepareConfig(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestDisconnect_NotConnected(t *testing.T) {
	m := NewManager(&config.Fetcher{CacheDir: "/tmp"})
	err := m.Disconnect()
	if err != nil {
		t.Errorf("disconnect on idle should not error: %v", err)
	}
	if m.Status().State != StateDisconnected {
		t.Errorf("state = %v, want disconnected", m.Status().State)
	}
}

func TestConnect_AlreadyRunning(t *testing.T) {
	body := vlessLine("s1.example.com")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	m := NewManager(newTestFetcher(srv.URL))

	// Connect will fail at Engine.Start (needs real sing-box config + TUN),
	// but the PrepareConfig phase should succeed.
	_ = m.Connect()

	// Second connect while first is in progress/error should fail.
	// Engine.IsRunning() is false since Start failed, so this tests the flow,
	// not the guard. The guard is tested implicitly.
}

// --- CheckServices tests ---

func TestCheckServices_AllOK(t *testing.T) {
	// Mock service endpoints — all respond fast.
	svcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer svcSrv.Close()

	// Override service URLs for testing.
	origServices := services
	services = []ServiceDef{
		{"YouTube", svcSrv.URL + "/yt"},
		{"Instagram", svcSrv.URL + "/ig"},
		{"GitHub", svcSrv.URL + "/gh"},
	}
	defer func() { services = origServices }()

	m := NewManager(&config.Fetcher{CacheDir: "/tmp"})
	// ClashAPI won't be called because YouTube is OK.
	results := m.CheckServices(context.Background())
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	for _, r := range results {
		if r.Status != "ok" {
			t.Errorf("%s status = %v, want ok", r.Name, r.Status)
		}
	}
}

func TestCheckServices_YouTubeSlow_Rotates(t *testing.T) {
	callCount := 0
	// First call: YouTube slow. Second call: YouTube fast.
	svcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/yt") {
			callCount++
			if callCount <= 1 {
				// Simulate slow by returning 500 (easier than actual delay).
				w.WriteHeader(500)
				return
			}
		}
		w.WriteHeader(200)
	}))
	defer svcSrv.Close()

	origServices := services
	services = []ServiceDef{
		{"YouTube", svcSrv.URL + "/yt"},
	}
	defer func() { services = origServices }()

	// Mock ClashAPI that returns proxy list.
	clashSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(engine.ProxyStatus{
				Proxies: map[string]engine.ProxyInfo{
					"auto": {
						Now: "server-0",
						All: []string{"server-0", "server-1"},
					},
				},
			})
		} else {
			w.WriteHeader(204) // SelectProxy
		}
	}))
	defer clashSrv.Close()

	m := NewManager(&config.Fetcher{CacheDir: "/tmp"})
	m.ClashAPI = &engine.ClashAPIClient{BaseURL: clashSrv.URL}

	results := m.CheckServices(context.Background())
	// After rotation, YouTube should be OK.
	for _, r := range results {
		if r.Name == "YouTube" && r.Status != "ok" {
			t.Errorf("YouTube status after rotation = %v, want ok", r.Status)
		}
	}
	if callCount < 2 {
		t.Errorf("YouTube called %d times, want >=2 (rotation happened)", callCount)
	}
}

// --- Tests with mock engine ---

func mockEngine() *engine.Engine {
	return &engine.Engine{
		NewBox: func(opts box.Options) (engine.BoxInstance, error) {
			return &mockBox{}, nil
		},
	}
}

type mockBox struct{ closed bool }

func (m *mockBox) Start() error { return nil }
func (m *mockBox) Close() error { m.closed = true; return nil }

func TestConnect_FullLifecycle_MockEngine(t *testing.T) {
	body := vlessLine("s1.example.com")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	m := NewManager(newTestFetcher(srv.URL))
	m.Engine = mockEngine()
	m.SkipPreValidate = true

	err := m.Connect()
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !m.Engine.IsRunning() {
		t.Error("engine should be running")
	}
	if m.Status().State != StateConnected {
		t.Errorf("state = %v, want connected", m.Status().State)
	}
	if m.Status().TotalCount != 1 {
		t.Errorf("TotalCount = %d, want 1", m.Status().TotalCount)
	}

	err = m.Disconnect()
	if err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if m.Engine.IsRunning() {
		t.Error("engine should not be running after disconnect")
	}
	if m.Status().State != StateDisconnected {
		t.Errorf("state = %v, want disconnected", m.Status().State)
	}
}

func TestStartEngine_MockEngine(t *testing.T) {
	m := NewManager(&config.Fetcher{CacheDir: "/tmp"})
	m.Engine = mockEngine()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := m.StartEngine(ctx, []byte(`{"outbounds":[{"type":"direct","tag":"direct"}]}`), 5)
	if err != nil {
		t.Fatalf("StartEngine: %v", err)
	}
	if m.Status().State != StateConnected {
		t.Errorf("state = %v, want connected", m.Status().State)
	}
	if m.Status().TotalCount != 5 {
		t.Errorf("TotalCount = %d, want 5", m.Status().TotalCount)
	}

	m.Disconnect()
}

func TestConnect_StatusTransitions_MockEngine(t *testing.T) {
	body := vlessLine("s1.example.com")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	m := NewManager(newTestFetcher(srv.URL))
	m.Engine = mockEngine()

	var states []State
	var mu sync.Mutex
	m.OnChange(func(s Status) {
		mu.Lock()
		states = append(states, s.State)
		mu.Unlock()
	})

	m.Connect()
	m.Disconnect()

	mu.Lock()
	defer mu.Unlock()
	// Should have: fetching, starting/connected, disconnected
	if len(states) < 3 {
		t.Fatalf("states = %v, want at least 3 transitions", states)
	}
	if states[0] != StateFetching {
		t.Errorf("states[0] = %v, want fetching", states[0])
	}
	if states[len(states)-1] != StateDisconnected {
		t.Errorf("last state = %v, want disconnected", states[len(states)-1])
	}
}

func TestPollStatus_MockClashAPI(t *testing.T) {
	clashSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(engine.ProxyStatus{
			Proxies: map[string]engine.ProxyInfo{
				"proxy": {Now: "best-server", All: []string{"best-server"}},
				"best-server": {
					Name:    "best-server",
					History: []struct{ Delay int `json:"delay"` }{{99}},
				},
			},
		})
	}))
	defer clashSrv.Close()

	m := NewManager(&config.Fetcher{CacheDir: "/tmp"})
	m.ClashAPI = &engine.ClashAPIClient{BaseURL: clashSrv.URL}
	m.Engine = mockEngine()

	m.StartEngine(context.Background(), []byte(`{"outbounds":[{"type":"direct","tag":"direct"}]}`), 1)

	// pollStatus waits 2s then ticks every 5s. Wait enough for first tick.
	time.Sleep(8 * time.Second)

	s := m.Status()
	if s.Server != "best-server" {
		t.Errorf("server = %q, want best-server (pollStatus should have updated)", s.Server)
	}
	if s.Delay != 99 {
		t.Errorf("delay = %d, want 99", s.Delay)
	}

	m.Disconnect()
}

func TestConnect_AlreadyRunning_MockEngine(t *testing.T) {
	body := vlessLine("s1.example.com")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	m := NewManager(newTestFetcher(srv.URL))
	m.Engine = mockEngine()

	m.Connect()
	defer m.Disconnect()

	err := m.Connect()
	if err == nil {
		t.Fatal("second Connect should fail")
	}
}

func TestCheckServices_AllFail_NoClash(t *testing.T) {
	svcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer svcSrv.Close()

	origServices := services
	services = []ServiceDef{
		{"YouTube", svcSrv.URL + "/yt"},
	}
	defer func() { services = origServices }()

	m := NewManager(&config.Fetcher{CacheDir: "/tmp"})
	// ClashAPI unreachable → CheckServices returns fail without panic.
	m.ClashAPI = &engine.ClashAPIClient{BaseURL: "http://127.0.0.1:1"}

	results := m.CheckServices(context.Background())
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Status != "fail" {
		t.Errorf("YouTube status = %v, want fail", results[0].Status)
	}
}
