package mobile

import (
	"encoding/json"
	"path/filepath"

	"github.com/mewmewmemw/autovpn/internal/config"
)

// buildMobileConfig creates a sing-box config for Android/iOS.
func buildMobileConfig(servers []config.VlessConfig, cacheDir string) ([]byte, error) {
	configJSON, err := config.BuildConfig(servers)
	if err != nil {
		return nil, err
	}

	var cfg map[string]any
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return nil, err
	}

	// Patch TUN inbound for mobile:
	// auto_route MUST stay true — PlatformInterface.OpenInterface handles it
	// on Android, but gvisor needs it for internal packet routing.
	if inbounds, ok := cfg["inbounds"].([]any); ok && len(inbounds) > 0 {
		if tunIn, ok := inbounds[0].(map[string]any); ok {
			tunIn["strict_route"] = true
			// IPv4 only — avoids IPv6 bind errors on Android
			tunIn["address"] = []string{"172.19.0.1/30"}
		}
	}

	// Fix cache_file path — Android root is read-only
	if exp, ok := cfg["experimental"].(map[string]any); ok {
		if cf, ok := exp["cache_file"].(map[string]any); ok {
			cf["path"] = filepath.Join(cacheDir, "cache.db")
		}
	}

	return json.MarshalIndent(cfg, "", "  ")
}
