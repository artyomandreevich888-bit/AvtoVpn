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
		Transport:   "xhttp",
		Security:    "reality",
		Fingerprint: "chrome",
		SNI:         host,
		PublicKey:    "testPublicKeyBase64",
		ShortID:     "abcd1234",
		Path:        "/xhttp/",
		Mode:        "packet-up",
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

	// selector + urltest + 1 vless + direct + block = 5
	if len(outbounds) != 5 {
		t.Errorf("got %d outbounds, want 5", len(outbounds))
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
	if len(selOuts) != 2 { // "auto" + "server-0"
		t.Errorf("selector outbounds = %d, want 2", len(selOuts))
	}
	if selOuts[0] != "auto" {
		t.Errorf("selector first outbound = %v, want auto", selOuts[0])
	}

	// Second outbound is urltest
	ut := outbounds[1].(map[string]any)
	if ut["type"] != "urltest" {
		t.Errorf("second outbound type = %v, want urltest", ut["type"])
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

	// selector + urltest + 3 vless + direct + block = 7
	if len(outbounds) != 7 {
		t.Errorf("got %d outbounds, want 7", len(outbounds))
	}

	// urltest should have 3 server outbounds
	ut := outbounds[1].(map[string]any)
	utOuts := ut["outbounds"].([]any)
	if len(utOuts) != 3 {
		t.Errorf("urltest outbounds = %d, want 3", len(utOuts))
	}

	// selector should have "auto" + 3 servers
	sel := outbounds[0].(map[string]any)
	selOuts := sel["outbounds"].([]any)
	if len(selOuts) != 4 {
		t.Errorf("selector outbounds = %d, want 4", len(selOuts))
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

	// Third outbound (index 2) is the VLESS server
	vless := outbounds[2].(map[string]any)
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
	if transport["type"] != "xhttp" {
		t.Errorf("transport type = %v", transport["type"])
	}
	if transport["path"] != "/xhttp/" {
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
	vless := outbounds[2].(map[string]any)

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
	vless := outbounds[2].(map[string]any)

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
