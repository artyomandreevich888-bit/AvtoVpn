package config

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ConfigSources lists all VLESS config files to fetch and merge.
var ConfigSources = []string{
	// igareck — curated reality configs, updated every 1-2h
	"https://raw.githubusercontent.com/igareck/vpn-configs-for-russia/main/Vless-Reality-White-Lists-Rus-Mobile.txt",
	"https://raw.githubusercontent.com/igareck/vpn-configs-for-russia/main/BLACK_VLESS_RUS.txt",
	"https://raw.githubusercontent.com/igareck/vpn-configs-for-russia/main/BLACK_VLESS_RUS_mobile.txt",
	// F0rc3Run — large pool (~500), we filter to reality-only (~50-90)
	"https://raw.githubusercontent.com/F0rc3Run/F0rc3Run/refs/heads/main/splitted-by-protocol/vless.txt",
}

// ConfigSource describes where configs were loaded from.
type ConfigSource string

const (
	SourceNetwork  ConfigSource = "network"
	SourceCache    ConfigSource = "cache"
	SourceEmbedded ConfigSource = "embedded"
)

// FetchResult holds configs and metadata about how they were obtained.
type FetchResult struct {
	Configs  []VlessConfig
	Source   ConfigSource
	CacheAge int64 // seconds since cache was written; 0 for network/embedded
}

type Fetcher struct {
	Client     *http.Client
	CacheDir   string
	URLs       []string                            // override ConfigSources for testing
	OnProgress func(current, total, servers int)    // optional progress callback
}

func (f *Fetcher) Fetch(ctx context.Context) ([]VlessConfig, error) {
	r, err := f.FetchWithMeta(ctx)
	if err != nil {
		return nil, err
	}
	return r.Configs, nil
}

func (f *Fetcher) FetchWithMeta(ctx context.Context) (*FetchResult, error) {
	urls := f.URLs
	if len(urls) == 0 {
		urls = ConfigSources
	}

	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}

	// Fetch all sources in parallel.
	type fetchResult struct {
		body string
		idx  int
	}
	results := make(chan fetchResult, len(urls))
	for i, u := range urls {
		go func(idx int, url string) {
			body, err := f.fetchURL(ctx, client, url)
			if err != nil || strings.TrimSpace(body) == "" {
				results <- fetchResult{idx: idx}
			} else {
				results <- fetchResult{body: body, idx: idx}
			}
		}(i, u)
	}

	// Collect results as they arrive, report progress.
	seen := make(map[string]bool)
	var all []VlessConfig
	var allBodies []string
	for done := 0; done < len(urls); done++ {
		r := <-results
		if f.OnProgress != nil {
			f.OnProgress(done+1, len(urls), len(all))
		}
		if r.body == "" {
			continue
		}
		allBodies = append(allBodies, r.body)
		configs, _ := ParseConfigFile(r.body)
		for _, c := range configs {
			if !isReality(c) {
				continue
			}
			key := c.Host + ":" + fmt.Sprint(c.Port)
			if !seen[key] {
				seen[key] = true
				all = append(all, c)
			}
		}
	}

	if len(all) > 0 {
		f.writeCache(strings.Join(allBodies, "\n"))
		return &FetchResult{Configs: all, Source: SourceNetwork}, nil
	}

	// Respect cancellation — don't try fallbacks if user disconnected.
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// All URLs failed — try cache.
	if configs, cacheAge, err := f.readCacheWithAge(); err == nil {
		return &FetchResult{Configs: configs, Source: SourceCache, CacheAge: cacheAge}, nil
	}

	// Cache empty — try embedded fallback (first run under blockade).
	configs, err := f.readEmbedded()
	if err != nil {
		return nil, err
	}
	return &FetchResult{Configs: configs, Source: SourceEmbedded}, nil
}

// isReality returns true for configs using VLESS+Reality (DPI-resistant).
func isReality(c VlessConfig) bool {
	return c.Security == "reality" && c.PublicKey != "" && c.Fingerprint != ""
}

func (f *Fetcher) readEmbedded() ([]VlessConfig, error) {
	data := strings.TrimSpace(fallbackConfigs)
	if data == "" {
		return nil, fmt.Errorf("all sources failed, no cache, no embedded configs")
	}
	configs, _ := ParseConfigFile(data)
	var filtered []VlessConfig
	for _, c := range configs {
		if isReality(c) {
			filtered = append(filtered, c)
		}
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("embedded configs contain no valid reality servers")
	}
	return filtered, nil
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
	configs, _, err := f.readCacheWithAge()
	return configs, err
}

func (f *Fetcher) readCacheWithAge() ([]VlessConfig, int64, error) {
	info, err := os.Stat(f.cachePath())
	if err != nil {
		return nil, 0, fmt.Errorf("all sources failed and no cache available")
	}
	ageSec := int64(time.Since(info.ModTime()).Seconds())

	data, err := os.ReadFile(f.cachePath())
	if err != nil {
		return nil, 0, err
	}

	configs, _ := ParseConfigFile(string(data))
	var filtered []VlessConfig
	for _, c := range configs {
		if isReality(c) {
			filtered = append(filtered, c)
		}
	}
	if len(filtered) == 0 {
		return nil, 0, fmt.Errorf("cache exists but contains no valid reality configs")
	}
	return filtered, ageSec, nil
}
