// AutoVPN — one-button VPN app.
// Run: sudo go run ./cmd/autovpn
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mewmewmemw/autovpn/internal/app"
	"github.com/mewmewmemw/autovpn/internal/config"
)

func main() {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = "/tmp"
	}
	cacheDir += "/autovpn"

	mgr := app.NewManager(&config.Fetcher{
		CacheDir: cacheDir,
	})

	mgr.OnChange(func(s app.Status) {
		switch s.State {
		case app.StateFetching:
			fmt.Println("⟳ Downloading configs...")
		case app.StateStarting:
			fmt.Println("⟳ Starting sing-box...")
		case app.StateConnected:
			if s.Server != "" {
				fmt.Printf("✓ Connected: %s (%dms) — %d/%d servers alive\n",
					s.Server, s.Delay, s.AliveCount, s.TotalCount)
			} else {
				fmt.Printf("✓ Connected — waiting for urltest (%d servers)\n", s.TotalCount)
			}
		case app.StateError:
			fmt.Printf("✗ Error: %s\n", s.Error)
		case app.StateDisconnected:
			fmt.Println("○ Disconnected")
		}
	})

	fmt.Println("AutoVPN — connecting...")
	if err := mgr.Connect(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\nPress Ctrl+C to disconnect")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	fmt.Println()
	mgr.Disconnect()
}
