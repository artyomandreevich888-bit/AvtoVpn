package main

import (
	"context"
	"os"

	"github.com/mewmewmemw/autovpn/internal/app"
	"github.com/mewmewmemw/autovpn/internal/config"
)

// App struct is exposed to the frontend via Wails bindings.
type App struct {
	ctx     context.Context
	manager *app.Manager
}

func NewApp() *App {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = "/tmp"
	}
	cacheDir += "/autovpn"

	return &App{
		manager: app.NewManager(&config.Fetcher{
			CacheDir: cacheDir,
		}),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

type StatusResult struct {
	State      string `json:"State"`
	Server     string `json:"Server"`
	Delay      int    `json:"Delay"`
	AliveCount int    `json:"AliveCount"`
	TotalCount int    `json:"TotalCount"`
	Error      string `json:"Error"`
}

// Connect starts the VPN. Returns empty string on success, error message on failure.
func (a *App) Connect() string {
	if err := a.manager.Connect(); err != nil {
		return err.Error()
	}
	return ""
}

// Disconnect stops the VPN.
func (a *App) Disconnect() {
	a.manager.Disconnect()
}

// GetStatus returns current connection status.
func (a *App) GetStatus() StatusResult {
	s := a.manager.Status()
	return StatusResult{
		State:      string(s.State),
		Server:     s.Server,
		Delay:      s.Delay,
		AliveCount: s.AliveCount,
		TotalCount: s.TotalCount,
		Error:      s.Error,
	}
}
