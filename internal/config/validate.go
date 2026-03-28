package config

import (
	"context"
	"fmt"
	"log"
	"net"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// ValidatedServer holds pre-validation result for a single server.
type ValidatedServer struct {
	Config VlessConfig
	RTT    time.Duration
	Alive  bool
}

// PreValidate tests TCP connectivity to each server in parallel.
// Runs BEFORE TUN creation — network is still open.
// Returns only alive servers, sorted by RTT (fastest first).
func PreValidate(ctx context.Context, servers []VlessConfig, concurrency int, timeout time.Duration, onProgress func(done, total, alive int)) []ValidatedServer {
	if len(servers) == 0 {
		return nil
	}

	results := make([]ValidatedServer, len(servers))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var doneCount, aliveCount int64

	for i, s := range servers {
		results[i] = ValidatedServer{Config: s}
		wg.Add(1)
		go func(idx int, server VlessConfig) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			addr := fmt.Sprintf("%s:%d", server.Host, server.Port)
			start := time.Now()

			dialer := net.Dialer{Timeout: timeout}
			conn, err := dialer.DialContext(ctx, "tcp", addr)
			rtt := time.Since(start)

			if err == nil {
				conn.Close()
				results[idx].RTT = rtt
				results[idx].Alive = true
				atomic.AddInt64(&aliveCount, 1)
			} else {
				log.Printf("[validate] %s: %v", addr, err)
			}

			d := int(atomic.AddInt64(&doneCount, 1))
			a := int(atomic.LoadInt64(&aliveCount))
			if onProgress != nil {
				onProgress(d, len(servers), a)
			}
		}(i, s)
	}

	wg.Wait()

	var alive []ValidatedServer
	for _, r := range results {
		if r.Alive {
			alive = append(alive, r)
		}
	}
	sort.Slice(alive, func(i, j int) bool {
		return alive[i].RTT < alive[j].RTT
	})
	return alive
}
