package mobile

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	box "github.com/sagernet/sing-box"
	sbLog "github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	tun "github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/control"

	"github.com/mewmewmemw/autovpn/internal/app"
	"github.com/mewmewmemw/autovpn/internal/engine"
)

// --- androidPlatformProvider ---

func TestPlatformProvider_SetupContext(t *testing.T) {
	p := &androidPlatformProvider{
		vpn:        &mockVPNService{},
		tunFd:      42,
		netIfName:  "wlan0",
		netIfIndex: 3,
	}
	ctx := p.SetupContext(context.Background())
	if ctx == nil {
		t.Fatal("SetupContext returned nil")
	}
	// Context should have PlatformInterface injected
	// (can't easily check without sing-box internals, but no panic = pass)
}

func TestPlatformProvider_BoxOptions(t *testing.T) {
	p := &androidPlatformProvider{}
	opts := p.BoxOptions(box.Options{})
	if opts.PlatformLogWriter == nil {
		t.Error("PlatformLogWriter should be set")
	}
}

// --- androidPlatform ---

func TestAndroidPlatform_Initialize(t *testing.T) {
	p := &androidPlatform{}
	err := p.Initialize(nil)
	if err != nil {
		t.Errorf("Initialize: %v", err)
	}
}

func TestAndroidPlatform_AutoDetectInterfaceControl(t *testing.T) {
	p := &androidPlatform{vpn: &mockVPNService{}}
	err := p.AutoDetectInterfaceControl(42)
	if err != nil {
		t.Errorf("Protect should succeed: %v", err)
	}
}

func TestAndroidPlatform_AutoDetectInterfaceControl_NilVPN(t *testing.T) {
	p := &androidPlatform{vpn: nil}
	err := p.AutoDetectInterfaceControl(42)
	if err == nil {
		t.Error("should fail with nil vpn")
	}
}

func TestAndroidPlatform_AutoDetectInterfaceControl_ProtectFails(t *testing.T) {
	p := &androidPlatform{vpn: &failingVPNService{}}
	err := p.AutoDetectInterfaceControl(42)
	if err == nil {
		t.Error("should fail when Protect returns false")
	}
}

func TestAndroidPlatform_BooleanMethods(t *testing.T) {
	p := &androidPlatform{}
	if !p.UsePlatformAutoDetectInterfaceControl() {
		t.Error("UsePlatformAutoDetectInterfaceControl should be true")
	}
	if !p.UsePlatformInterface() {
		t.Error("UsePlatformInterface should be true")
	}
	if !p.UsePlatformDefaultInterfaceMonitor() {
		t.Error("UsePlatformDefaultInterfaceMonitor should be true")
	}
	if !p.UsePlatformNetworkInterfaces() {
		t.Error("UsePlatformNetworkInterfaces should be true — sing-box needs it for selectInterfaces")
	}
	if p.UnderNetworkExtension() {
		t.Error("UnderNetworkExtension should be false")
	}
	if p.NetworkExtensionIncludeAllNetworks() {
		t.Error("NetworkExtensionIncludeAllNetworks should be false")
	}
	if p.UsePlatformWIFIMonitor() {
		t.Error("UsePlatformWIFIMonitor should be false")
	}
	if p.UsePlatformConnectionOwnerFinder() {
		t.Error("UsePlatformConnectionOwnerFinder should be false")
	}
	if p.UsePlatformNotification() {
		t.Error("UsePlatformNotification should be false")
	}
}

func TestAndroidPlatform_NoOpMethods(t *testing.T) {
	p := &androidPlatform{}

	p.ClearDNSCache() // no panic

	if err := p.RequestPermissionForWIFIState(); err != nil {
		t.Errorf("RequestPermissionForWIFIState: %v", err)
	}

	ws := p.ReadWIFIState()
	_ = ws // just verify no panic

	certs := p.SystemCertificates()
	if certs != nil {
		t.Error("SystemCertificates should be nil")
	}

	ifaces, err := p.NetworkInterfaces()
	if ifaces != nil || err != nil {
		t.Errorf("NetworkInterfaces: %v, %v", ifaces, err)
	}

	_, err = p.FindConnectionOwner(nil)
	if err == nil {
		t.Error("FindConnectionOwner should error")
	}

	err = p.SendNotification(nil)
	if err != nil {
		t.Errorf("SendNotification: %v", err)
	}
}

