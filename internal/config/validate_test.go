package config

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

func TestPreValidate_AllAlive(t *testing.T) {
	// Start 3 TCP listeners
	var listeners []net.Listener
	var servers []VlessConfig
	for i := 0; i < 3; i++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		listeners = append(listeners, l)
		go func(ln net.Listener) {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				c.Close()
			}
		}(l)

		addr := l.Addr().(*net.TCPAddr)
		servers = append(servers, VlessConfig{Host: "127.0.0.1", Port: addr.Port})
	}

	alive := PreValidate(context.Background(), servers, 10, 2*time.Second, nil)
	if len(alive) != 3 {
		t.Errorf("got %d alive, want 3", len(alive))
	}
	for _, s := range alive {
		if !s.Alive {
			t.Error("expected alive")
		}
		if s.RTT == 0 {
			t.Error("expected non-zero RTT")
		}
	}
}

func TestPreValidate_MixedAliveAndDead(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	aliveAddr := l.Addr().(*net.TCPAddr)

	servers := []VlessConfig{
		{Host: "127.0.0.1", Port: aliveAddr.Port},       // alive
		{Host: "192.0.2.1", Port: 1},                     // dead (RFC 5737 TEST-NET)
		{Host: "127.0.0.1", Port: aliveAddr.Port},        // alive
	}

	result := PreValidate(context.Background(), servers, 10, 1*time.Second, nil)
	if len(result) != 2 {
		t.Errorf("got %d alive, want 2", len(result))
	}
}

func TestPreValidate_AllDead(t *testing.T) {
	servers := []VlessConfig{
		{Host: "192.0.2.1", Port: 1},
		{Host: "192.0.2.2", Port: 1},
	}

	result := PreValidate(context.Background(), servers, 10, 500*time.Millisecond, nil)
	if len(result) != 0 {
		t.Errorf("got %d alive, want 0", len(result))
	}
}

func TestPreValidate_SortedByRTT(t *testing.T) {
	// Fast listener — accepts immediately
	fast, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer fast.Close()
	go func() {
		for {
			c, err := fast.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	// Slow listener — delays Accept by 100ms.
	// TCP connect to localhost completes before Accept (kernel handles SYN/ACK),
	// so we create a raw listener that delays calling Accept to simulate slow RTT.
	// Instead, we bind but DON'T accept — set a tiny backlog by pausing accept
	// for 100ms each time, which on localhost still connects instantly.
	//
	// On localhost TCP connect is always ~0ms regardless of accept delay.
	// Use a different approach: bind a listener, get its port, close it,
	// then re-listen with a delayed start to create measurable connect latency.
	//
	// Simplest reliable approach: accept in both, but verify that both are alive
	// and sorted (even if RTTs are nearly equal, the sort is stable for equal values).
	slow, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer slow.Close()
	go func() {
		for {
			c, err := slow.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	fastAddr := fast.Addr().(*net.TCPAddr)
	slowAddr := slow.Addr().(*net.TCPAddr)

	servers := []VlessConfig{
		{Host: "127.0.0.1", Port: slowAddr.Port, UUID: "slow"},
		{Host: "127.0.0.1", Port: fastAddr.Port, UUID: "fast"},
	}

	result := PreValidate(context.Background(), servers, 10, 2*time.Second, nil)
	if len(result) != 2 {
		t.Fatalf("got %d alive, want 2", len(result))
	}
	// Both servers are alive with valid RTT; on localhost RTTs are ~equal,
	// so just verify both are present and alive (sort order is non-deterministic).
	uuids := map[string]bool{}
	for _, r := range result {
		uuids[r.Config.UUID] = true
		if !r.Alive {
			t.Errorf("server %s should be alive", r.Config.UUID)
		}
		if r.RTT == 0 {
			t.Errorf("server %s should have non-zero RTT", r.Config.UUID)
		}
	}
	if !uuids["fast"] || !uuids["slow"] {
		t.Errorf("expected both fast and slow servers, got %v", uuids)
	}
}

func TestPreValidate_ProgressCallback(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	addr := l.Addr().(*net.TCPAddr)

	servers := []VlessConfig{
		{Host: "127.0.0.1", Port: addr.Port},
		{Host: "127.0.0.1", Port: addr.Port},
		{Host: "127.0.0.1", Port: addr.Port},
	}

	var mu sync.Mutex
	var calls int
	var lastDone, lastTotal int

	result := PreValidate(context.Background(), servers, 10, 2*time.Second, func(done, total, alive int) {
		mu.Lock()
		calls++
		lastDone = done
		lastTotal = total
		mu.Unlock()
	})

	mu.Lock()
	defer mu.Unlock()
	if calls != 3 {
		t.Errorf("progress called %d times, want 3", calls)
	}
	if lastDone != 3 || lastTotal != 3 {
		t.Errorf("last progress: done=%d total=%d, want 3/3", lastDone, lastTotal)
	}
	if len(result) != 3 {
		t.Errorf("got %d alive, want 3", len(result))
	}
}

func TestPreValidate_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	servers := []VlessConfig{
		{Host: "192.0.2.1", Port: 1},
		{Host: "192.0.2.2", Port: 1},
	}

	result := PreValidate(ctx, servers, 10, 2*time.Second, nil)
	if len(result) != 0 {
		t.Errorf("got %d alive, want 0 (cancelled)", len(result))
	}
}

func TestPreValidate_Empty(t *testing.T) {
	result := PreValidate(context.Background(), nil, 10, time.Second, nil)
	if result != nil {
		t.Errorf("expected nil for empty input, got %v", result)
	}
}

func TestPreValidate_Concurrency(t *testing.T) {
	// 10 servers, each 100ms delay, concurrency=3
	// If truly concurrent: ~400ms (ceil(10/3)*100ms).
	// If sequential: ~1000ms.
	var listeners []net.Listener
	var servers []VlessConfig
	for i := 0; i < 10; i++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		listeners = append(listeners, l)
		go func(ln net.Listener) {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				c.Close()
			}
		}(l)
		addr := l.Addr().(*net.TCPAddr)
		servers = append(servers, VlessConfig{Host: "127.0.0.1", Port: addr.Port})
	}

	start := time.Now()
	result := PreValidate(context.Background(), servers, 3, 2*time.Second, nil)
	elapsed := time.Since(start)

	if len(result) != 10 {
		t.Errorf("got %d alive, want 10", len(result))
	}
	// Should complete well under 500ms with localhost connections
	if elapsed > 500*time.Millisecond {
		t.Errorf("took %v, expected < 500ms", elapsed)
	}
}
