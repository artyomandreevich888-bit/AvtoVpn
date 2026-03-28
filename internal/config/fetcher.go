package config

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// ConfigSources lists all VLESS config files to fetch and merge.
var ConfigSources = []string{
	"https://raw.githubusercontent.com/igareck/vpn-configs-for-russia/main/Vless-Reality-White-Lists-Rus-Mobile.txt",
	"https://raw.githubusercontent.com/igareck/vpn-configs-for-russia/main/BLACK_VLESS_RUS.txt",
	"https://raw.githubusercontent.com/igareck/vpn-configs-for-russia/main/BLACK_VLESS_RUS_mobile.txt",
}

type Fetcher struct {
	Client   *http.Client
	CacheDir string
	URLs     []string // override ConfigSources for testing
}

func (f *Fetcher) Fetch(ctx context.Context) ([]VlessConfig, error) {
	urls := f.URLs
	if len(urls) == 0 {
		urls = ConfigSources
	}

	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}

	// Fetch all sources, merge results
	seen := make(map[string]bool)
	var all []VlessConfig
	var allBodies []string

	for _, u := range urls {
		body, err := f.fetchURL(ctx, client, u)
		if err != nil || strings.TrimSpace(body) == "" {
			continue
		}
		allBodies = append(allBodies, body)
		configs, _ := ParseConfigFile(body)
		for _, c := range configs {
			key := c.Host + ":" + fmt.Sprint(c.Port)
			if !seen[key] {
				seen[key] = true
				all = append(all, c)
			}
		}
	}

	if len(all) > 0 {
		f.writeCache(strings.Join(allBodies, "\n"))
		return all, nil
	}

	// All URLs failed — try cache
	return f.readCache()
}

func (f *Fetcher) fetchURL(ctx context.Context, client *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

func (f *Fetcher) cachePath() string {
	return filepath.Join(f.CacheDir, "configs.txt")
}

func (f *Fetcher) writeCache(text string) {
	os.MkdirAll(f.CacheDir, 0755)
	os.WriteFile(f.cachePath(), []byte(text), 0644)
}

func (f *Fetcher) readCache() ([]VlessConfig, error) {
	data, err := os.ReadFile(f.cachePath())
	if err != nil {
		return nil, fmt.Errorf("all sources failed and no cache available")
	}

	configs, _ := ParseConfigFile(string(data))
	if len(configs) == 0 {
		return nil, fmt.Errorf("cache exists but contains no valid configs")
	}

	return configs, nil
}