func TestAndroidPlatform_CreateDefaultInterfaceMonitor(t *testing.T) {
	p := &androidPlatform{netIfName: "wlan0", netIfIndex: 5}
	mon := p.CreateDefaultInterfaceMonitor(nil)
	if mon == nil {
		t.Fatal("monitor is nil")
	}
}

// --- androidInterfaceMonitor ---

func TestInterfaceMonitor_StartAndDefault(t *testing.T) {
	p := &androidPlatform{netIfName: "wlan0", netIfIndex: 5}
	mon := p.CreateDefaultInterfaceMonitor(nil).(*androidInterfaceMonitor)

	err := mon.Start()
	if err != nil {
		t.Fatal(err)
	}

	iface := mon.DefaultInterface()
	if iface == nil {
		t.Fatal("DefaultInterface is nil after Start")
	}
	if iface.Name != "wlan0" {
		t.Errorf("Name = %q, want wlan0", iface.Name)
	}
	if iface.Index != 5 {
		t.Errorf("Index = %d, want 5", iface.Index)
	}
}

func TestInterfaceMonitor_StartNoInterface(t *testing.T) {
	p := &androidPlatform{netIfName: "", netIfIndex: 0}
	mon := p.CreateDefaultInterfaceMonitor(nil).(*androidInterfaceMonitor)

	err := mon.Start()
	if err != nil {
		t.Fatal(err)
	}
	if mon.DefaultInterface() != nil {
		t.Error("should be nil when no interface provided")
	}
}

func TestInterfaceMonitor_Close(t *testing.T) {
	mon := &androidInterfaceMonitor{}
	if err := mon.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestInterfaceMonitor_RegisterMyInterface(t *testing.T) {
	mon := &androidInterfaceMonitor{}
	mon.RegisterMyInterface("tun0")
	if mon.MyInterface() != "tun0" {
		t.Errorf("MyInterface = %q, want tun0", mon.MyInterface())
	}
}

func TestInterfaceMonitor_Callbacks(t *testing.T) {
	mon := &androidInterfaceMonitor{}
	elem := mon.RegisterCallback(func(iface *control.Interface, flags int) {})
	if elem == nil {
		t.Fatal("RegisterCallback returned nil")
	}
	mon.UnregisterCallback(elem)
}

func TestInterfaceMonitor_VPNFlags(t *testing.T) {
	mon := &androidInterfaceMonitor{}
	if mon.OverrideAndroidVPN() {
		t.Error("OverrideAndroidVPN should be false")
	}
	if !mon.AndroidVPNEnabled() {
		t.Error("AndroidVPNEnabled should be true")
	}
}

// --- logcatWriter ---

func TestLogcatWriter(t *testing.T) {
	w := &logcatWriter{}
	// Should not panic with any log level
	w.WriteMessage(sbLog.LevelInfo, "test message")
	w.WriteMessage(sbLog.LevelError, "error message")
	w.WriteMessage(sbLog.LevelDebug, "debug message")
}

// --- stateToInt ---

func TestStateToInt(t *testing.T) {
	tests := []struct {
		state app.State
		want  int
	}{
		{app.StateDisconnected, StateDisconnected},
		{app.StateFetching, StateFetching},
		{app.StateStarting, StateStarting},
		{app.StateConnected, StateConnected},
		{app.StateError, StateError},
		{app.State("unknown"), StateDisconnected},
	}
	for _, tt := range tests {
		if got := stateToInt(tt.state); got != tt.want {
			t.Errorf("stateToInt(%q) = %d, want %d", tt.state, got, tt.want)
		}
	}
}

// --- getExternalIPWith ---

func TestGetExternalIPWith_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "  1.2.3.4  \n")
	}))
	defer srv.Close()

	old := ipCheckURL
	ipCheckURL = srv.URL
	defer func() { ipCheckURL = old }()

	ip := getExternalIPWith(srv.Client())
	if ip != "1.2.3.4" {
		t.Errorf("ip = %q, want 1.2.3.4", ip)
	}
}

func TestGetExternalIPWith_ServerDown(t *testing.T) {
	old := ipCheckURL
	ipCheckURL = "http://127.0.0.1:1" // nothing listens
	defer func() { ipCheckURL = old }()

	ip := getExternalIPWith(&http.Client{Timeout: 100 * time.Millisecond})
	if ip != "" {
		t.Errorf("ip = %q, want empty", ip)
	}
}

