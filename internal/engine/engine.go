package engine

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	json "github.com/sagernet/sing/common/json"
)

// PlatformProvider injects platform-specific behavior into the engine.
// Nil for desktop; implemented by mobile/ for Android/iOS.
type PlatformProvider interface {
	SetupContext(ctx context.Context) context.Context
	BoxOptions(opts box.Options) box.Options
}

// BoxInstance abstracts a sing-box instance for testability.
type BoxInstance interface {
	Start() error
	Close() error
}

// BoxFactory creates BoxInstance from options. Overridable for testing.
type BoxFactory func(opts box.Options) (BoxInstance, error)

// defaultBoxFactory creates a real sing-box instance.
func defaultBoxFactory(opts box.Options) (BoxInstance, error) {
	return box.New(opts)
}

type Engine struct {
	mu       sync.Mutex
	instance BoxInstance
	cancel   context.CancelFunc

	// Platform is set by mobile before Start. Nil on desktop.
	Platform PlatformProvider

	// NewBox creates a box instance. Nil uses real sing-box.
	NewBox BoxFactory
}

func (e *Engine) boxFactory() BoxFactory {
	if e.NewBox != nil {
		return e.NewBox
	}
	return defaultBoxFactory
}

func (e *Engine) Start(configJSON []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.instance != nil {
		return fmt.Errorf("already running")
	}

	ctx, cancel := context.WithCancel(context.Background())
	ctx = include.Context(ctx)

	if e.Platform != nil {
		ctx = e.Platform.SetupContext(ctx)
	}

	log.Println("[engine] parsing config JSON,", len(configJSON), "bytes")
	var opts option.Options
	if err := json.UnmarshalContext(ctx, configJSON, &opts); err != nil {
		cancel()
		return fmt.Errorf("parse config: %w", err)
	}

	boxOpts := box.Options{
		Options: opts,
		Context: ctx,
	}
	if e.Platform != nil {
		boxOpts = e.Platform.BoxOptions(boxOpts)
	}

	log.Println("[engine] creating box instance")
	instance, err := e.boxFactory()(boxOpts)
	if err != nil {
		cancel()
		return fmt.Errorf("create instance: %w", err)
	}

	log.Println("[engine] starting box instance")
	if err := instance.Start(); err != nil {
		instance.Close()
		cancel()
		return fmt.Errorf("start: %w", err)
	}
	log.Println("[engine] started OK")

	e.instance = instance
	e.cancel = cancel
	return nil
}

func (e *Engine) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.instance == nil {
		return nil
	}

	err := e.instance.Close()
	e.cancel()
	e.instance = nil
	e.cancel = nil
	return err
}

func (e *Engine) IsRunning() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.instance != nil
}
