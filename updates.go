package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/endpoint"
)

// Release builds inject the version and public key. Private keys stay in CI.
var buildVersion = "dev"
var updatePublicKey = ""

const updateManifestURL = "https://github.com/linvva/cf-node-bench/releases/latest/download/update-manifest.json"

type UpdateState struct {
	CurrentVersion string `json:"currentVersion"`
	Status         string `json:"status"`
	Version        string `json:"version,omitempty"`
	Notes          string `json:"notes,omitempty"`
	Size           int64  `json:"size,omitempty"`
	Downloaded     int64  `json:"downloaded,omitempty"`
	Error          string `json:"error,omitempty"`
}

type updateBackend interface {
	Check(context.Context) (*updater.Release, error)
	DownloadAndInstall(context.Context) error
	Restart(context.Context) error
}

type updateController struct {
	mu      sync.Mutex
	opMu    sync.Mutex
	emitMu  sync.Mutex
	state   UpdateState
	pending bool
	backend updateBackend
	emit    func(UpdateState)
}

func newUpdateController(version, publicKey, manifestURL string, client *http.Client, engine *updater.Updater, emit func(UpdateState)) *updateController {
	c := &updateController{state: UpdateState{CurrentVersion: version, Status: "unavailable"}, emit: emit}
	if version == "" || version == "dev" {
		c.state.Error = "开发版本不支持自动更新，请使用正式发布版本"
		return c
	}
	if strings.TrimSpace(publicKey) == "" {
		c.state.Error = "此构建未配置更新公钥，请从 GitHub Release 手动更新"
		return c
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(publicKey))
	if err != nil {
		key, err = base64.RawStdEncoding.DecodeString(strings.TrimSpace(publicKey))
	}
	if err != nil || len(key) != ed25519.PublicKeySize {
		c.state.Error = "更新公钥无效，请从 GitHub Release 手动更新"
		return c
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Minute}
	}
	provider, err := endpoint.New(endpoint.Config{URL: manifestURL, Channel: "stable", HTTPClient: client})
	if err == nil {
		err = engine.Init(updater.Config{
			CurrentVersion: strings.TrimPrefix(version, "v"),
			Providers:      []updater.Provider{provider},
			PublicKey:      key,
			Window:         updater.WindowNone,
		})
	}
	if err != nil {
		c.state.Error = fmt.Sprintf("无法启用自动更新：%v", err)
		return c
	}
	c.backend = engine
	c.state.Status = "idle"
	return c
}

func (a *App) configureUpdater(desktop *application.App) error {
	c := newUpdateController(buildVersion, updatePublicKey, updateManifestURL, nil, desktop.Updater, func(state UpdateState) {
		desktop.Event.Emit("update:state", state)
	})
	desktop.Event.On(updater.EventDownloadProgress, func(event *application.CustomEvent) {
		if progress, ok := event.Data.(updater.Progress); ok {
			c.progress(progress)
		}
	})
	a.mu.Lock()
	a.updates = c
	a.mu.Unlock()
	if c.backend != nil {
		go func() { _, _ = a.CheckForUpdate() }()
	}
	return nil
}

func (c *updateController) snapshot() UpdateState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

func (c *updateController) change(fn func(*UpdateState)) UpdateState {
	c.emitMu.Lock()
	defer c.emitMu.Unlock()
	c.mu.Lock()
	fn(&c.state)
	state := c.state
	c.mu.Unlock()
	if c.emit != nil {
		c.emit(state)
	}
	return state
}

func (c *updateController) progress(progress updater.Progress) {
	c.emitMu.Lock()
	defer c.emitMu.Unlock()
	c.mu.Lock()
	if c.state.Status != "downloading" {
		c.mu.Unlock()
		return
	}
	c.state.Downloaded = progress.Written
	if progress.Total > 0 {
		c.state.Size = progress.Total
	}
	state := c.state
	c.mu.Unlock()
	if c.emit != nil {
		c.emit(state)
	}
}

func (a *App) updateController() *updateController {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.updates
}

func (a *App) UpdateStatus() UpdateState {
	if c := a.updateController(); c != nil {
		return c.snapshot()
	}
	return UpdateState{CurrentVersion: buildVersion, Status: "unavailable", Error: "自动更新尚未初始化"}
}

func (a *App) CheckForUpdate() (UpdateState, error) {
	c := a.updateController()
	if c == nil || c.backend == nil {
		state := a.UpdateStatus()
		return state, errors.New(state.Error)
	}
	if !c.opMu.TryLock() {
		return c.snapshot(), errors.New("更新操作正在进行")
	}
	defer c.opMu.Unlock()
	if state := c.snapshot(); state.Status == "ready" {
		return state, nil
	}
	c.pending = false
	c.change(func(state *UpdateState) {
		*state = UpdateState{CurrentVersion: state.CurrentVersion, Status: "checking"}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	release, err := c.backend.Check(ctx)
	state := c.change(func(state *UpdateState) {
		switch {
		case err != nil:
			state.Status, state.Error = "error", err.Error()
		case release == nil:
			state.Status = "idle"
		default:
			state.Status, state.Version, state.Notes, state.Size = "available", release.Version, release.Notes, release.Artifact.Size
			c.pending = true
		}
	})
	return state, err
}

func (a *App) DownloadUpdate() (UpdateState, error) {
	c := a.updateController()
	if c == nil || c.backend == nil {
		state := a.UpdateStatus()
		return state, errors.New(state.Error)
	}
	if !c.opMu.TryLock() {
		return c.snapshot(), errors.New("更新操作正在进行")
	}
	defer c.opMu.Unlock()
	if state := c.snapshot(); state.Status == "ready" {
		return state, nil
	}
	if !c.pending {
		return c.snapshot(), errors.New("请先检查并选择可用更新")
	}
	c.change(func(state *UpdateState) { state.Status, state.Error, state.Downloaded = "downloading", "", 0 })
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	err := c.backend.DownloadAndInstall(ctx)
	state := c.change(func(state *UpdateState) {
		if err != nil {
			state.Status, state.Error = "error", err.Error()
		} else {
			state.Status = "ready"
			state.Downloaded = state.Size
		}
	})
	return state, err
}

func (a *App) RestartForUpdate() error {
	c := a.updateController()
	if c == nil || c.backend == nil {
		return errors.New(a.UpdateStatus().Error)
	}
	if !c.opMu.TryLock() {
		return errors.New("更新操作正在进行")
	}
	defer c.opMu.Unlock()
	if c.snapshot().Status != "ready" {
		return errors.New("更新尚未下载并验证完成")
	}
	a.mu.Lock()
	if a.restarting {
		a.mu.Unlock()
		return errors.New("应用正在重启")
	}
	if a.runPending || a.manager.Current() != "" || (a.queue != nil && a.queue.Busy()) {
		a.mu.Unlock()
		return errors.New("测速或发布任务进行中，请等待完成后重启更新")
	}
	a.restarting = true
	a.mu.Unlock()
	// Restart may emit events and request shutdown, so do not hold the App lock.
	if err := c.backend.Restart(context.Background()); err != nil {
		a.mu.Lock()
		a.restarting = false
		a.mu.Unlock()
		c.change(func(state *UpdateState) { state.Error = err.Error() })
		return err
	}
	return nil
}
