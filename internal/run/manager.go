package run

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
	"github.com/linvva/cf-node-bench/internal/source"
)

var ErrAlreadyRunning = errors.New("已有测速任务正在运行")

type Manager struct {
	mu      sync.Mutex
	cancel  context.CancelFunc
	current string
}

func (m *Manager) Start(parent context.Context, engine Engine, settings config.Settings, sources []source.HTTPSource, emit func(model.RunProgress), done func(model.RunSummary)) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		return "", ErrAlreadyRunning
	}
	id := fmt.Sprintf("run-%d", time.Now().UnixMilli())
	ctx, cancel := context.WithCancel(parent)
	m.cancel, m.current = cancel, id
	go func() {
		summary := engine.Run(ctx, id, settings, sources, emit)
		m.mu.Lock()
		m.cancel = nil
		m.current = ""
		m.mu.Unlock()
		if done != nil {
			done(summary)
		}
	}()
	return id, nil
}

func (m *Manager) Cancel() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel == nil {
		return false
	}
	m.cancel()
	return true
}
func (m *Manager) Current() string { m.mu.Lock(); defer m.mu.Unlock(); return m.current }
