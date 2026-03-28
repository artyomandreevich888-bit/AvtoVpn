package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const proxiesJSON = `{
  "proxies": {
    "proxy": {
      "name": "proxy",
      "type": "Selector",
      "now": "server-0",
      "all": ["server-0", "server-1"]
    },
    "server-0": {
      "name": "server-0",
      "type": "VLESS",
      "history": [{"delay": 42}]
    },
    "server-1": {
      "name": "server-1",
      "type": "VLESS",
      "history": [{"delay": 0}]
    }
  }
}`

func TestGetProxies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxies" {
			w.WriteHeader(404)
			return
		}
		w.Write([]byte(proxiesJSON))
	}))
	defer srv.Close()

	c := &ClashAPIClient{BaseURL: srv.URL, Client: srv.Client()}
	ps, err := c.GetProxies(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(ps.Proxies) != 3 {
		t.Errorf("got %d proxies, want 3", len(ps.Proxies))
	}

	proxy := ps.Proxies["proxy"]
	if proxy.Now != "server-0" {
		t.Errorf("proxy.Now = %q, want server-0", proxy.Now)
	}
}

func TestGetStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(proxiesJSON))
	}))
	defer srv.Close()

	c := &ClashAPIClient{BaseURL: srv.URL, Client: srv.Client()}
	status, err := c.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status.CurrentServer != "server-0" {
		t.Errorf("CurrentServer = %q", status.CurrentServer)
	}
	if status.CurrentDelay != 42 {
		t.Errorf("CurrentDelay = %d, want 42", status.CurrentDelay)
	}
	if status.AliveCount != 1 {
		t.Errorf("AliveCount = %d, want 1 (server-1 has delay=0)", status.AliveCount)
	}
	if status.TotalCount != 2 {
		t.Errorf("TotalCount = %d, want 2", status.TotalCount)
	}
}

func TestGetStatus_ServerDetails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(proxiesJSON))
	}))
	defer srv.Close()

	c := &ClashAPIClient{BaseURL: srv.URL, Client: srv.Client()}
	status, err := c.GetStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Servers) != 2 {
		t.Fatalf("Servers = %d, want 2", len(status.Servers))
	}
	for _, s := range status.Servers {
		switch s.Name {
		case "server-0":
			if !s.Alive || !s.Active || s.Delay != 42 {
				t.Errorf("server-0: alive=%v active=%v delay=%d", s.Alive, s.Active, s.Delay)
			}
		case "server-1":
			if s.Alive || s.Active {
				t.Errorf("server-1: alive=%v active=%v (should be dead/inactive)", s.Alive, s.Active)
			}
		}
	}
}

func TestSelectProxy(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := &ClashAPIClient{BaseURL: srv.URL, Secret: "test-secret", Client: srv.Client()}
	err := c.SelectProxy(context.Background(), "proxy", "server-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotPath != "/proxies/proxy" {
		t.Errorf("path = %q, want /proxies/proxy", gotPath)
	}
	if gotBody == "" {
		t.Error("empty body")
	}
}

func TestSelectProxy_WithAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := &ClashAPIClient{BaseURL: srv.URL, Secret: "my-secret", Client: srv.Client()}
	c.SelectProxy(context.Background(), "proxy", "server-0")

	if gotAuth != "Bearer my-secret" {
		t.Errorf("Authorization = %q, want 'Bearer my-secret'", gotAuth)
	}
}

func TestGetProxies_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	c := &ClashAPIClient{BaseURL: srv.URL, Client: srv.Client()}
	_, err := c.GetProxies(context.Background())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

// --- TestProxyDelay ---

func TestTestProxyDelay_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]int{"delay": 123})
	}))
	defer srv.Close()

	c := &ClashAPIClient{BaseURL: srv.URL}
	delay, err := c.TestProxyDelay(context.Background(), "server-0", "https://test.com", 5000)
	if err != nil {
		t.Fatal(err)
	}
	if delay != 123 {
		t.Errorf("delay = %d, want 123", delay)
	}
}

func TestTestProxyDelay_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(408)
	}))
	defer srv.Close()

	c := &ClashAPIClient{BaseURL: srv.URL}
	_, err := c.TestProxyDelay(context.Background(), "server-0", "https://test.com", 5000)
	if err == nil {
		t.Fatal("expected error for timeout")
	}
}

func TestTestProxyDelay_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := &ClashAPIClient{BaseURL: srv.URL}
	_, err := c.TestProxyDelay(ctx, "server-0", "https://test.com", 5000)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

// --- ValidateAllProxies ---

