package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const proxiesJSON = `{
  "proxies": {
    "proxy": {
      "name": "proxy",
      "type": "Selector",
      "now": "auto",
      "all": ["auto", "server-0", "server-1"]
    },
    "auto": {
      "name": "auto",
      "type": "URLTest",
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

	if len(ps.Proxies) != 4 {
		t.Errorf("got %d proxies, want 4", len(ps.Proxies))
	}

	auto := ps.Proxies["auto"]
	if auto.Now != "server-0" {
		t.Errorf("auto.Now = %q, want server-0", auto.Now)
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
