package main

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
	"github.com/linvva/cf-node-bench/internal/probe"
	"github.com/linvva/cf-node-bench/internal/publish"
	runengine "github.com/linvva/cf-node-bench/internal/run"
	"github.com/linvva/cf-node-bench/internal/source"
	"github.com/linvva/cf-node-bench/internal/storage"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type NetworkInfo struct {
	Interface string `json:"interface"`
	IPv4      string `json:"ipv4"`
	Status    string `json:"status"`
}

type Bootstrap struct {
	Settings        config.Settings      `json:"settings"`
	PublishSettings publish.SettingsView `json:"publishSettings"`
	Sources         []source.HTTPSource  `json:"sources"`
	History         []model.RunSummary   `json:"history"`
	Network         NetworkInfo          `json:"network"`
	Current         string               `json:"currentRunId,omitempty"`
}

type App struct {
	ctx     context.Context
	store   *storage.Store
	manager runengine.Manager
	engine  runengine.Engine
	publish *publish.Service
	queue   *publish.Coordinator
	mu      sync.Mutex
}

func NewApp(store *storage.Store) *App {
	settings := store.Settings()
	app := &App{store: store, publish: publish.NewService(), engine: runengine.Engine{Dependencies: runengine.Dependencies{
		Fetcher:   newTrackingFetcher(store, settings),
		TCP:       probe.TCPProber{Timeout: settings.ConnectTimeout()},
		HTTPS:     probe.HTTPSProber{ConnectTimeout: settings.ConnectTimeout(), RequestTimeout: settings.RequestTimeout()},
		Bandwidth: probe.BandwidthProber{ConnectTimeout: settings.ConnectTimeout(), TotalTimeout: settings.BandwidthTimeout(), MaxBytes: settings.MaxDownloadBytes, URL: settings.BandwidthTestURL},
	}}}
	app.queue = publish.NewCoordinator(app.publish, app.handlePublishUpdate)
	return app
}

type trackingFetcher struct {
	store   *storage.Store
	fetcher source.Fetcher
}

func newTrackingFetcher(store *storage.Store, settings config.Settings) trackingFetcher {
	return trackingFetcher{store: store, fetcher: source.Fetcher{Timeout: settings.SourceTimeout(), MaxRetries: settings.SourceRetries}}
}

func (f trackingFetcher) Fetch(ctx context.Context, current source.HTTPSource) (source.ParseResult, error) {
	result, err := f.fetcher.Fetch(ctx, current)
	status := "可用"
	count := len(result.Candidates)
	if err != nil {
		status = err.Error()
		count = 0
	}
	_ = f.store.UpdateSourceStatus(current.ID, time.Now(), status, count)
	return result, err
}

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

func (a *App) Bootstrap() Bootstrap {
	return Bootstrap{Settings: a.store.Settings(), PublishSettings: a.store.PublishSettingsView(), Sources: a.store.Sources(), History: a.store.History(), Network: detectNetwork(), Current: a.manager.Current()}
}

func (a *App) SaveSettings(settings config.Settings) error { return a.store.SaveSettings(settings) }

func (a *App) SavePublishSettings(request publish.SaveRequest) (publish.SettingsView, error) {
	return a.store.SavePublishSettings(request)
}

func (a *App) ClearPublishCredential(target string) (publish.SettingsView, error) {
	return a.store.ClearPublishCredential(target)
}

func (a *App) TestPublishTarget(target string, request publish.SaveRequest) error {
	settings := request.Merge(a.store.PublishSettings())
	settings.Cloudflare.Enabled = target == "cloudflare"
	settings.GitHub.Enabled = target == "github"
	settings.Gist.Enabled = target == "gist"
	settings.Telegram.Enabled = target == "telegram"
	if err := settings.Validate(); err != nil {
		return err
	}
	ctx := a.appContext()
	switch target {
	case "cloudflare":
		return a.publish.TestCloudflare(ctx, settings)
	case "github":
		return a.publish.TestGitHub(ctx, settings)
	case "gist":
		return a.publish.TestGist(ctx, settings)
	case "telegram":
		return a.publish.TestTelegram(ctx, settings)
	default:
		return fmt.Errorf("未知发布目标")
	}
}

func (a *App) PublishRun(runID, target string) error {
	if target != "all" && target != "cloudflare" && target != "github" && target != "gist" && target != "telegram" {
		return fmt.Errorf("未知发布目标")
	}
	summary, ok := a.store.HistoryByID(runID)
	if !ok {
		return fmt.Errorf("未找到测速历史")
	}
	if summary.State != "completed" {
		return fmt.Errorf("只有已完成的测速可以发布")
	}
	return a.queue.Enqueue(a.appContext(), summary, a.store.PublishSettings(), target)
}

func (a *App) SaveSources(sources []source.HTTPSource) error {
	for index, item := range sources {
		if strings.TrimSpace(item.ID) == "" {
			sources[index].ID = fmt.Sprintf("source-%d", time.Now().UnixNano()+int64(index))
		}
		if strings.TrimSpace(item.Name) == "" || strings.TrimSpace(item.URL) == "" {
			return fmt.Errorf("数据源名称和 URL 不能为空")
		}
		if !strings.HasPrefix(item.URL, "http://") && !strings.HasPrefix(item.URL, "https://") {
			return fmt.Errorf("数据源 URL 必须以 http:// 或 https:// 开头")
		}
	}
	return a.store.SaveSources(sources)
}

func (a *App) StartRun() (string, error) {
	a.mu.Lock()
	settings := a.store.Settings()
	a.engine.Dependencies.Fetcher = newTrackingFetcher(a.store, settings)
	a.engine.Dependencies.TCP = probe.TCPProber{Timeout: settings.ConnectTimeout()}
	a.engine.Dependencies.HTTPS = probe.HTTPSProber{ConnectTimeout: settings.ConnectTimeout(), RequestTimeout: settings.RequestTimeout()}
	a.engine.Dependencies.Bandwidth = probe.BandwidthProber{ConnectTimeout: settings.ConnectTimeout(), TotalTimeout: settings.BandwidthTimeout(), MaxBytes: settings.MaxDownloadBytes, URL: settings.BandwidthTestURL}
	a.mu.Unlock()
	return a.manager.Start(a.ctx, a.engine, settings, a.store.Sources(), func(progress model.RunProgress) {
		runtime.EventsEmit(a.ctx, "run:progress", progress)
	}, func(summary model.RunSummary) {
		_ = a.store.AddHistory(summary)
		runtime.EventsEmit(a.ctx, "run:complete", summary)
		if summary.State == "completed" {
			_ = a.queue.Enqueue(a.appContext(), summary, a.store.PublishSettings(), "all")
		}
	})
}

func (a *App) CancelRun() bool { return a.manager.Cancel() }

func (a *App) handlePublishUpdate(update publish.Update) {
	_ = a.store.UpdatePublication(update.RunID, update.Result)
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "publish:progress", update)
	}
}

func (a *App) appContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func detectNetwork() NetworkInfo {
	interfaces, err := net.Interfaces()
	if err != nil {
		return NetworkInfo{Status: "unavailable"}
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, _ := iface.Addrs()
		for _, address := range addresses {
			ip, _, _ := net.ParseCIDR(address.String())
			if ip != nil && ip.To4() != nil {
				return NetworkInfo{Interface: iface.Name, IPv4: ip.String(), Status: "online"}
			}
		}
	}
	return NetworkInfo{Status: "offline"}
}
