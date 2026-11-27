package run

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
)

var ErrAlreadyRunning = errors.New("已有测速任务正在运行")

type Manager struct {
	mu      sync.Mutex
	cancel  context.CancelCauseFunc
	current string
}

func (m *Manager) Start(parent context.Context, engine Engine, settings config.Settings, input RunInput, emit func(model.RunProgress), done func(model.RunSummary)) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		return "", ErrAlreadyRunning
	}
	id := fmt.Sprintf("run-%d", time.Now().UnixMilli())
	ctx, cancel := context.WithCancelCause(parent)
	m.cancel, m.current = cancel, id
	go func() {
		defer cancel(nil)
		summary := engine.Run(ctx, id, settings, input, emit)
		m.mu.Lock()
		if ctx.Err() != nil && summary.State == "completed" {
			// A cancellation accepted before this lock wins the completion race.
			summary.State = "cancelled"
			summary.PartialResults = errors.Is(context.Cause(ctx), errKeepCancelledResults) && countIf(summary.Results, func(r model.ProbeResult) bool { return r.Status == "qualified" }) > 0
			if !summary.PartialResults {
				summary.Results = []model.ProbeResult{}
			}
		}
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
	return m.CancelWithResults(false)
}

func (m *Manager) CancelWithResults(keep bool) bool {
	if keep {
		return m.cancelWithCause(errKeepCancelledResults)
	}
	return m.cancelWithCause(errDiscardRun)
}

func (m *Manager) SaveAndStop() bool {
	return m.cancelWithCause(errSaveDownload)
}

func (m *Manager) cancelWithCause(cause error) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel == nil {
		return false
	}
	m.cancel(cause)
	return true
}
func (m *Manager) Current() string { m.mu.Lock(); defer m.mu.Unlock(); return m.current }
