package app

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/mewmewmemw/autovpn/internal/config"
	"github.com/mewmewmemw/autovpn/internal/engine"
)

type State string

const (
	StateDisconnected State = "disconnected"
	StateFetching     State = "fetching"
	StateStarting     State = "starting"
	StateConnected    State = "connected"
	StateError        State = "error"
)

type Status struct {
	State      State
	Server     string
	Delay      int
	AliveCount int
	TotalCount int
	Error      string
}

type Manager struct {
	Engine   *engine.Engine
	Fetcher  *config.Fetcher
	ClashAPI *engine.ClashAPIClient

	// SkipPreValidate disables TCP pre-validation of servers.
	// Used in tests with fake hostnames.
	SkipPreValidate bool

	mu          sync.RWMutex
	status      Status
	cancel      context.CancelFunc
	onChange    func(Status)
	serverNames  map[string]string
	killSwitch   bool
}

func NewManager(fetcher *config.Fetcher) *Manager {
	return &Manager{
		Engine:  &engine.Engine{},
		Fetcher: fetcher,
		ClashAPI: &engine.ClashAPIClient{
			Secret: "autovpn",
		},
		status:     Status{State: StateDisconnected},
		killSwitch: true,
	}
}

func (m *Manager) OnChange(fn func(Status)) {
	m.mu.Lock()
	m.onChange = fn
	m.mu.Unlock()
}

func (m *Manager) Status() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

func (m *Manager) setStatus(s Status) {
	m.mu.Lock()
	m.status = s
	fn := m.onChange
	m.mu.Unlock()
	if fn != nil {
		fn(s)
	}
}

// SetCancel registers an external cancel function for the connection lifecycle.
// Used by mobile to control the lifecycle from outside.
func (m *Manager) SetCancel(cancel context.CancelFunc) {
	m.mu.Lock()
	m.cancel = cancel
	m.mu.Unlock()
}

// ConfigMeta holds info about where configs came from.
type ConfigMeta struct {
	Source     string // "network", "cache", "embedded"
	CacheAge  int64  // seconds since cache was written
	AliveCount int   // servers that passed pre-validation
	TotalCount int   // total servers before filtering
}

// PrepareConfig fetches servers and builds sing-box JSON config.
// Mobile calls this before creating the TUN (network still open).
func (m *Manager) PrepareConfig(ctx context.Context) ([]byte, int, error) {
	_, configJSON, count, err := m.PrepareConfigWithMeta(ctx)
	return configJSON, count, err
}

// PrepareConfigWithMeta fetches servers, pre-validates via TCP, filters to alive,
// and builds sing-box config with only working servers (sorted by RTT).
// Runs BEFORE TUN — network is open, no kill switch.
func (m *Manager) PrepareConfigWithMeta(ctx context.Context) (*ConfigMeta, []byte, int, error) {
	m.setStatus(Status{State: StateFetching})

	result, err := m.Fetcher.FetchWithMeta(ctx)
	if err != nil {
		m.setStatus(Status{State: StateError, Error: err.Error()})
		return nil, nil, 0, err
	}

	totalCount := len(result.Configs)
	var aliveConfigs []config.VlessConfig

	if !m.SkipPreValidate {
		// Pre-validate: TCP connect to each server (network still open, no TUN).
		m.setStatus(Status{
			State:  StateFetching,
			Server: fmt.Sprintf("Testing %d servers...", totalCount),
		})
		alive := config.PreValidate(ctx, result.Configs, 30, 3*time.Second, func(done, total, aliveCount int) {
			m.setStatus(Status{
				State:      StateFetching,
				Server:     fmt.Sprintf("Testing %d/%d (%d alive)", done, total, aliveCount),
				AliveCount: aliveCount,
				TotalCount: total,
			})
		})

		if len(alive) == 0 {
			// TCP validation blocked by ISP (common in Russia) — skip filtering, use all servers
			aliveConfigs = result.Configs
		} else {
			aliveConfigs = make([]config.VlessConfig, len(alive))
			for i, v := range alive {
				aliveConfigs[i] = v.Config
			}
		}
	} else {
		aliveConfigs = result.Configs
	}

	m.mu.RLock()
	ks := m.killSwitch
	m.mu.RUnlock()
	configJSON, err := config.BuildConfig(aliveConfigs, ks)
	if err != nil {
		m.setStatus(Status{State: StateError, Error: err.Error()})
		return nil, nil, 0, err
	}

	m.mu.Lock()
	m.serverNames = config.ServerNamesForConfigs(aliveConfigs)
	m.mu.Unlock()

	meta := &ConfigMeta{
		Source:     string(result.Source),
		CacheAge:  result.CacheAge,
		AliveCount: len(aliveConfigs),
		TotalCount: totalCount,
	}
	return meta, configJSON, len(aliveConfigs), nil
}

