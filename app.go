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
	Settings           config.Settings      `json:"settings"`
	PublishSettings    publish.SettingsView `json:"publishSettings"`
	Sources            []source.HTTPSource  `json:"sources"`
	History            []model.RunSummary   `json:"history"`
	Network            NetworkInfo          `json:"network"`
	Current            string               `json:"currentRunId,omitempty"`
	Resume             runengine.ResumeInfo `json:"resume"`
	CurrentResultRunID string               `json:"currentResultRunId,omitempty"`
	MigrationNotice    string               `json:"migrationNotice,omitempty"`
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
	return trackingFetcher{store: store, fetcher: source.Fetcher{Timeout: settings.SourceTimeout(), MaxRetries: settings.SourceRetries, AllowDomains: settings.RequireH3}}
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
	network := detectNetwork()
	return Bootstrap{Settings: a.store.Settings(), PublishSettings: a.store.PublishSettingsView(), Sources: a.store.Sources(), History: a.store.History(), Network: network, Current: a.manager.Current(), Resume: a.resumeInfo(network), MigrationNotice: a.store.MigrationNotice(), CurrentResultRunID: a.store.CurrentResultID()}
}

func (a *App) SaveSettings(settings config.Settings) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.manager.Current() != "" && settings.RequireH3 != a.store.Settings().RequireH3 {
		return fmt.Errorf("测速运行中不能切换测速模式")
	}
	return a.store.SaveSettings(settings)
}

func (a *App) SavePublishSettings(request publish.SaveRequest) (publish.SettingsView, error) {
	return a.store.SavePublishSettings(request)
}

func (a *App) ClearPublishCredential(target string) (publish.SettingsView, error) {
	return a.store.ClearPublishCredential(target)
}

