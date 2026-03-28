//go:build ignore

// gen_fallback downloads current VLESS+Reality configs and writes them
// to internal/config/fallback.txt for embedding into the binary.
//
// Usage: go generate ./internal/config/...
//    or: go run ./scripts/gen_fallback.go
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

var sources = []string{
	"https://raw.githubusercontent.com/igareck/vpn-configs-for-russia/main/Vless-Reality-White-Lists-Rus-Mobile.txt",
	"https://raw.githubusercontent.com/igareck/vpn-configs-for-russia/main/BLACK_VLESS_RUS.txt",
}

const output = "internal/config/fallback.txt"
const maxConfigs = 30

func main() {
	client := &http.Client{Timeout: 15 * time.Second}

	seen := make(map[string]bool)
	var lines []string

	for _, url := range sources {
		resp, err := client.Get(url)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s: %v\n", url, err)
			continue
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "vless://") {
				continue
			}
			if !strings.Contains(line, "security=reality") {
				continue
			}
			if seen[line] {
				continue
			}
			seen[line] = true
			lines = append(lines, line)
			if len(lines) >= maxConfigs {
				break
			}
		}
		if len(lines) >= maxConfigs {
			break
		}
	}

	if len(lines) == 0 {
		fmt.Fprintln(os.Stderr, "error: no reality configs found")
		os.Exit(1)
	}

	err := os.WriteFile(output, []byte(strings.Join(lines, "\n")+"\n"), 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error writing %s: %v\n", output, err)
		os.Exit(1)
	}

	fmt.Printf("wrote %d reality configs to %s\n", len(lines), output)
}
