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

var DefaultURLs = []string{
	"https://raw.githubusercontent.com/igareck/vpn-configs-for-russia/main/BLACK_VLESS_RUS.txt",
	"https://cdn.jsdelivr.net/gh/igareck/vpn-configs-for-russia@main/BLACK_VLESS_RUS.txt",
	"https://cdn.statically.io/gh/igareck/vpn-configs-for-russia/main/BLACK_VLESS_RUS.txt",
}

type Fetcher struct {
	Client   *http.Client
	CacheDir string
	URLs     []string
}

func (f *Fetcher) Fetch(ctx context.Context) ([]VlessConfig, error) {
	urls := f.URLs
	if len(urls) == 0 {
		urls = DefaultURLs
	}

	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}

	for _, u := range urls {
		body, err := f.fetchURL(ctx, client, u)
		if err != nil || strings.TrimSpace(body) == "" {
			continue
		}

		configs, _ := ParseConfigFile(body)
		if len(configs) == 0 {
			continue
		}

		f.writeCache(body)
		return configs, nil
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
