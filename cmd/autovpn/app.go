package main

import (
	"context"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/mewmewmemw/autovpn/internal/app"
	"github.com/mewmewmemw/autovpn/internal/config"
)

// App struct is exposed to the frontend via Wails bindings.
type App struct {
	ctx     context.Context
	manager *app.Manager
}

func NewApp() *App {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = "/tmp"
	}
	cacheDir += "/autovpn"

	return &App{
		manager: app.NewManager(&config.Fetcher{
			CacheDir: cacheDir,
		}),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

type StatusResult struct {
	State      string `json:"State"`
	Server     string `json:"Server"`
	Delay      int    `json:"Delay"`
	AliveCount int    `json:"AliveCount"`
	TotalCount int    `json:"TotalCount"`
	Error      string `json:"Error"`
}

// Connect starts the VPN. Returns empty string on success, error message on failure.
func (a *App) Connect() string {
	if err := a.manager.Connect(); err != nil {
		return err.Error()
	}
	return ""
}

// Disconnect stops the VPN.
func (a *App) Disconnect() {
	a.manager.Disconnect()
}

// GetStatus returns current connection status.
func (a *App) GetStatus() StatusResult {
	s := a.manager.Status()
	return StatusResult{
		State:      string(s.State),
		Server:     s.Server,
		Delay:      s.Delay,
		AliveCount: s.AliveCount,
		TotalCount: s.TotalCount,
		Error:      s.Error,
	}
}

type ServiceCheck struct {
	Name   string `json:"Name"`
	URL    string `json:"URL"`
	Status string `json:"Status"` // "ok", "fail", "checking"
	Delay  int    `json:"Delay"`  // ms
}

var services = []struct {
	Name string
	URL  string
}{
	{"YouTube", "https://www.youtube.com"},
	{"Instagram", "https://www.instagram.com"},
	{"GitHub", "https://github.com"},
}

const slowThreshold = 3000 // ms — server considered bad if YouTube > 3s
const maxRetries = 5       // try up to 5 servers before giving up

// CheckServices tests connectivity to key services through the VPN.
// If YouTube is too slow or fails, automatically switches to next server.
func (a *App) CheckServices() []ServiceCheck {
	for attempt := 0; attempt < maxRetries; attempt++ {
		results := a.checkOnce()

		// Find YouTube result
		ytOK := false
		for _, r := range results {
			if r.Name == "YouTube" && r.Status == "ok" && r.Delay < slowThreshold {
				ytOK = true
			}
		}

		if ytOK {
			return results
		}

		// YouTube bad — try next server
		ctx := context.Background()
		ps, err := a.manager.ClashAPI.GetProxies(ctx)
		if err != nil {
			return results
		}

		auto, ok := ps.Proxies["auto"]
		if !ok || len(auto.All) < 2 {
			return results
		}

		// Find current server index and pick next
		current := auto.Now
		nextServer := ""
		for i, name := range auto.All {
			if name == current && i+1 < len(auto.All) {
				nextServer = auto.All[i+1]
				break
			}
		}
		if nextServer == "" {
			return results // no more servers to try
		}

		// Switch via selector
		a.manager.ClashAPI.SelectProxy(ctx, "proxy", nextServer)
	}

	return a.checkOnce()
}

func (a *App) checkOnce() []ServiceCheck {
	client := &http.Client{Timeout: 10 * time.Second}
	results := make([]ServiceCheck, len(services))

	var wg sync.WaitGroup
	for i, svc := range services {
		results[i] = ServiceCheck{Name: svc.Name, URL: svc.URL, Status: "checking"}
		wg.Add(1)
		go func(idx int, url string) {
			defer wg.Done()
			start := time.Now()
			resp, err := client.Head(url)
			elapsed := int(time.Since(start).Milliseconds())
			if err != nil || resp.StatusCode >= 500 {
				results[idx].Status = "fail"
				results[idx].Delay = elapsed
				return
			}
			resp.Body.Close()
			results[idx].Status = "ok"
			results[idx].Delay = elapsed
		}(i, svc.URL)
	}
	wg.Wait()
	return results
}
