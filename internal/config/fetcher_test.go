package config

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const testConfigBody = `# profile-title: test
# profile-update-interval: 5

vless://uuid1@host1.com:443?security=reality&sni=host1.com&pbk=key1&sid=01
vless://uuid2@host2.com:443?security=reality&sni=host2.com&pbk=key2&sid=02
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

func TestFetcher_AllFailNoCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	f := &Fetcher{
		Client:   srv.Client(),
		CacheDir: t.TempDir(),
		URLs:     []string{srv.URL},
	}

	_, err := f.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error when all URLs fail and no cache")
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
