package config

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

const testConfigBody = `# profile-title: test
# profile-update-interval: 5

vless://uuid1@host1.com:443?security=reality&sni=host1.com&pbk=key1&sid=01&fp=chrome
vless://uuid2@host2.com:443?security=reality&sni=host2.com&pbk=key2&sid=02&fp=chrome
`

// testMixedBody contains both reality and non-reality configs.
const testMixedBody = `vless://uuid1@host1.com:443?security=reality&sni=host1.com&pbk=key1&sid=01&fp=chrome
vless://uuid-skip@host-no-reality.com:443?security=tls&sni=example.com
vless://uuid2@host2.com:443?security=reality&sni=host2.com&pbk=key2&sid=02&fp=chrome
vless://uuid-skip2@host-none.com:80?security=none&type=ws
`

func TestFetcher_FirstURLSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(testConfigBody))
	}))
	defer srv.Close()

	f := &Fetcher{
		Client:   srv.Client(),
		CacheDir: t.TempDir(),
		URLs:     []string{srv.URL},
	}

	configs, err := f.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 2 {
		t.Errorf("got %d configs, want 2", len(configs))
	}
}

func TestFetcher_FallbackOnError(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(500)
			return
		}
		w.Write([]byte(testConfigBody))
	}))
	defer srv.Close()

	f := &Fetcher{
		Client:   srv.Client(),
		CacheDir: t.TempDir(),
		URLs:     []string{srv.URL + "/bad", srv.URL + "/good"},
	}

	configs, err := f.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 2 {
		t.Errorf("got %d configs, want 2", len(configs))
	}
}

func TestFetcher_AllFailWithCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	cacheDir := t.TempDir()
	// Pre-populate cache
	os.WriteFile(filepath.Join(cacheDir, "configs.txt"), []byte(testConfigBody), 0644)

	f := &Fetcher{
		Client:   srv.Client(),
		CacheDir: cacheDir,
		URLs:     []string{srv.URL},
	}

	configs, err := f.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 2 {
		t.Errorf("got %d configs from cache, want 2", len(configs))
	}
}

func TestFetcher_AllFailNoCache_UsesEmbedded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	f := &Fetcher{
		Client:   srv.Client(),
		CacheDir: t.TempDir(),
		URLs:     []string{srv.URL},
	}

	// With embedded fallback.txt populated, this should succeed.
	configs, err := f.Fetch(context.Background())
	if err != nil {
		t.Fatalf("expected embedded fallback, got error: %v", err)
	}
	if len(configs) == 0 {
		t.Fatal("expected configs from embedded fallback")
	}
}

func TestFetcher_EmptyBodySkipped(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Write([]byte(""))
			return
		}
		w.Write([]byte(testConfigBody))
	}))
	defer srv.Close()

	f := &Fetcher{
		Client:   srv.Client(),
		CacheDir: t.TempDir(),
		URLs:     []string{srv.URL + "/empty", srv.URL + "/good"},
	}

	configs, err := f.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 2 {
		t.Errorf("got %d configs, want 2", len(configs))
	}
}

func TestFetcher_CacheWritten(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(testConfigBody))
	}))
	defer srv.Close()

	cacheDir := t.TempDir()
	f := &Fetcher{
		Client:   srv.Client(),
		CacheDir: cacheDir,
		URLs:     []string{srv.URL},
	}

	f.Fetch(context.Background())

	cached, err := os.ReadFile(filepath.Join(cacheDir, "configs.txt"))
	if err != nil {
		t.Fatalf("cache file not written: %v", err)
	}
	if string(cached) != testConfigBody {
		t.Error("cache content doesn't match")
	}
}

func TestFetcher_RealityFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(testMixedBody))
	}))
	defer srv.Close()

	f := &Fetcher{
		Client:   srv.Client(),
		CacheDir: t.TempDir(),
		URLs:     []string{srv.URL},
	}

	configs, err := f.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// testMixedBody has 4 lines: 2 reality, 2 non-reality.
	if len(configs) != 2 {
		t.Errorf("got %d configs, want 2 (reality only)", len(configs))
	}
	for _, c := range configs {
		if c.Security != "reality" {
			t.Errorf("non-reality config leaked: %s security=%s", c.Host, c.Security)
		}
	}
}

