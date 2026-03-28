package mobile

import (
	"encoding/json"
	"path/filepath"
)

// TunMTU is the MTU used for both the sing-box config and Android VpnService.Builder.
// Keep in sync with AutoVpnService.kt.
const TunMTU = 9000

// PatchMobileConfig adjusts a sing-box config for Android/iOS:
//   - IPv4-only TUN address (avoids IPv6 bind errors on Android)
//   - strict_route enabled (kill switch)
//   - Writable cache_file path (Android root is read-only)
//   - Exclude captive portal from VPN (hotel/cafe WiFi login)
func PatchMobileConfig(configJSON []byte, cacheDir string) ([]byte, error) {
	var cfg map[string]any
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return nil, err
	}

	if inbounds, ok := cfg["inbounds"].([]any); ok && len(inbounds) > 0 {
		if tunIn, ok := inbounds[0].(map[string]any); ok {
			tunIn["strict_route"] = true
			// IPv4 only — no IPv6 address to avoid bind errors on Android.
			// Kotlin Builder must also NOT add addRoute("::", 0).
			tunIn["address"] = []string{"172.19.0.1/30"}
			tunIn["exclude_package"] = []string{
				"com.android.captiveportallogin",
			}
		}
	}

	// auto_detect_interface MUST stay true on mobile.
	// It's the master switch that enables socket protection — when PlatformInterface
	// is set and UsePlatformAutoDetectInterfaceControl() returns true, sing-box
	// delegates to AutoDetectInterfaceControl(fd) → VpnService.protect(fd).
	// Without it, outbound sockets go through TUN → routing loop → all servers dead.

	// Fix cache_file path — Android root is read-only.
	if exp, ok := cfg["experimental"].(map[string]any); ok {
		if cf, ok := exp["cache_file"].(map[string]any); ok {
			cf["path"] = filepath.Join(cacheDir, "cache.db")
		}
	}

	return json.MarshalIndent(cfg, "", "  ")
}