func TestGetExternalIPWith_EmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "   \n  ")
	}))
	defer srv.Close()

	old := ipCheckURL
	ipCheckURL = srv.URL
	defer func() { ipCheckURL = old }()

	ip := getExternalIPWith(srv.Client())
	if ip != "" {
		t.Errorf("ip = %q, want empty (whitespace only)", ip)
	}
}

func TestVerifyConnection_MockedIPAndClash(t *testing.T) {
	// Mock IP API
	ipSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "5.6.7.8")
	}))
	defer ipSrv.Close()

	old := ipCheckURL
	ipCheckURL = ipSrv.URL
	defer func() { ipCheckURL = old }()

	// Mock Clash API
	clashSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/proxies" {
			json.NewEncoder(w).Encode(map[string]any{
				"proxies": map[string]any{
					"proxy":     map[string]any{"now": "srv-0", "all": []string{"srv-0"}},
					"srv-0":    map[string]any{"name": "srv-0"},
				},
			})
			return
		}
		if strings.Contains(r.URL.Path, "/delay") {
			json.NewEncoder(w).Encode(map[string]int{"delay": 77})
			return
		}
		w.WriteHeader(204)
	}))
	defer clashSrv.Close()

	reset()
	mu.Lock()
	mgr = &app.Manager{
		ClashAPI: &engine.ClashAPIClient{BaseURL: clashSrv.URL},
		Engine:   &engine.Engine{},
	}
	mu.Unlock()

	result := VerifyConnection(5)
	if result == "" {
		t.Fatal("VerifyConnection should succeed with mocked IP and Clash")
	}
	// VerifyConnection now returns just "ip".
	if result != "5.6.7.8" {
		t.Errorf("result = %q, want 5.6.7.8", result)
	}
}

func TestVerifyConnection_IPUnreachable(t *testing.T) {
	reset()
	// Point IP check at unreachable endpoint to simulate no connectivity.
	old := ipCheckURL
	ipCheckURL = "http://127.0.0.1:1"
	defer func() { ipCheckURL = old }()

	result := VerifyConnection(5)
	if result != "" {
		t.Errorf("should be empty when IP check fails, got %q", result)
	}
}

// --- OpenInterface mock test ---

func TestOpenInterface_MockedSyscalls(t *testing.T) {
	// Save originals
	origDup := dupFd
	origTunName := tunNameFromFd
	defer func() { dupFd = origDup; tunNameFromFd = origTunName }()

	// Mock
	dupFd = func(fd int) (int, error) { return 99, nil }
	tunNameFromFd = func(fd int) (string, error) { return "tun7", nil }

	p := &androidPlatform{tunFd: 42, vpn: &mockVPNService{}}
	opts := &tun.Options{}

	// OpenInterface will call tun.New which will fail (no real TUN),
	// but we verify our mocks were called correctly.
	_, err := p.OpenInterface(opts, option.TunPlatformOptions{})
	// tun.New will fail — that's expected
	_ = err

	if opts.Name != "tun7" {
		t.Errorf("Name = %q, want tun7", opts.Name)
	}
	if opts.FileDescriptor != 99 {
		t.Errorf("FileDescriptor = %d, want 99", opts.FileDescriptor)
	}
}

func TestOpenInterface_DupFails(t *testing.T) {
	origDup := dupFd
	origTunName := tunNameFromFd
	defer func() { dupFd = origDup; tunNameFromFd = origTunName }()

	dupFd = func(fd int) (int, error) { return 0, fmt.Errorf("dup failed") }
	tunNameFromFd = func(fd int) (string, error) { return "tun0", nil }

	p := &androidPlatform{tunFd: 42}
	_, err := p.OpenInterface(&tun.Options{}, option.TunPlatformOptions{})
	if err == nil {
		t.Fatal("should fail when dup fails")
	}
}

func TestOpenInterface_TunNameFails_FallsBackToTun0(t *testing.T) {
	origDup := dupFd
	origTunName := tunNameFromFd
	defer func() { dupFd = origDup; tunNameFromFd = origTunName }()

	dupFd = func(fd int) (int, error) { return 99, nil }
	tunNameFromFd = func(fd int) (string, error) { return "", fmt.Errorf("ioctl failed") }

	p := &androidPlatform{tunFd: 42}
	opts := &tun.Options{}
	_, _ = p.OpenInterface(opts, option.TunPlatformOptions{})

	if opts.Name != "tun0" {
		t.Errorf("should fallback to tun0, got %q", opts.Name)
	}
}

// --- mock types ---

type failingVPNService struct{}

func (f *failingVPNService) Protect(fd int32) bool { return false }
