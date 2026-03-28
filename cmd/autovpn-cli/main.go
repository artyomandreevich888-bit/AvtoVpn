// AutoVPN CLI — headless mode for testing.
// Run: sudo ./autovpn-cli
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mewmewmemw/autovpn/internal/app"
	"github.com/mewmewmemw/autovpn/internal/config"
)

func main() {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = "/tmp"
	}
	cacheDir += "/autovpn"

	mgr := app.NewManager(&config.Fetcher{CacheDir: cacheDir})

	mgr.OnChange(func(s app.Status) {
		switch s.State {
		case app.StateFetching:
			fmt.Println("⟳ Downloading configs...")
		case app.StateStarting:
			fmt.Println("⟳ Starting sing-box...")
		case app.StateConnected:
			if s.Server != "" {
				fmt.Printf("✓ %s (%dms) — %d/%d alive\n", s.Server, s.Delay, s.AliveCount, s.TotalCount)
			} else {
				fmt.Printf("✓ Connected — urltest running (%d servers)\n", s.TotalCount)
			}
		case app.StateError:
			fmt.Printf("✗ %s\n", s.Error)
		case app.StateDisconnected:
			fmt.Println("○ Disconnected")
		}
	})

	fmt.Println("AutoVPN CLI — connecting...")
	if err := mgr.Connect(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Press Ctrl+C to stop")
	fmt.Println()

	// Wait a bit then show periodic status
	go func() {
		time.Sleep(5 * time.Second)
		for {
			time.Sleep(10 * time.Second)
			s := mgr.Status()
			if s.State == app.StateConnected && s.Server != "" {
				fmt.Printf("  [status] %s (%dms) — %d/%d alive\n", s.Server, s.Delay, s.AliveCount, s.TotalCount)
			}
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	fmt.Println("\nStopping...")
	mgr.Disconnect()
}