func (a *App) TestPublishTarget(target string, request publish.SaveRequest) error {
	if request.TestMode != "" && request.TestMode != model.ModeTCPHTTPS && request.TestMode != model.ModeH3 {
		return fmt.Errorf("未知发布测试协议")
	}
	form := request.Merge(a.store.PublishSettings()).ForMode(request.TestMode)
	settings := publish.DefaultSettings()
	settings.Request = form.Request
	switch target {
	case "cloudflare":
		settings.Cloudflare = form.Cloudflare
		settings.Cloudflare.Enabled = settings.Cloudflare.RecordName != ""
	case "github":
		settings.GitHub = form.GitHub
		settings.GitHub.Enabled = settings.GitHub.Path != ""
	case "gist":
		settings.Gist = form.Gist
		settings.Gist.Enabled = settings.Gist.GistID != "" && settings.Gist.Filename != ""
	case "telegram":
		settings.Telegram = form.Telegram
		settings.Telegram.Enabled = true
	default:
		return fmt.Errorf("未知发布目标")
	}
	configured := map[string]bool{
		"cloudflare": settings.Cloudflare.Enabled,
		"github":     settings.GitHub.Enabled,
		"gist":       settings.Gist.Enabled,
		"telegram":   settings.Telegram.Enabled,
	}[target]
	if !configured {
		return fmt.Errorf("当前模式未配置该发布目标")
	}
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

func (a *App) StartRun(request runengine.StartRequest) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.manager.Current() != "" {
		return "", runengine.ErrAlreadyRunning
	}
	settings := a.store.Settings()
	if err := settings.Validate(); err != nil {
		return "", err
	}
	a.engine.Dependencies.Fetcher = newTrackingFetcher(a.store, settings)
	a.engine.Dependencies.TCP = probe.TCPProber{Timeout: settings.ConnectTimeout()}
	a.engine.Dependencies.HTTPS = probe.HTTPSProber{ConnectTimeout: settings.ConnectTimeout(), RequestTimeout: settings.RequestTimeout()}
	a.engine.Dependencies.Bandwidth = probe.BandwidthProber{ConnectTimeout: settings.ConnectTimeout(), TotalTimeout: settings.BandwidthTimeout(), MaxBytes: settings.MaxDownloadBytes, URL: settings.BandwidthTestURL}
	a.engine.Dependencies.QUIC = probe.QUICProber{Timeout: settings.ConnectTimeout(), WorkerHost: settings.EffectiveVerificationHost()}
	a.engine.Dependencies.HTTP3 = probe.HTTP3Prober{ConnectTimeout: settings.ConnectTimeout(), RequestTimeout: settings.RequestTimeout(), WorkerHost: settings.EffectiveVerificationHost()}
	a.engine.Dependencies.H3Bandwidth = probe.H3BandwidthProber{ConnectTimeout: settings.ConnectTimeout(), TotalTimeout: settings.BandwidthTimeout(), MaxBytes: settings.MaxDownloadBytes, URL: settings.EffectiveBandwidthTestURL()}
	stage := request.StartStage
	if stage == "" {
		stage = runengine.StageSource
	}
	if !runengine.ValidStartStage(stage) {
		return "", fmt.Errorf("未知测速起点")
	}
	if settings.RequireH3 && (stage == runengine.StageTCP || stage == runengine.StageHTTPS) || !settings.RequireH3 && (stage == runengine.StageQUIC || stage == runengine.StageHTTP3) {
		return "", fmt.Errorf("测速阶段与模式不匹配")
	}
	network := detectNetwork()
	networkSnapshot := runengine.NetworkSnapshot{Interface: network.Interface, IPv4: network.IPv4}
	var base *runengine.Checkpoint
	retained := []model.ProbeResult{}
	if stage == runengine.StageSource && settings.RetainPreviousResults {
		for _, summary := range a.store.History() {
			if summary.State == "completed" && model.NormalizeMode(summary.Mode) == settings.ProbeMode() && (!settings.RequireH3 || summary.WorkerHost == settings.EffectiveVerificationHost()) {
				retained = summary.Results
				break
			}
		}
	}

	if stage != runengine.StageSource {
		checkpoint, ok, err := a.store.Checkpoint()
		if err != nil {
			return "", fmt.Errorf("无法读取阶段检查点：%w", err)
		}
		if !ok || request.BaseRunID == "" || checkpoint.RunID != request.BaseRunID {
			return "", fmt.Errorf("阶段检查点已变化，请刷新后重试")
		}
		currentRunID := a.latestRunID()
		info := runengine.BuildResumeInfo(checkpoint, currentRunID, settings, a.store.Sources(), networkSnapshot, time.Now())
		var selected *runengine.ResumeOption
		for index := range info.Options {
			if info.Options[index].Stage == stage {
				selected = &info.Options[index]
				break
			}
		}
		if selected == nil || !selected.Available {
			return "", fmt.Errorf("当前检查点不能从该阶段重测")
		}
		if len(selected.Warnings) > 0 && !request.AllowStaleCheckpoint {
			return "", fmt.Errorf("阶段检查点需要确认：%s", strings.Join(selected.Warnings, "；"))
		}
		base = &checkpoint
	}
	input := runengine.RunInput{Sources: a.store.Sources(), Retained: retained, Plan: runengine.RunPlan{StartStage: stage, Base: base, Network: networkSnapshot, Save: a.store.SaveCheckpoint}}
	return a.manager.Start(a.appContext(), a.engine, settings, input, func(progress model.RunProgress) {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "run:progress", progress)
		}
	}, func(summary model.RunSummary) {
		_ = a.store.AddHistory(summary)
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "run:complete", summary)
		}
		if summary.State == "completed" {
			_ = a.queue.Enqueue(a.appContext(), summary, a.store.PublishSettings(), "all")
		}
	})
}

func (a *App) latestRunID() string {
	return a.store.CurrentResultID()
}

func (a *App) resumeInfo(network NetworkInfo) runengine.ResumeInfo {
	checkpoint, ok, err := a.store.Checkpoint()
	if err != nil {
		return runengine.ResumeInfo{Options: []runengine.ResumeOption{}, Error: "无法读取阶段检查点：" + err.Error()}
	}
	if !ok {
		return runengine.ResumeInfo{Options: []runengine.ResumeOption{}}
	}
	snapshot := runengine.NetworkSnapshot{Interface: network.Interface, IPv4: network.IPv4}
	return runengine.BuildResumeInfo(checkpoint, a.latestRunID(), a.store.Settings(), a.store.Sources(), snapshot, time.Now())
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