func TestFetcher_EmbeddedFallback(t *testing.T) {
	// All URLs fail, no cache — should fall back to embedded.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	f := &Fetcher{
		Client:   srv.Client(),
		CacheDir: t.TempDir(),
		URLs:     []string{srv.URL},
	}

	configs, err := f.Fetch(context.Background())
	// Embedded fallback.txt has real configs — should succeed.
	if err != nil {
		t.Fatalf("expected embedded fallback to work: %v", err)
	}
	if len(configs) == 0 {
		t.Fatal("embedded fallback returned 0 configs")
	}
	for _, c := range configs {
		if c.Security != "reality" {
			t.Errorf("embedded config not reality: %s security=%s", c.Host, c.Security)
		}
	}
}

// TestFetcher_ParallelFetch verifies multiple URLs are fetched concurrently.
// 3 servers each with 100ms delay => if sequential would take >= 300ms.
// With parallel fetch, total should be < 200ms.
func TestFetcher_ParallelFetch(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Write([]byte(testConfigBody))
	}
	srv1 := httptest.NewServer(http.HandlerFunc(handler))
	defer srv1.Close()
	srv2 := httptest.NewServer(http.HandlerFunc(handler))
	defer srv2.Close()
	srv3 := httptest.NewServer(http.HandlerFunc(handler))
	defer srv3.Close()

	f := &Fetcher{
		Client:   &http.Client{Timeout: 5 * time.Second},
		CacheDir: t.TempDir(),
		URLs:     []string{srv1.URL, srv2.URL, srv3.URL},
	}

	start := time.Now()
	configs, err := f.Fetch(context.Background())
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if len(configs) == 0 {
		t.Fatal("expected configs, got 0")
	}

	// Parallel: ~100ms. Sequential: ~300ms. Allow generous margin.
	if elapsed > 200*time.Millisecond {
		t.Errorf("fetch took %v, expected < 200ms (sources should be parallel)", elapsed)
	}
}

// TestFetcher_ProgressCallback verifies OnProgress fires for each source.
func TestFetcher_ProgressCallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(testConfigBody))
	}))
	defer srv.Close()

	type progressEvent struct {
		current int
		total   int
		servers int
	}
	var mu sync.Mutex
	var events []progressEvent

	f := &Fetcher{
		Client:   srv.Client(),
		CacheDir: t.TempDir(),
		URLs:     []string{srv.URL + "/a", srv.URL + "/b", srv.URL + "/c"},
		OnProgress: func(current, total, servers int) {
			mu.Lock()
			events = append(events, progressEvent{current, total, servers})
			mu.Unlock()
		},
	}

	_, err := f.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// OnProgress must fire once per source (3 total).
	if len(events) != 3 {
		t.Fatalf("OnProgress called %d times, want 3", len(events))
	}

	// Every event should report total=3.
	for i, e := range events {
		if e.total != 3 {
			t.Errorf("event[%d].total = %d, want 3", i, e.total)
		}
	}

	// current values should be 1, 2, 3 (in some order, since parallel).
	seenCurrent := map[int]bool{}
	for _, e := range events {
		seenCurrent[e.current] = true
	}
	for _, want := range []int{1, 2, 3} {
		if !seenCurrent[want] {
			t.Errorf("never saw current=%d in progress events", want)
		}
	}

	// The last event should have servers > 0 (at least some parsed).
	lastServers := events[len(events)-1].servers
	if lastServers == 0 {
		t.Error("last progress event has 0 servers, expected some parsed configs")
	}
}

func TestIsReality(t *testing.T) {
	tests := []struct {
		name   string
		config VlessConfig
		want   bool
	}{
		{
			name:   "full reality",
			config: VlessConfig{Security: "reality", PublicKey: "key", Fingerprint: "chrome"},
			want:   true,
		},
		{
			name:   "tls not reality",
			config: VlessConfig{Security: "tls", Fingerprint: "chrome"},
			want:   false,
		},
		{
			name:   "reality missing pubkey",
			config: VlessConfig{Security: "reality", Fingerprint: "chrome"},
			want:   false,
		},
		{
			name:   "reality missing fingerprint",
			config: VlessConfig{Security: "reality", PublicKey: "key"},
			want:   false,
		},
		{
			name:   "none",
			config: VlessConfig{Security: "none"},
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isReality(tt.config); got != tt.want {
				t.Errorf("isReality() = %v, want %v", got, tt.want)
			}
		})
	}
}
