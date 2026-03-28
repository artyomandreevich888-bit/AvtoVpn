package engine

import (
	"context"
	"fmt"
	"sync"
	"testing"

	box "github.com/sagernet/sing-box"
)

// --- mock box ---

type mockBox struct {
	started bool
	closed  bool
}

func (m *mockBox) Start() error { m.started = true; return nil }
func (m *mockBox) Close() error { m.closed = true; return nil }

type failStartBox struct{}

func (f *failStartBox) Start() error { return fmt.Errorf("start failed") }
func (f *failStartBox) Close() error { return nil }

func mockFactory(instance BoxInstance) BoxFactory {
	return func(opts box.Options) (BoxInstance, error) {
		return instance, nil
	}
}

func failFactory(err error) BoxFactory {
	return func(opts box.Options) (BoxInstance, error) {
		return nil, err
	}
}

// Minimal valid sing-box config JSON for parsing.
var minimalConfig = []byte(`{
	"log": {"level": "error"},
	"outbounds": [{"type": "direct", "tag": "direct"}]
}`)

// --- tests ---

func TestEngine_IsRunning_Default(t *testing.T) {
	e := &Engine{}
	if e.IsRunning() {
		t.Error("new engine should not be running")
	}
}

func TestEngine_Stop_WhenNotRunning(t *testing.T) {
	e := &Engine{}
	if err := e.Stop(); err != nil {
		t.Errorf("stop idle: %v", err)
	}
}

func TestEngine_DoubleStop(t *testing.T) {
	e := &Engine{}
	e.Stop()
	e.Stop()
	e.Stop()
}

func TestEngine_Start_InvalidJSON(t *testing.T) {
	e := &Engine{}
	err := e.Start([]byte("not json"))
	if err == nil {
		t.Fatal("expected error")
	}
	if e.IsRunning() {
		t.Error("should not be running")
	}
}

func TestEngine_Start_Success_Mock(t *testing.T) {
	mb := &mockBox{}
	e := &Engine{NewBox: mockFactory(mb)}

	err := e.Start(minimalConfig)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !e.IsRunning() {
		t.Error("should be running")
	}
	if !mb.started {
		t.Error("box.Start not called")
	}

	err = e.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if e.IsRunning() {
		t.Error("should not be running after stop")
	}
	if !mb.closed {
		t.Error("box.Close not called")
	}
}

func TestEngine_Start_AlreadyRunning(t *testing.T) {
	e := &Engine{NewBox: mockFactory(&mockBox{})}
	e.Start(minimalConfig)
	defer e.Stop()

	err := e.Start(minimalConfig)
	if err == nil {
		t.Fatal("expected error for double start")
	}
}

func TestEngine_Start_FactoryError(t *testing.T) {
	e := &Engine{NewBox: failFactory(fmt.Errorf("factory boom"))}

	err := e.Start(minimalConfig)
	if err == nil {
		t.Fatal("expected error")
	}
	if e.IsRunning() {
		t.Error("should not be running")
	}
}

func TestEngine_Start_BoxStartFails(t *testing.T) {
	e := &Engine{NewBox: mockFactory(&failStartBox{})}

	err := e.Start(minimalConfig)
	if err == nil {
		t.Fatal("expected error from Start failure")
	}
	if e.IsRunning() {
		t.Error("should not be running")
	}
}

func TestEngine_Start_WithPlatformProvider(t *testing.T) {
	mp := &mockPlatform{}
	mb := &mockBox{}
	e := &Engine{
		Platform: mp,
		NewBox:   mockFactory(mb),
	}

	err := e.Start(minimalConfig)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer e.Stop()

	if !mp.setupCalled {
		t.Error("SetupContext not called")
	}
	if !mp.optsCalled {
		t.Error("BoxOptions not called")
	}
}

func TestEngine_ConcurrentStartStop(t *testing.T) {
	var wg sync.WaitGroup
	e := &Engine{NewBox: mockFactory(&mockBox{})}

	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			e.Start(minimalConfig)
		}()
		go func() {
			defer wg.Done()
			e.Stop()
		}()
	}
	wg.Wait()
	e.Stop()
}

// --- mock platform ---

type mockPlatform struct {
	setupCalled bool
	optsCalled  bool
}

func (m *mockPlatform) SetupContext(ctx context.Context) context.Context {
	m.setupCalled = true
	return ctx
}

func (m *mockPlatform) BoxOptions(opts box.Options) box.Options {
	m.optsCalled = true
	return opts
}
