package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
)

type ClashAPIClient struct {
	BaseURL string // default: http://127.0.0.1:9090
	Secret  string // authorization secret
	Client  *http.Client
}

type ProxyInfo struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Now     string `json:"now"`     // currently selected (for selector/urltest)
	History []struct {
		Delay int `json:"delay"` // ms, 0 = timeout
	} `json:"history"`
	All []string `json:"all"` // member outbounds
}

type ProxyStatus struct {
	Proxies map[string]ProxyInfo `json:"proxies"`
}

// ServerStatus is a simplified view for the UI.
type ServerStatus struct {
	CurrentServer string // display name of active server
	CurrentDelay  int    // latency in ms
	AliveCount    int
	TotalCount    int
	Servers       []ServerInfo // per-server detail
}

// ServerInfo holds status of a single proxy server.
type ServerInfo struct {
	Name   string
	Delay  int  // ms, 0 = timeout/dead
	Alive  bool
	Active bool // currently selected
}

// TrafficSnapshot holds a single reading from /traffic.
type TrafficSnapshot struct {
	Up   int64 `json:"up"`   // bytes/sec upload
	Down int64 `json:"down"` // bytes/sec download
}

func (c *ClashAPIClient) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return "http://127.0.0.1:9090"
}

func (c *ClashAPIClient) client() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return http.DefaultClient
}

func (c *ClashAPIClient) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL()+path, body)
	if err != nil {
		return nil, err
	}
	if c.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.Secret)
	}
	req.Header.Set("Content-Type", "application/json")
	return c.client().Do(req)
}

func (c *ClashAPIClient) GetProxies(ctx context.Context) (*ProxyStatus, error) {
	resp, err := c.do(ctx, http.MethodGet, "/proxies", nil)
	if err != nil {
		return nil, fmt.Errorf("get proxies: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get proxies: HTTP %d", resp.StatusCode)
	}

	var status ProxyStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("decode proxies: %w", err)
	}
	return &status, nil
}

func (c *ClashAPIClient) SelectProxy(ctx context.Context, group, name string) error {
	body := fmt.Sprintf(`{"name":%q}`, name)
	resp, err := c.do(ctx, http.MethodPut, "/proxies/"+group, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("select proxy: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("select proxy: HTTP %d", resp.StatusCode)
	}
	return nil
}

// GetStatus returns a simplified view by reading the "proxy" urltest group.
func (c *ClashAPIClient) GetStatus(ctx context.Context) (*ServerStatus, error) {
	ps, err := c.GetProxies(ctx)
	if err != nil {
		return nil, err
	}

	auto, ok := ps.Proxies["auto"]
	if !ok {
		return &ServerStatus{}, nil
	}

	status := &ServerStatus{
		CurrentServer: auto.Now,
		TotalCount:    len(auto.All),
	}

	// Find best server if urltest hasn't selected one yet
	bestDelay := 0
	bestName := ""

	// Collect per-server detail
	for _, name := range auto.All {
		p, ok := ps.Proxies[name]
		if !ok {
			continue
		}
		si := ServerInfo{Name: name, Active: name == auto.Now}
		if len(p.History) > 0 {
			si.Delay = p.History[len(p.History)-1].Delay
			si.Alive = si.Delay > 0
		}
		if si.Alive {
			status.AliveCount++
			if bestDelay == 0 || si.Delay < bestDelay {
				bestDelay = si.Delay
				bestName = name
			}
		}
		if si.Active {
			status.CurrentDelay = si.Delay
		}
		status.Servers = append(status.Servers, si)
	}

	// urltest may not populate Now immediately — fall back to fastest known server
	if status.CurrentServer == "" && bestName != "" {
		status.CurrentServer = bestName
		status.CurrentDelay = bestDelay
	}

	return status, nil
}

// TestProxyDelay tests a single proxy's latency via Clash API.
// Returns delay in ms, 0 if timeout/error.
func (c *ClashAPIClient) TestProxyDelay(ctx context.Context, name string, testURL string, timeoutMs int) (int, error) {
	path := fmt.Sprintf("/proxies/%s/delay?url=%s&timeout=%d", name, url.QueryEscape(testURL), timeoutMs)
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var result struct {
		Delay int `json:"delay"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}
	return result.Delay, nil
}

// ValidateResult holds per-server validation result.
type ValidateResult struct {
	Name  string
	Delay int  // ms, 0 = dead
	Alive bool
}

// ValidateAllProxies tests all proxies in the "proxy" group concurrently.
// concurrency controls max parallel tests. onProgress called after each test.
func (c *ClashAPIClient) ValidateAllProxies(ctx context.Context, concurrency int, testURL string, timeoutMs int, onProgress ...func(done, total, alive int)) ([]ValidateResult, error) {
	ps, err := c.GetProxies(ctx)
	if err != nil {
		return nil, err
	}

	auto, ok := ps.Proxies["auto"]
	if !ok {
		return nil, fmt.Errorf("no 'proxy' group found")
	}

	total := len(auto.All)
	results := make([]ValidateResult, total)
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var done, aliveCount int64

	var progressFn func(int, int, int)
	if len(onProgress) > 0 {
		progressFn = onProgress[0]
	}

	for i, name := range auto.All {
		results[i].Name = name
		wg.Add(1)
		go func(idx int, proxyName string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			delay, err := c.TestProxyDelay(ctx, proxyName, testURL, timeoutMs)
			if err == nil && delay > 0 {
				results[idx].Delay = delay
				results[idx].Alive = true
				atomic.AddInt64(&aliveCount, 1)
			} else if err != nil {
				log.Printf("[clashapi] %s: delay test failed: %v", proxyName, err)
			}
			d := int(atomic.AddInt64(&done, 1))
			a := int(atomic.LoadInt64(&aliveCount))
			if progressFn != nil {
				progressFn(d, total, a)
			}
		}(i, name)
	}

	wg.Wait()
	return results, nil
}
func (c *ClashAPIClient) GetTraffic(ctx context.Context) (*TrafficSnapshot, error) {
	resp, err := c.do(ctx, http.MethodGet, "/traffic", nil)
	if err != nil {
		return nil, fmt.Errorf("get traffic: %w", err)
	}
	defer resp.Body.Close()

	var snap TrafficSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		return nil, fmt.Errorf("decode traffic: %w", err)
	}
	return &snap, nil
}
