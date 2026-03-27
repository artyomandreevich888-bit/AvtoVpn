package config

import (
	"testing"
)

const testURI = "vless://0cfb8127-873d-46ca-9f31-f7918cdaaf11@xfer.mos-drywall.shop:443?type=xhttp&security=reality&encryption=none&fp=chrome&pbk=bPRyJNdN6kxTypxr65RjpadXexY5fztAecykcC-NrTM&sid=6a2f3e&sni=xfer.mos-drywall.shop&path=/devxhttp/&mode=packet-up#%F0%9F%87%A9%F0%9F%87%AA%20Germany%20Frankfurt%20%5BBL%5D"

func TestParseVlessURI_FullParams(t *testing.T) {
	cfg, err := ParseVlessURI(testURI)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.UUID != "0cfb8127-873d-46ca-9f31-f7918cdaaf11" {
		t.Errorf("UUID = %q, want %q", cfg.UUID, "0cfb8127-873d-46ca-9f31-f7918cdaaf11")
	}
	if cfg.Host != "xfer.mos-drywall.shop" {
		t.Errorf("Host = %q", cfg.Host)
	}
	if cfg.Port != 443 {
		t.Errorf("Port = %d, want 443", cfg.Port)
	}
	if cfg.Transport != "xhttp" {
		t.Errorf("Transport = %q, want %q", cfg.Transport, "xhttp")
	}
	if cfg.Security != "reality" {
		t.Errorf("Security = %q", cfg.Security)
	}
	if cfg.Fingerprint != "chrome" {
		t.Errorf("Fingerprint = %q", cfg.Fingerprint)
	}
	if cfg.SNI != "xfer.mos-drywall.shop" {
		t.Errorf("SNI = %q", cfg.SNI)
	}
	if cfg.PublicKey != "bPRyJNdN6kxTypxr65RjpadXexY5fztAecykcC-NrTM" {
		t.Errorf("PublicKey = %q", cfg.PublicKey)
	}
	if cfg.ShortID != "6a2f3e" {
		t.Errorf("ShortID = %q", cfg.ShortID)
	}
	if cfg.Path != "/devxhttp/" {
		t.Errorf("Path = %q", cfg.Path)
	}
	if cfg.Mode != "packet-up" {
		t.Errorf("Mode = %q", cfg.Mode)
	}
	if cfg.DisplayName != "🇩🇪 Germany Frankfurt [BL]" {
		t.Errorf("DisplayName = %q", cfg.DisplayName)
	}
	if cfg.RawURI != testURI {
		t.Errorf("RawURI not preserved")
	}
}

func TestParseVlessURI_MinimalParams(t *testing.T) {
	uri := "vless://test-uuid@example.com:8443"
	cfg, err := ParseVlessURI(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.UUID != "test-uuid" {
		t.Errorf("UUID = %q", cfg.UUID)
	}
	if cfg.Host != "example.com" {
		t.Errorf("Host = %q", cfg.Host)
	}
	if cfg.Port != 8443 {
		t.Errorf("Port = %d", cfg.Port)
	}
	if cfg.Transport != "" {
		t.Errorf("Transport = %q, want empty", cfg.Transport)
	}
	if cfg.DisplayName != "" {
		t.Errorf("DisplayName = %q, want empty", cfg.DisplayName)
	}
}

func TestParseVlessURI_WithFlow(t *testing.T) {
	uri := "vless://uuid@host.com:443?security=reality&flow=xtls-rprx-vision"
	cfg, err := ParseVlessURI(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Flow != "xtls-rprx-vision" {
		t.Errorf("Flow = %q", cfg.Flow)
	}
}

func TestParseVlessURI_InvalidScheme(t *testing.T) {
	_, err := ParseVlessURI("vmess://uuid@host.com:443")
	if err == nil {
		t.Fatal("expected error for non-vless scheme")
	}
}

func TestParseVlessURI_InvalidPort(t *testing.T) {
	_, err := ParseVlessURI("vless://uuid@host.com:abc")
	if err == nil {
		t.Fatal("expected error for non-numeric port")
	}
}

func TestParseVlessURI_MissingHost(t *testing.T) {
	_, err := ParseVlessURI("vless://uuid@:443")
	if err == nil {
		t.Fatal("expected error for missing host")
	}
}

func TestParseVlessURI_BadFragment(t *testing.T) {
	uri := "vless://uuid@host.com:443#%ZZbadencoding"
	cfg, err := ParseVlessURI(uri)
	if err != nil {
		t.Fatalf("bad fragment should not cause error: %v", err)
	}
	// Should fall back to raw fragment
	if cfg.DisplayName != "%ZZbadencoding" {
		t.Errorf("DisplayName = %q, want raw fragment", cfg.DisplayName)
	}
}

func TestParseConfigFile(t *testing.T) {
	input := `# profile-title: test
# Date/Time: 2026-03-27

vless://uuid1@host1.com:443?security=reality&sni=host1.com
vless://uuid2@host2.com:443?security=reality&sni=host2.com

# comment in the middle
not-a-valid-uri
vless://uuid3@host3.com:443
`
	configs, errs := ParseConfigFile(input)
	if len(configs) != 3 {
		t.Errorf("got %d configs, want 3", len(configs))
	}
	if len(errs) != 1 {
		t.Errorf("got %d errors, want 1", len(errs))
	}
	if configs[0].Host != "host1.com" {
		t.Errorf("first config host = %q", configs[0].Host)
	}
}

func TestParseConfigFile_Empty(t *testing.T) {
	configs, errs := ParseConfigFile("")
	if len(configs) != 0 {
		t.Errorf("got %d configs, want 0", len(configs))
	}
	if len(errs) != 0 {
		t.Errorf("got %d errors, want 0", len(errs))
	}
}

func TestParseConfigFile_OnlyComments(t *testing.T) {
	input := "# comment\n# another\n\n"
	configs, errs := ParseConfigFile(input)
	if len(configs) != 0 {
		t.Errorf("got %d configs, want 0", len(configs))
	}
	if len(errs) != 0 {
		t.Errorf("got %d errors, want 0", len(errs))
	}
}
