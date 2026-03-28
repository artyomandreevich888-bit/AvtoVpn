// Package mobile provides a gomobile-compatible API for AutoVPN.
// Android/iOS apps call Prepare/Start/Stop and implement the callback interfaces.
//
// This is a thin adapter over internal/app.Manager — all business logic
// lives in the shared core, not here.
package mobile

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mewmewmemw/autovpn/internal/app"
	"github.com/mewmewmemw/autovpn/internal/config"
)

// VPN state constants for StatusListener.OnStatusChanged.
const (
	StateDisconnected = 0
	StateFetching     = 1
	StateStarting     = 2
	StateConnected    = 3
	StateError        = 4
)

// StatusListener receives VPN status updates. Implement in Kotlin/Swift.
type StatusListener interface {
	OnStatusChanged(state int, server string, delayMs int, aliveCount int, totalCount int, errorMsg string)
}

// VPNService provides platform VPN operations. Implement in Kotlin/Swift.
type VPNService interface {
	Protect(fd int32) bool
}

// TestSkipPreValidate disables TCP pre-validation in tests with fake hostnames.
var TestSkipPreValidate bool

var (
	mu       sync.Mutex
	mgr      *app.Manager
	cancelFn context.CancelFunc
	lsnr     StatusListener

	// preparedConfig holds config JSON between Prepare() and Start().
	preparedConfig      []byte
	preparedServerCount int
	preparedAliveCount  int
	preparedTotalCount  int
	preparedSource      string // "network", "cache", "embedded"
	preparedCacheAge    int64  // seconds
	prepared            bool
)

func setListener(l StatusListener) {
	// caller must hold mu
	lsnr = l
}

func notify(state int, server string, delay, alive, total int, errMsg string) {
	log.Printf("[autovpn] notify: state=%d server=%q alive=%d total=%d err=%q", state, server, alive, total, errMsg)
	// read lsnr under lock to avoid race
	mu.Lock()
	l := lsnr
	mu.Unlock()
	if l != nil {
		l.OnStatusChanged(state, server, delay, alive, total, errMsg)
	}
}

func stateToInt(s app.State) int {
	switch s {
	case app.StateDisconnected:
		return StateDisconnected
	case app.StateFetching:
		return StateFetching
	case app.StateStarting:
		return StateStarting
	case app.StateConnected:
		return StateConnected
	case app.StateError:
		return StateError
	default:
		return StateDisconnected
	}
}

// Prepare fetches configs and builds sing-box JSON.
// Call BEFORE establishing the TUN (network is still open).
// Returns the config JSON (already patched for mobile).
func Prepare(cacheDir string, listener StatusListener) ([]byte, error) {
	mu.Lock()

	if mgr != nil && mgr.Engine.IsRunning() {
		mu.Unlock()
		return nil, errAlreadyRunning
	}

	setListener(listener)
	prepared = false

	fetcher := &config.Fetcher{
		CacheDir: cacheDir,
		Client:   &http.Client{Timeout: 10 * time.Second},
	}
	fetcher.OnProgress = func(current, total, servers int) {
		notify(StateFetching, fmt.Sprintf("Source %d/%d (%d servers)", current, total, servers), 0, 0, 0, "")
	}
	mgr = app.NewManager(fetcher)
	mgr.SkipPreValidate = TestSkipPreValidate
	mgr.OnChange(func(s app.Status) {
		notify(stateToInt(s.State), s.Server, s.Delay, s.AliveCount, s.TotalCount, s.Error)
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancelFn = cancel
	mgr.SetCancel(cancel)

	// Unlock during network I/O to allow Stop() from another goroutine.
	mu.Unlock()
	meta, configJSON, count, err := mgr.PrepareConfigWithMeta(ctx)
	mu.Lock()

	if err != nil {
		cancelFn = nil
		mu.Unlock()
		return nil, err
	}

	patched, err := PatchMobileConfig(configJSON, cacheDir)
	if err != nil {
		cancelFn = nil
		mu.Unlock()
		return nil, err
	}

	preparedConfig = patched
	preparedServerCount = count
	preparedAliveCount = meta.AliveCount
	preparedTotalCount = meta.TotalCount
	preparedSource = meta.Source
	preparedCacheAge = meta.CacheAge
	prepared = true

	log.Printf("[autovpn] Prepare: OK, %d/%d alive servers, %d bytes config", meta.AliveCount, meta.TotalCount, len(patched))
	mu.Unlock()
	return patched, nil
}

// Start launches sing-box with a pre-built config.
// Call AFTER Prepare() and VpnService.Builder.establish().
func Start(tunFd int32, configJSON []byte, netIf string, netIfIndex int32, vpnService VPNService, listener StatusListener) error {
	mu.Lock()

	if mgr == nil {
		mu.Unlock()
		return errNotPrepared
	}
	if mgr.Engine.IsRunning() {
		mu.Unlock()
		return errAlreadyRunning
	}

	setListener(listener)

	mgr.Engine.Platform = &androidPlatformProvider{
		vpn:        vpnService,
		tunFd:      int(tunFd),
		netIfName:  netIf,
		netIfIndex: int(netIfIndex),
	}

	ctx := context.Background()
	if cancelFn != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		oldCancel := cancelFn
		cancelFn = func() { cancel(); oldCancel() }
		mgr.SetCancel(cancelFn)
	}

	count := preparedServerCount
	if configJSON == nil {
		configJSON = preparedConfig
	}

	// Unlock during engine start (can take time).
	mu.Unlock()
	log.Printf("[autovpn] Start: calling StartEngine, %d servers, %d bytes config", count, len(configJSON))
	err := mgr.StartEngine(ctx, configJSON, count)
	if err != nil {
		log.Printf("[autovpn] Start: engine FAILED: %v", err)
		return err
	}

	log.Printf("[autovpn] Start: CONNECTED, %d servers", count)
	return nil
}

// Stop disconnects the VPN. Safe to call multiple times.
func Stop() error {
	mu.Lock()
	m := mgr
	if m == nil {
		mu.Unlock()
		return nil
	}
	prepared = false
	preparedConfig = nil
	mu.Unlock()

	// Disconnect outside lock — callbacks (notify) also take mu.
	return m.Disconnect()
}

// IsRunning returns true if VPN is connected.
func IsRunning() bool {
	mu.Lock()
	defer mu.Unlock()
	return mgr != nil && mgr.Engine.IsRunning()
}

// CheckServices tests connectivity and auto-rotates servers if YouTube is slow.
// Returns empty string on success, error description on failure.
func CheckServices() string {
	mu.Lock()
	m := mgr
	mu.Unlock()
	if m == nil {
		return "not connected"
	}
	checks := m.CheckServices(context.Background())
	for _, c := range checks {
		if c.Name == "YouTube" {
			if c.Status == "ok" {
				return ""
			}
			return "YouTube: " + c.Status
		}
	}
	return ""
}

// GetConfigInfo returns config source metadata as "source,aliveCount,totalCount,cacheAgeSec".
// source is "network", "cache", or "embedded".
func GetConfigInfo() string {
	mu.Lock()
	defer mu.Unlock()
	return fmt.Sprintf("%s,%d,%d,%d", preparedSource, preparedAliveCount, preparedTotalCount, preparedCacheAge)
}

// GetServerList returns per-server status as lines: "name,delay_ms,alive,active\n..."
// alive/active are 0 or 1.
func GetServerList() string {
	mu.Lock()
	m := mgr
	mu.Unlock()
	if m == nil || m.ClashAPI == nil {
		return ""
	}
	ss, err := m.ClashAPI.GetStatus(context.Background())
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, s := range ss.Servers {
		alive, active := 0, 0
		if s.Alive {
			alive = 1
		}
		if s.Active {
			active = 1
		}
		fmt.Fprintf(&b, "%s,%d,%d,%d\n", s.Name, s.Delay, alive, active)
	}
	return b.String()
}

// GetTraffic returns current upload/download speed in bytes/sec as "up,down".
func GetTraffic() string {
	mu.Lock()
	m := mgr
	mu.Unlock()
	if m == nil || m.ClashAPI == nil {
		return "0,0"
	}
	snap, err := m.ClashAPI.GetTraffic(context.Background())
	if err != nil {
		return "0,0"
	}
	return fmt.Sprintf("%d,%d", snap.Up, snap.Down)
}

// GetExternalIP fetches the external IP address via a public API.
func GetExternalIP() string {
	return getExternalIPWith(&http.Client{Timeout: 5 * time.Second})
}

// ipCheckURL is the endpoint used to determine external IP. Overridable in tests.
var ipCheckURL = "https://api.ipify.org"

func getExternalIPWith(client *http.Client) string {
	resp, err := client.Get(ipCheckURL)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(body))
}