func TestValidateAllProxies_MixedResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/proxies" {
			json.NewEncoder(w).Encode(ProxyStatus{
				Proxies: map[string]ProxyInfo{
					"proxy": {Now: "s0", All: []string{"s0", "s1", "s2"}},
					"s0":   {Name: "s0"}, "s1": {Name: "s1"}, "s2": {Name: "s2"},
				},
			})
			return
		}
		switch r.URL.Path {
		case "/proxies/s0/delay":
			json.NewEncoder(w).Encode(map[string]int{"delay": 50})
		case "/proxies/s1/delay":
			json.NewEncoder(w).Encode(map[string]int{"delay": 500})
		default:
			w.WriteHeader(408)
		}
	}))
	defer srv.Close()

	c := &ClashAPIClient{BaseURL: srv.URL}
	results, err := c.ValidateAllProxies(context.Background(), 10, "https://test.com", 5000)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}

	alive := 0
	for _, r := range results {
		if r.Alive {
			alive++
		}
	}
	if alive != 2 {
		t.Errorf("alive = %d, want 2", alive)
	}
}

func TestValidateAllProxies_ConcurrencyLimit(t *testing.T) {
	var concurrent, maxConcurrent int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/proxies" {
			names := make([]string, 30)
			proxies := map[string]ProxyInfo{}
			for i := range 30 {
				n := fmt.Sprintf("s%d", i)
				names[i] = n
				proxies[n] = ProxyInfo{Name: n}
			}
			proxies["proxy"] = ProxyInfo{Now: "s0", All: names}
			json.NewEncoder(w).Encode(ProxyStatus{Proxies: proxies})
			return
		}
		cur := atomic.AddInt64(&concurrent, 1)
		for {
			old := atomic.LoadInt64(&maxConcurrent)
			if cur <= old || atomic.CompareAndSwapInt64(&maxConcurrent, old, cur) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		atomic.AddInt64(&concurrent, -1)
		json.NewEncoder(w).Encode(map[string]int{"delay": 100})
	}))
	defer srv.Close()

	c := &ClashAPIClient{BaseURL: srv.URL}
	results, err := c.ValidateAllProxies(context.Background(), 5, "https://test.com", 5000)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 30 {
		t.Errorf("results = %d, want 30", len(results))
	}
	if atomic.LoadInt64(&maxConcurrent) > 5 {
		t.Errorf("max concurrent = %d, want <=5", maxConcurrent)
	}
	if atomic.LoadInt64(&maxConcurrent) < 2 {
		t.Errorf("max concurrent = %d, want >=2", maxConcurrent)
	}
}

func TestValidateAllProxies_AllDead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/proxies" {
			json.NewEncoder(w).Encode(ProxyStatus{
				Proxies: map[string]ProxyInfo{
					"proxy": {Now: "s0", All: []string{"s0", "s1"}},
					"s0":   {Name: "s0"}, "s1": {Name: "s1"},
				},
			})
			return
		}
		w.WriteHeader(408)
	}))
	defer srv.Close()

	c := &ClashAPIClient{BaseURL: srv.URL}
	results, err := c.ValidateAllProxies(context.Background(), 5, "https://test.com", 5000)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Alive {
			t.Errorf("%s should be dead", r.Name)
		}
	}
}

func TestValidateAllProxies_NoProxyGroup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ProxyStatus{
			Proxies: map[string]ProxyInfo{"direct": {Name: "direct"}},
		})
	}))
	defer srv.Close()

	c := &ClashAPIClient{BaseURL: srv.URL}
	_, err := c.ValidateAllProxies(context.Background(), 5, "https://test.com", 5000)
	if err == nil {
		t.Fatal("expected error with no 'proxy' group")
	}
}

// --- GetTraffic ---

func TestGetTraffic_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(TrafficSnapshot{Up: 1024, Down: 4096})
	}))
	defer srv.Close()

	c := &ClashAPIClient{BaseURL: srv.URL}
	snap, err := c.GetTraffic(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Up != 1024 || snap.Down != 4096 {
		t.Errorf("traffic up=%d down=%d, want 1024/4096", snap.Up, snap.Down)
	}
}

