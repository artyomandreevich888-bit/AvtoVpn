// Spike: минимальная проверка sing-box как Go library.
// Запуск: sudo go run ./cmd/autovpn
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/option"
)

func main() {
	config := generateConfig()

	configJSON, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "config marshal error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Generated config:")
	fmt.Println(string(configJSON))

	var opts option.Options
	if err := json.Unmarshal(configJSON, &opts); err != nil {
		fmt.Fprintf(os.Stderr, "config parse error: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	instance, err := box.New(box.Options{
		Options: opts,
		Context: ctx,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sing-box create error: %v\n", err)
		os.Exit(1)
	}

	if err := instance.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "sing-box start error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n✓ sing-box started")
	fmt.Println("  Check: curl https://ifconfig.me")
	fmt.Println("  API:   curl http://127.0.0.1:9090/proxies")
	fmt.Println("  Press Ctrl+C to stop")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	fmt.Println("\nStopping...")
	instance.Close()
}

func generateConfig() map[string]any {
	return map[string]any{
		"log": map[string]any{
			"level":     "info",
			"timestamp": true,
		},
		"dns": map[string]any{
			"servers": []map[string]any{
				{"type": "tls", "tag": "dns-remote", "server": "8.8.8.8"},
				{"type": "local", "tag": "dns-local"},
			},
			"rules": []map[string]any{
				{"outbound": "any", "server": "dns-local"},
			},
			"final": "dns-remote",
		},
		"inbounds": []map[string]any{
			{
				"type":         "tun",
				"tag":          "tun-in",
				"address":      []string{"172.19.0.1/30", "fdfe:dcba:9876::1/126"},
				"auto_route":   true,
				"strict_route": true,
				"stack":        "mixed",
			},
		},
		"outbounds": []map[string]any{
			{"type": "direct", "tag": "direct"},
		},
		"route": map[string]any{
			"rules": []map[string]any{
				{"action": "sniff"},
				{"protocol": "dns", "action": "hijack-dns"},
				{"ip_is_private": true, "outbound": "direct"},
			},
			"final":                "direct",
			"auto_detect_interface": true,
		},
		"experimental": map[string]any{
			"clash_api": map[string]any{
				"external_controller": "127.0.0.1:9090",
				"secret":              "autovpn-spike",
			},
		},
	}
}
