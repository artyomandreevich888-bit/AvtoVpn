// Package mobile provides a gomobile-compatible API for AutoVPN.
// Android/iOS apps call Start/Stop and implement the callback interfaces.
package mobile

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/include"
	sbLog "github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	json "github.com/sagernet/sing/common/json"
	"github.com/sagernet/sing/service"

	"github.com/mewmewmemw/autovpn/internal/config"
	"github.com/mewmewmemw/autovpn/internal/engine"
)

// VPN state constants for StatusListener.OnStatusChanged
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
	// Protect marks a socket as protected from VPN routing (prevents loops).
	Protect(fd int32) bool
}

var (
	mu       sync.Mutex
	inst     *box.Box
	cancelFn context.CancelFunc
	svc      VPNService
	lsnr     StatusListener
	clash    *engine.ClashAPIClient
)

func notify(state int, server string, delay, alive, total int, errMsg string) {
	if lsnr != nil {
		lsnr.OnStatusChanged(state, server, delay, alive, total, errMsg)
	}
}

// Prepare fetches configs and builds sing-box JSON.
// Call BEFORE establishing the TUN (network is still open).
// Returns the config JSON and server count.
func Prepare(cacheDir string, listener StatusListener) ([]byte, error) {
	lsnr = listener

	log.Println("[autovpn] Prepare: fetching configs, cacheDir=", cacheDir)
	notify(StateFetching, "", 0, 0, 0, "")
	fetcher := &config.Fetcher{
		CacheDir: cacheDir,
		Client:   &http.Client{Timeout: 10 * time.Second},
	}
	configs, err := fetcher.Fetch(context.Background())
	if err != nil {
		log.Println("[autovpn] Prepare: fetch failed:", err)
		notify(StateError, "", 0, 0, 0, err.Error())
		return nil, err
	}
	log.Printf("[autovpn] Prepare: got %d configs", len(configs))

	configJSON, err := buildMobileConfig(configs, cacheDir)
	if err != nil {
		log.Println("[autovpn] Prepare: build config failed:", err)
		notify(StateError, "", 0, 0, 0, err.Error())
		return nil, err
	}

	preparedServerCount = len(configs)
	log.Printf("[autovpn] Prepare: OK, %d servers, %d bytes config", len(configs), len(configJSON))
	return configJSON, nil
}

var preparedServerCount int

// Start launches sing-box with a pre-built config.
// Call AFTER Prepare and VpnService.Builder.establish().
func Start(tunFd int32, configJSON []byte, netIf string, netIfIndex int32, vpnService VPNService, listener StatusListener) error {
	mu.Lock()
	defer mu.Unlock()

	if inst != nil {
		return fmt.Errorf("already running")
	}

	svc = vpnService
	lsnr = listener

	log.Printf("[autovpn] Start: tunFd=%d, configLen=%d", tunFd, len(configJSON))
	notify(StateStarting, "", 0, 0, 0, "")

	// Create context with protocol registries + platform interface
	ctx, cancel := context.WithCancel(context.Background())
	ctx = include.Context(ctx)
	ctx = service.ContextWith[adapter.PlatformInterface](ctx, &androidPlatform{
		vpn:        vpnService,
		tunFd:      int(tunFd),
		netIfName:  netIf,
		netIfIndex: int(netIfIndex),
	})

	var opts option.Options
	if err := json.UnmarshalContext(ctx, configJSON, &opts); err != nil {
		cancel()
		notify(StateError, "", 0, 0, 0, err.Error())
		return err
	}

	instance, err := box.New(box.Options{
		Context:           ctx,
		Options:           opts,
		PlatformLogWriter: &logcatWriter{},
	})
	if err != nil {
		cancel()
		notify(StateError, "", 0, 0, 0, err.Error())
		return err
	}

	log.Println("[autovpn] Start: instance.Start()...")
	if err := instance.Start(); err != nil {
		log.Println("[autovpn] Start: FAILED:", err)
		instance.Close()
		cancel()
		notify(StateError, "", 0, 0, 0, err.Error())
		return err
	}

	inst = instance
	cancelFn = cancel
	clash = &engine.ClashAPIClient{Secret: "autovpn"}

	log.Printf("[autovpn] Start: CONNECTED, %d servers", preparedServerCount)
	notify(StateConnected, "", 0, 0, preparedServerCount, "")
	go pollStatus(ctx)

	return nil
}

// Stop disconnects the VPN.
func Stop() error {
	mu.Lock()
	defer mu.Unlock()

	if inst == nil {
		return nil
	}

	cancelFn()
	err := inst.Close()
	inst = nil
	cancelFn = nil

	notify(StateDisconnected, "", 0, 0, 0, "")
	return err
}

// IsRunning returns true if VPN is connected.
func IsRunning() bool {
	mu.Lock()
	defer mu.Unlock()
	return inst != nil
}

func pollStatus(ctx context.Context) {
	select {
	case <-time.After(3 * time.Second):
	case <-ctx.Done():
		return
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ss, err := clash.GetStatus(ctx)
			if err != nil {
				continue
			}
			notify(StateConnected, ss.CurrentServer, ss.CurrentDelay, ss.AliveCount, ss.TotalCount, "")
		}
	}
}

// logcatWriter forwards sing-box logs to Go's log (→ Android logcat)
type logcatWriter struct{}

func (w *logcatWriter) WriteMessage(level sbLog.Level, message string) {
	log.Printf("[sing-box] %s %s", level, message)
}