// StartEngine starts sing-box with a pre-built config.
// Non-blocking: sets StateStarting, launches engine in background,
// sends StateConnected or StateError when done.
func (m *Manager) StartEngine(ctx context.Context, configJSON []byte, serverCount int) error {
	m.setStatus(Status{State: StateStarting, Server: fmt.Sprintf("Initializing %d servers...", serverCount)})

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			m.setStatus(Status{State: StateStarting, Server: fmt.Sprintf("Очистка интерфейса, попытка %d/3...", attempt+1)})
			exec.Command("netsh", "interface", "ip", "delete", "address", "name=tun0", "addr=172.19.0.1").Run()
			exec.Command("netsh", "interface", "delete", "interface", "name=tun0").Run()
			time.Sleep(time.Second)
		}

		done := make(chan error, 1)
		go func() { done <- m.Engine.Start(configJSON) }()

		select {
		case lastErr = <-done:
		case <-ctx.Done():
			return ctx.Err()
		}

		if lastErr == nil {
			break
		}
		if !strings.Contains(lastErr.Error(), "already exists") {
			m.setStatus(Status{State: StateError, Error: lastErr.Error()})
			return lastErr
		}
	}

	if lastErr != nil {
		m.setStatus(Status{State: StateError, Error: lastErr.Error()})
		return lastErr
	}

	m.setStatus(Status{
		State:      StateConnected,
		TotalCount: serverCount,
	})

	go m.pollStatus(ctx)
	return nil
}




// quickSelectBest tests all servers with high concurrency and short timeout,
// then switches to the fastest one. Designed to complete in ~2-3 seconds.
func (m *Manager) quickSelectBest(ctx context.Context) {
	results, err := m.ClashAPI.ValidateAllProxies(ctx, 50, "https://www.gstatic.com/generate_204", 1500)
	if err != nil {
		return
	}

	bestDelay := 0
	bestName := ""
	for _, r := range results {
		if r.Alive && (bestDelay == 0 || r.Delay < bestDelay) {
			bestDelay = r.Delay
			bestName = r.Name
		}
	}

	if bestName != "" {
		m.ClashAPI.SelectProxy(ctx, "proxy", bestName)
	}
}

// ServerListItem holds display info for a single server.
type ServerListItem struct {
	Tag    string // sing-box proxy tag (e.g. server-0)
	Name   string // display name
	Delay  int
	Active bool
	Alive  bool
}

// GetServerList returns current server list with delays from ClashAPI.
func (m *Manager) GetServerList(ctx context.Context) []ServerListItem {
	m.mu.RLock()
	names := m.serverNames
	m.mu.RUnlock()

	ss, err := m.ClashAPI.GetStatus(ctx)
	if err != nil || ss == nil {
		return nil
	}

	items := make([]ServerListItem, 0, len(ss.Servers))
	for _, s := range ss.Servers {
		name := s.Name
		if names != nil {
			if n, ok := names[s.Name]; ok {
				name = n
			}
		}
		items = append(items, ServerListItem{
			Tag:    s.Name,
			Name:   name,
			Delay:  s.Delay,
			Active: s.Active,
			Alive:  s.Alive,
		})
	}
	return items
}


// SetKillSwitch enables or disables kill switch (strict routing).
func (m *Manager) SetKillSwitch(enabled bool) {
	m.mu.Lock()
	m.killSwitch = enabled
	m.mu.Unlock()
}

// GetKillSwitch returns current kill switch state.
func (m *Manager) GetKillSwitch() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.killSwitch
}

// SelectServer switches the active proxy to the given server tag.
func (m *Manager) SelectServer(ctx context.Context, tag string) error {
	return m.ClashAPI.SelectProxy(ctx, "proxy", tag)
}

// killConflictingVPNs stops known VPN apps that may hold the TUN interface.
func killConflictingVPNs() {
	conflicts := []string{"happ.exe", "nekobox.exe", "nekoray.exe", "clash.exe", "v2ray.exe", "xray.exe", "mihomo.exe"}
	for _, proc := range conflicts {
		exec.Command("taskkill", "/IM", proc, "/F").Run()
	}
	time.Sleep(500 * time.Millisecond)
}

