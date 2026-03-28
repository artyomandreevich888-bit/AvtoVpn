package config

import (
	"encoding/json"
	"testing"
)

func realityServer(tag string, host string) VlessConfig {
	return VlessConfig{
		UUID:        "test-uuid-" + tag,
		Host:        host,
		Port:        443,
		Transport:   "ws",
		Security:    "reality",
		Fingerprint: "chrome",
		SNI:         host,
		PublicKey:    "testPublicKeyBase64",
		ShortID:     "abcd1234",
		Path:        "/ws/",
	}
}

func mustUnmarshal(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	return m
}

func TestBuildConfig_SingleServer(t *testing.T) {
	servers := []VlessConfig{realityServer("1", "host1.com")}
	data, err := BuildConfig(servers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := mustUnmarshal(t, data)

	// Check outbounds exist
	outbounds, ok := m["outbounds"].([]any)
	if !ok {
		t.Fatal("outbounds not an array")
	}

	// selector + 1 vless + direct + block = 4
	if len(outbounds) != 4 {
		t.Errorf("got %d outbounds, want 4", len(outbounds))
	}

	// First outbound is selector
	sel := outbounds[0].(map[string]any)
	if sel["type"] != "selector" {
		t.Errorf("first outbound type = %v, want selector", sel["type"])
	}
	if sel["tag"] != "proxy" {
		t.Errorf("selector tag = %v", sel["tag"])
	}
	selOuts := sel["outbounds"].([]any)
	if len(selOuts) != 1 { // "server-0"
		t.Errorf("selector outbounds = %d, want 1", len(selOuts))
	}
	if selOuts[0] != "server-0" {
		t.Errorf("selector first outbound = %v, want server-0", selOuts[0])
	}
}

func TestBuildConfig_MultipleServers(t *testing.T) {
	servers := []VlessConfig{
		realityServer("1", "host1.com"),
		realityServer("2", "host2.com"),
		realityServer("3", "host3.com"),
	}
	data, err := BuildConfig(servers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := mustUnmarshal(t, data)
	outbounds := m["outbounds"].([]any)

	// selector + 3 vless + direct + block = 6
	if len(outbounds) != 6 {
		t.Errorf("got %d outbounds, want 6", len(outbounds))
	}

	// selector should have 3 servers directly
	sel := outbounds[0].(map[string]any)
	selOuts := sel["outbounds"].([]any)
	if len(selOuts) != 3 {
		t.Errorf("selector outbounds = %d, want 3", len(selOuts))
	}
}

func TestBuildConfig_VlessFields(t *testing.T) {
	servers := []VlessConfig{realityServer("1", "host1.com")}
	data, err := BuildConfig(servers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := mustUnmarshal(t, data)
	outbounds := m["outbounds"].([]any)

	// Second outbound (index 1) is the VLESS server
	vless := outbounds[1].(map[string]any)
	if vless["type"] != "vless" {
		t.Fatalf("outbound type = %v, want vless", vless["type"])
	}
	if vless["server"] != "host1.com" {
		t.Errorf("server = %v", vless["server"])
	}
	if vless["server_port"] != float64(443) {
		t.Errorf("server_port = %v", vless["server_port"])
	}
	if vless["uuid"] != "test-uuid-1" {
		t.Errorf("uuid = %v", vless["uuid"])
	}

	// TLS
	tls := vless["tls"].(map[string]any)
	if tls["enabled"] != true {
		t.Error("tls not enabled")
	}
	if tls["server_name"] != "host1.com" {
		t.Errorf("server_name = %v", tls["server_name"])
	}

	// Reality
	reality := tls["reality"].(map[string]any)
	if reality["enabled"] != true {
		t.Error("reality not enabled")
	}
	if reality["public_key"] != "testPublicKeyBase64" {
		t.Errorf("public_key = %v", reality["public_key"])
	}

	// uTLS
	utls := tls["utls"].(map[string]any)
	if utls["fingerprint"] != "chrome" {
		t.Errorf("fingerprint = %v", utls["fingerprint"])
	}

	// Transport
	transport := vless["transport"].(map[string]any)
	if transport["type"] != "ws" {
		t.Errorf("transport type = %v", transport["type"])
	}
	if transport["path"] != "/ws/" {
		t.Errorf("transport path = %v", transport["path"])
	}
}

func TestBuildConfig_NoTransportForTCP(t *testing.T) {
	srv := VlessConfig{
		UUID:     "uuid",
		Host:     "host.com",
		Port:     443,
		Security: "tls",
		SNI:      "host.com",
	}
	data, err := BuildConfig([]VlessConfig{srv})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := mustUnmarshal(t, data)
	outbounds := m["outbounds"].([]any)
	vless := outbounds[1].(map[string]any)

	if _, ok := vless["transport"]; ok {
		t.Error("transport should be omitted for tcp/empty")
	}
}

func TestBuildConfig_NoTLSForNone(t *testing.T) {
	srv := VlessConfig{
		UUID:     "uuid",
		Host:     "host.com",
		Port:     443,
		Security: "none",
	}
	data, err := BuildConfig([]VlessConfig{srv})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := mustUnmarshal(t, data)
	outbounds := m["outbounds"].([]any)
	vless := outbounds[1].(map[string]any)

	if _, ok := vless["tls"]; ok {
		t.Error("tls should be omitted for security=none")
	}
}

func TestBuildConfig_ClashAPI(t *testing.T) {
	data, err := BuildConfig([]VlessConfig{realityServer("1", "h.com")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := mustUnmarshal(t, data)
	exp := m["experimental"].(map[string]any)
	api := exp["clash_api"].(map[string]any)

	if api["external_controller"] != "127.0.0.1:9090" {
		t.Errorf("external_controller = %v", api["external_controller"])
	}
}

func TestBuildConfig_EmptyServers(t *testing.T) {
	_, err := BuildConfig(nil)
	if err == nil {
		t.Fatal("expected error for empty servers")
	}
}
