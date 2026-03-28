package mobile

import (
	"encoding/json"
	"testing"
)

func baseConfig() []byte {
	cfg := map[string]any{
		"inbounds": []map[string]any{
			{
				"type":         "tun",
				"tag":          "tun-in",
				"address":      []string{"172.19.0.1/30", "fdfe:dcba:9876::1/126"},
				"auto_route":   true,
				"strict_route": true,
				"stack":        "mixed",
			},
		},
		"experimental": map[string]any{
			"clash_api": map[string]any{
				"external_controller": "127.0.0.1:9090",
				"secret":              "autovpn",
			},
			"cache_file": map[string]any{
				"enabled": true,
				"path":    "cache.db",
			},
		},
	}
	data, _ := json.Marshal(cfg)
	return data
}

func mustUnmarshal(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	return m
}

func getTunInbound(t *testing.T, cfg map[string]any) map[string]any {
	t.Helper()
	inbounds := cfg["inbounds"].([]any)
	return inbounds[0].(map[string]any)
}

func TestPatchMobileConfig_IPv4Only(t *testing.T) {
	patched, err := PatchMobileConfig(baseConfig(), "/tmp/cache")
	if err != nil {
		t.Fatal(err)
	}
	cfg := mustUnmarshal(t, patched)
	tunIn := getTunInbound(t, cfg)

	addrs := tunIn["address"].([]any)
	if len(addrs) != 1 {
		t.Fatalf("address count = %d, want 1 (IPv4 only)", len(addrs))
	}
	if addrs[0] != "172.19.0.1/30" {
		t.Errorf("address = %v, want 172.19.0.1/30", addrs[0])
	}
}

func TestPatchMobileConfig_StrictRoute(t *testing.T) {
	patched, err := PatchMobileConfig(baseConfig(), "/tmp/cache")
	if err != nil {
		t.Fatal(err)
	}
	cfg := mustUnmarshal(t, patched)
	tunIn := getTunInbound(t, cfg)

	if tunIn["strict_route"] != true {
		t.Errorf("strict_route = %v, want true", tunIn["strict_route"])
	}
}

func TestPatchMobileConfig_ExcludePackage(t *testing.T) {
	patched, err := PatchMobileConfig(baseConfig(), "/tmp/cache")
	if err != nil {
		t.Fatal(err)
	}
	cfg := mustUnmarshal(t, patched)
	tunIn := getTunInbound(t, cfg)

	pkgs, ok := tunIn["exclude_package"].([]any)
	if !ok || len(pkgs) == 0 {
		t.Fatal("exclude_package not set")
	}
	found := false
	for _, p := range pkgs {
		if p == "com.android.captiveportallogin" {
			found = true
		}
	}
	if !found {
		t.Error("captive portal package not in exclude_package")
	}
}

func TestPatchMobileConfig_CacheFilePath(t *testing.T) {
	patched, err := PatchMobileConfig(baseConfig(), "/data/user/0/com.autovpn/cache")
	if err != nil {
		t.Fatal(err)
	}
	cfg := mustUnmarshal(t, patched)

	exp := cfg["experimental"].(map[string]any)
	cf := exp["cache_file"].(map[string]any)
	path := cf["path"].(string)
	if path != "/data/user/0/com.autovpn/cache/cache.db" {
		t.Errorf("cache path = %v, want /data/user/0/com.autovpn/cache/cache.db", path)
	}
}

func TestPatchMobileConfig_RemovesAutoDetectInterface(t *testing.T) {
	raw := baseConfig()
	// Inject auto_detect_interface into route
	var cfg map[string]any
	json.Unmarshal(raw, &cfg)
	cfg["route"] = map[string]any{"auto_detect_interface": true, "final": "proxy"}
	raw, _ = json.Marshal(cfg)

	patched, err := PatchMobileConfig(raw, "/tmp/cache")
	if err != nil {
		t.Fatal(err)
	}
	result := mustUnmarshal(t, patched)
	route := result["route"].(map[string]any)
	if _, exists := route["auto_detect_interface"]; exists {
		t.Error("auto_detect_interface should be removed on mobile")
	}
	if route["final"] != "proxy" {
		t.Error("other route fields should be preserved")
	}
}

func TestPatchMobileConfig_InvalidJSON(t *testing.T) {
	_, err := PatchMobileConfig([]byte("not json"), "/tmp")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestTunMTU(t *testing.T) {
	if TunMTU != 9000 {
		t.Errorf("TunMTU = %d, want 9000", TunMTU)
	}
}