func TestGetTraffic_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	c := &ClashAPIClient{BaseURL: srv.URL}
	_, err := c.GetTraffic(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- Context cancellation ---

func TestGetProxies_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := &ClashAPIClient{BaseURL: srv.URL}
	_, err := c.GetProxies(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

// --- E2E: ValidateAllProxies progress callback ---

// TestValidateAllProxies_ProgressCallback verifies the progress callback fires
// with correct done/total/alive counts as each proxy is tested.
func TestValidateAllProxies_ProgressCallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/proxies" {
			json.NewEncoder(w).Encode(ProxyStatus{
				Proxies: map[string]ProxyInfo{
					"proxy": {Now: "s0", All: []string{"s0", "s1", "s2", "s3"}},
					"s0":    {Name: "s0"},
					"s1":    {Name: "s1"},
					"s2":    {Name: "s2"},
					"s3":    {Name: "s3"},
				},
			})
			return
		}
		// s0 alive (50ms), s1 alive (100ms), s2 dead (timeout), s3 alive (75ms)
		switch {
		case r.URL.Path == "/proxies/s0/delay":
			json.NewEncoder(w).Encode(map[string]int{"delay": 50})
		case r.URL.Path == "/proxies/s1/delay":
			json.NewEncoder(w).Encode(map[string]int{"delay": 100})
		case r.URL.Path == "/proxies/s2/delay":
			w.WriteHeader(408)
		case r.URL.Path == "/proxies/s3/delay":
			json.NewEncoder(w).Encode(map[string]int{"delay": 75})
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	type progressEvent struct {
		done  int
		total int
		alive int
	}
	var mu sync.Mutex
	var events []progressEvent

	c := &ClashAPIClient{BaseURL: srv.URL}
	results, err := c.ValidateAllProxies(
		context.Background(),
		10,
		"http://test.local",
		5000,
		func(done, total, alive int) {
			mu.Lock()
			events = append(events, progressEvent{done, total, alive})
			mu.Unlock()
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()

	// Progress should fire once per proxy (4 total).
	if len(events) != 4 {
		t.Fatalf("progress called %d times, want 4", len(events))
	}

	// Every event should have total=4.
	for i, e := range events {
		if e.total != 4 {
			t.Errorf("events[%d].total = %d, want 4", i, e.total)
		}
	}

	// done values should be 1..4 (in some order due to concurrency).
	seenDone := map[int]bool{}
	for _, e := range events {
		seenDone[e.done] = true
	}
	for _, want := range []int{1, 2, 3, 4} {
		if !seenDone[want] {
			t.Errorf("never saw done=%d in progress events", want)
		}
	}

	// Final alive count: s0, s1, s3 are alive = 3 alive total.
	lastEvent := events[len(events)-1]
	// The last event by done count (done=4) should have alive=3.
	var finalEvent progressEvent
	for _, e := range events {
		if e.done == 4 {
			finalEvent = e
		}
	}
	if finalEvent.alive != 3 {
		t.Errorf("final alive = %d, want 3", finalEvent.alive)
	}
	_ = lastEvent

	// Verify results match.
	aliveCount := 0
	for _, r := range results {
		if r.Alive {
			aliveCount++
		}
	}
	if aliveCount != 3 {
		t.Errorf("results alive = %d, want 3", aliveCount)
	}
}

// TestValidateAllProxies_IPBasedURL verifies the test URL is passed through to
// the delay endpoint as-is. The production code uses http://1.1.1.1/cdn-cgi/trace
// which is IP-based (no DNS required). This test confirms the URL is forwarded correctly.
func TestValidateAllProxies_IPBasedURL(t *testing.T) {
	var capturedURLs []string
	var urlMu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/proxies" {
			json.NewEncoder(w).Encode(ProxyStatus{
				Proxies: map[string]ProxyInfo{
					"proxy": {Now: "s0", All: []string{"s0"}},
					"s0":    {Name: "s0"},
				},
			})
			return
		}
		if strings.Contains(r.URL.Path, "/delay") {
			// Capture the url query parameter.
			urlParam := r.URL.Query().Get("url")
			urlMu.Lock()
			capturedURLs = append(capturedURLs, urlParam)
			urlMu.Unlock()
			json.NewEncoder(w).Encode(map[string]int{"delay": 42})
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	c := &ClashAPIClient{BaseURL: srv.URL}

	// Use IP-based URL (no DNS needed).
	ipURL := "http://1.1.1.1/cdn-cgi/trace"
	_, err := c.ValidateAllProxies(context.Background(), 5, ipURL, 5000)
	if err != nil {
		t.Fatal(err)
	}

	urlMu.Lock()
	defer urlMu.Unlock()

	if len(capturedURLs) != 1 {
		t.Fatalf("captured %d URL params, want 1", len(capturedURLs))
	}
	if capturedURLs[0] != ipURL {
		t.Errorf("test URL = %q, want %q", capturedURLs[0], ipURL)
	}

	// The URL must be IP-based, not a hostname like gstatic.
	if strings.Contains(capturedURLs[0], "gstatic") {
		t.Error("test URL should use IP-based URL (1.1.1.1), not gstatic (requires DNS)")
	}
	if !strings.HasPrefix(capturedURLs[0], "http://1.1.1.1") {
		t.Errorf("test URL should start with http://1.1.1.1, got %q", capturedURLs[0])
	}
}