// ValidateServers tests ALL servers in parallel via Clash API.
// concurrency = number of parallel tests (10-50 recommended).
// Returns "alive,dead,total,bestServer,bestDelay" on first line,
// then per-server: "name,delay,alive\n" for each.
func ValidateServers(concurrency int) string {
	mu.Lock()
	m := mgr
	mu.Unlock()
	if m == nil || m.ClashAPI == nil {
		return "0,0,0,,"
	}

	results, err := m.ClashAPI.ValidateAllProxies(
		context.Background(),
		concurrency,
		"http://1.1.1.1/cdn-cgi/trace",
		3000,
	)
	if err != nil {
		return "0,0,0,,"
	}

	var alive, dead int
	bestName := ""
	bestDelay := 999999
	var b strings.Builder

	for _, r := range results {
		if r.Alive {
			alive++
			if r.Delay < bestDelay {
				bestDelay = r.Delay
				bestName = r.Name
			}
		} else {
			dead++
		}
	}
	total := alive + dead
	if bestDelay == 999999 {
		bestDelay = 0
	}

	fmt.Fprintf(&b, "%d,%d,%d,%s,%d\n", alive, dead, total, bestName, bestDelay)
	for _, r := range results {
		a := 0
		if r.Alive {
			a = 1
		}
		fmt.Fprintf(&b, "%s,%d,%d\n", r.Name, r.Delay, a)
	}
	return b.String()
}

// VerifyConnection checks that VPN is working by fetching external IP.
// Servers are already pre-validated during Prepare (before TUN).
// Returns "ip" on success, empty on failure.
func VerifyConnection(concurrency int) string {
	log.Println("[autovpn] VerifyConnection: checking external IP")
	notify(StateConnected, "Verifying IP...", 0, 0, 0, "")

	client := &http.Client{Timeout: 10 * time.Second}
	ip := getExternalIPWith(client)
	if ip == "" {
		log.Println("[autovpn] VerifyConnection: first IP check failed, retrying")
		time.Sleep(2 * time.Second)
		ip = getExternalIPWith(client)
	}
	if ip == "" {
		log.Println("[autovpn] VerifyConnection: IP check failed")
		return ""
	}

	log.Printf("[autovpn] VerifyConnection: IP = %s", ip)
	return ip
}

var (
	errAlreadyRunning = stringError("already running")
	errNotPrepared    = stringError("call Prepare() first")
)

type stringError string

func (e stringError) Error() string { return string(e) }
