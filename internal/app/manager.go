package app

import (
	"context"
	"fmt"
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
	State         State
	Server        string
	Delay         int
	AliveCount    int
	TotalCount    int
	Error         string
}

type Manager struct {
	Engine   *engine.Engine
	Fetcher  *config.Fetcher
	ClashAPI *engine.ClashAPIClient

	mu       sync.RWMutex
	status   Status
	cancel   context.CancelFunc
	onChange func(Status)
}

func NewManager(fetcher *config.Fetcher) *Manager {
	return &Manager{
		Engine:  &engine.Engine{},
		Fetcher: fetcher,
		ClashAPI: &engine.ClashAPIClient{
			Secret: "autovpn",
		},
		status: Status{State: StateDisconnected},
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

func (m *Manager) Connect() error {
	if m.Engine.IsRunning() {
		return fmt.Errorf("already connected")
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.cancel = cancel
	m.mu.Unlock()

	// Fetch configs
	m.setStatus(Status{State: StateFetching})
	configs, err := m.Fetcher.Fetch(ctx)
	if err != nil {
		cancel()
		m.setStatus(Status{State: StateError, Error: err.Error()})
		return err
	}

	// Build sing-box config
	m.setStatus(Status{State: StateStarting})
	configJSON, err := config.BuildConfig(configs)
	if err != nil {
		cancel()
		m.setStatus(Status{State: StateError, Error: err.Error()})
		return err
	}

	// Start sing-box
	if err := m.Engine.Start(configJSON); err != nil {
		cancel()
		m.setStatus(Status{State: StateError, Error: err.Error()})
		return err
	}

	m.setStatus(Status{
		State:      StateConnected,
		TotalCount: len(configs),
	})

	// Start polling clash_api in background
	go m.pollStatus(ctx)

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

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
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
		}
	}
}
