package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

// GetStatus returns a simplified view by reading the "auto" urltest group.
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

	// Count alive servers and get current delay
	for _, name := range auto.All {
		p, ok := ps.Proxies[name]
		if !ok {
			continue
		}
		if len(p.History) > 0 && p.History[len(p.History)-1].Delay > 0 {
			status.AliveCount++
			if name == auto.Now {
				status.CurrentDelay = p.History[len(p.History)-1].Delay
			}
		}
	}

	return status, nil
}