// Connect performs the full lifecycle (prepare + start). Desktop uses this.
func (m *Manager) Connect() error {
	killConflictingVPNs()
	if m.Engine.IsRunning() {
		return fmt.Errorf("already connected")
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.SetCancel(cancel)

	configJSON, count, err := m.PrepareConfig(ctx)
	if err != nil {
		cancel()
		return err
	}

	if err := m.StartEngine(ctx, configJSON, count); err != nil {
		cancel()
		return err
	}

	return nil
}

func (m *Manager) Disconnect() error {
	m.mu.Lock()
	cancel := m.cancel
	m.cancel = nil
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	err := m.Engine.Stop()
	m.setStatus(Status{State: StateDisconnected})
	return err
}

func (m *Manager) pollStatus(ctx context.Context) {
	// Wait a moment for clash_api to come up
	select {
	case <-time.After(2 * time.Second):
	case <-ctx.Done():
		return
	}

	// Quick initial selection — complete in ~2-3s, user gets best server fast
	go m.quickSelectBest(ctx)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Periodic full re-test every 90 seconds to catch server degradation
	retestTicker := time.NewTicker(90 * time.Second)
	defer retestTicker.Stop()

	badCount := 0 // consecutive polls with high latency

	for {
		select {
		case <-ctx.Done():
			return
		case <-retestTicker.C:
			// Background re-test — find a better server if available
			go m.quickSelectBest(ctx)
		case <-ticker.C:
			ss, err := m.ClashAPI.GetStatus(ctx)
			if err != nil {
				continue
			}
			m.setStatus(Status{
				State:      StateConnected,
				Server:     ss.CurrentServer,
				Delay:      ss.CurrentDelay,
				AliveCount: ss.AliveCount,
				TotalCount: ss.TotalCount,
			})

			// Auto-reselect if current server is consistently slow
			if ss.CurrentDelay > 2500 && ss.CurrentDelay > 0 {
				badCount++
				if badCount >= 2 { // 2 bad polls in a row (~10s) — switch
					badCount = 0
					go m.quickSelectBest(ctx)
				}
			} else {
				badCount = 0
			}
		}
	}
}

// --- Smart server selection ---

type ServiceCheck struct {
	Name   string
	URL    string
	Status string // "ok", "fail", "checking"
	Delay  int    // ms
}

// ServiceDef describes a service to check.
type ServiceDef struct {
	Name string
	URL  string
}

var services = []ServiceDef{
	{"YouTube", "https://www.youtube.com"},
	{"Instagram", "https://www.instagram.com"},
	{"GitHub", "https://github.com"},
	{"Telegram", "https://telegram.org"},
}

// ExportServices returns current services list (for testing).
func ExportServices() []ServiceDef { return services }

// SetServices overrides services list (for testing).
func SetServices(s []ServiceDef) { services = s }

const (
	SlowThreshold = 3000 // ms — server considered bad if YouTube > 3s
	MaxRetries    = 5    // try up to 5 servers before giving up
)

// CheckServices tests connectivity to key services through the VPN.
// If YouTube is too slow or fails, automatically switches to the next server.
func (m *Manager) CheckServices(ctx context.Context) []ServiceCheck {
	for attempt := 0; attempt < MaxRetries; attempt++ {
		results := m.checkOnce(ctx)

		ytOK := false
		for _, r := range results {
			if r.Name == "YouTube" && r.Status == "ok" && r.Delay < SlowThreshold {
				ytOK = true
			}
		}
		if ytOK {
			return results
		}

		// YouTube bad — try next server
		ps, err := m.ClashAPI.GetProxies(ctx)
		if err != nil {
			return results
		}
		auto, ok := ps.Proxies["auto"]
		if !ok || len(auto.All) < 2 {
			return results
		}

		current := auto.Now
		nextServer := ""
		for i, name := range auto.All {
			if name == current && i+1 < len(auto.All) {
				nextServer = auto.All[i+1]
				break
			}
		}
		if nextServer == "" {
			return results
		}

		m.ClashAPI.SelectProxy(ctx, "proxy", nextServer)
		time.Sleep(2 * time.Second) // wait for new server
	}

	return m.checkOnce(ctx)
}

func (m *Manager) checkOnce(ctx context.Context) []ServiceCheck {
	proxyURL, _ := url.Parse("http://127.0.0.1:7890")
	transport := &http.Transport{
		Proxy:             http.ProxyURL(proxyURL),
		DisableKeepAlives: true,
	}
	client := &http.Client{Timeout: 10 * time.Second, Transport: transport}
	results := make([]ServiceCheck, len(services))

	var wg sync.WaitGroup
	for i, svc := range services {
		results[i] = ServiceCheck{Name: svc.Name, URL: svc.URL, Status: "checking"}
		wg.Add(1)
		go func(idx int, url string) {
			defer wg.Done()
			start := time.Now()
			req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
			if err != nil {
				results[idx].Status = "fail"
				return
			}
			resp, err := client.Do(req)
			elapsed := int(time.Since(start).Milliseconds())
			if err != nil || resp.StatusCode >= 500 {
				results[idx].Status = "fail"
				results[idx].Delay = elapsed
				return
			}
			resp.Body.Close()
			results[idx].Status = "ok"
			results[idx].Delay = elapsed
		}(i, svc.URL)
	}
	wg.Wait()
	return results
}
