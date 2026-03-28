package engine

import (
	"context"
	"fmt"
	"sync"

	"github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	json "github.com/sagernet/sing/common/json"
)

type Engine struct {
	mu       sync.Mutex
	instance *box.Box
	cancel   context.CancelFunc
}

func (e *Engine) Start(configJSON []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.instance != nil {
		return fmt.Errorf("already running")
	}

	ctx, cancel := context.WithCancel(context.Background())
	ctx = include.Context(ctx)

	var opts option.Options
	if err := json.UnmarshalContext(ctx, configJSON, &opts); err != nil {
		cancel()
		return fmt.Errorf("parse config: %w", err)
	}

	instance, err := box.New(box.Options{
		Options: opts,
		Context: ctx,
	})
	if err != nil {
		cancel()
		return fmt.Errorf("create instance: %w", err)
	}

	if err := instance.Start(); err != nil {
		instance.Close()
		cancel()
		return fmt.Errorf("start: %w", err)
	}

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
