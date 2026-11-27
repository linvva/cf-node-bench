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
	"github.com/wailsapp/wails/v3/pkg/application"
)

type NetworkInfo struct {
	Interface string `json:"interface"`
	IPv4      string `json:"ipv4"`
	Status    string `json:"status"`
}

type Bootstrap struct {
	Version            string               `json:"version"`
	Settings           config.Settings      `json:"settings"`
	PublishSettings    publish.SettingsView `json:"publishSettings"`
	Sources            []source.HTTPSource  `json:"sources"`
	History            []model.RunSummary   `json:"history"`
	PinnedNodes        []model.PinnedNode   `json:"pinnedNodes"`
	Network            NetworkInfo          `json:"network"`
	Current            string               `json:"currentRunId,omitempty"`
	Resume             runengine.ResumeInfo `json:"resume"`
	CurrentResultRunID string               `json:"currentResultRunId,omitempty"`
	MigrationNotice    string               `json:"migrationNotice,omitempty"`
}

type App struct {
	ctx        context.Context
	desktop    *application.App
	updates    *updateController
	store      *storage.Store
	manager    runengine.Manager
	engine     runengine.Engine
	publish    *publish.Service
	queue      *publish.Coordinator
	mu         sync.Mutex
	restarting bool
	runPending bool
}

func NewApp(store *storage.Store) *App {
	settings := store.Settings()
	app := &App{store: store, publish: publish.NewService(), engine: runengine.Engine{Dependencies: runengine.Dependencies{
		Fetcher: newTrackingFetcher(store, settings),
		TCP:     probe.TCPProber{Timeout: settings.ConnectTimeout()},
		HTTPS:   probe.HTTPSProber{ConnectTimeout: settings.ConnectTimeout(), RequestTimeout: settings.RequestTimeout()},
		Bandwidth: probe.BandwidthProber{ConnectTimeout: settings.ConnectTimeout(), TotalTimeout: settings.BandwidthTimeout(), MaxBytes: settings.MaxDownloadBytes, URL: settings.BandwidthTestURL,
			Threshold: probe.BandwidthThreshold{MinMbps: settings.MinBandwidthMbps, LineMbps: settings.LineBandwidthMbps}},
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

func (a *App) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	a.ctx = ctx
	return nil
}

func (a *App) Bootstrap() Bootstrap {
	network := detectNetwork()
	return Bootstrap{Version: buildVersion, Settings: a.store.Settings(), PublishSettings: a.store.PublishSettingsView(), Sources: a.store.Sources(), History: a.store.History(), PinnedNodes: a.store.PinnedNodes(), Network: network, Current: a.manager.Current(), Resume: a.resumeInfo(network), MigrationNotice: a.store.MigrationNotice(), CurrentResultRunID: a.store.CurrentResultID()}
}

func (a *App) SetNodePinned(runID, candidateKey string, pinned bool) ([]model.PinnedNode, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.manager.Current() != "" {
		return nil, fmt.Errorf("测速运行中不能修改固化节点")
	}
	summary, ok := a.store.HistoryByID(runID)
	if !ok {
		return nil, fmt.Errorf("未找到测速历史")
	}
	for _, result := range summary.Results {
		if result.Candidate.Key() == candidateKey {
			return a.store.SetNodePinned(summary.Mode, result.Candidate, pinned)
		}
	}
	return nil, fmt.Errorf("未找到测速节点")
}

func (a *App) pinnedCandidates() []model.Candidate {
	mode := a.store.Settings().ProbeMode()
	candidates := []model.Candidate{}
	for _, node := range a.store.PinnedNodes() {
		if node.Mode == mode {
			candidates = append(candidates, node.Candidate)
		}
	}
	return candidates
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
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.restarting {
		return fmt.Errorf("应用正在重启以完成更新")
	}
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
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.restarting {
		return fmt.Errorf("应用正在重启以完成更新")
	}
	if target != "all" && target != "cloudflare" && target != "github" && target != "gist" && target != "telegram" {
		return fmt.Errorf("未知发布目标")
	}
	summary, ok := a.store.HistoryByID(runID)
	if !ok {
		return fmt.Errorf("未找到测速历史")
	}
	if summary.State != "completed" && !(summary.State == "cancelled" && summary.PartialResults && len(summary.Results) > 0) {
		return fmt.Errorf("只有已完成的测速或取消时保留的部分结果可以发布")
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
	return a.startRun(request, nil)
}

func (a *App) SelectLocalFiles() ([]string, error) {
	if a.manager.Current() != "" {
		return nil, runengine.ErrAlreadyRunning
	}
	if a.desktop == nil {
		return nil, fmt.Errorf("文件选择仅在桌面应用中可用")
	}
	return a.desktop.Dialog.OpenFile().
		SetTitle("选择节点文件开始测速").
		AddFilter("节点文件 (*.csv, *.txt, *.json)", "*.csv;*.txt;*.json").
		PromptForMultipleSelection()
}

func (a *App) StartLocalFiles(paths []string) (string, error) {
	return a.StartLocalFilesWithStrategy(paths, false)
}

func (a *App) StartLocalFilesWithStrategy(paths []string, quick bool) (string, error) {
	if len(paths) == 0 {
		return "", fmt.Errorf("请选择节点文件")
	}
	return a.startRun(runengine.StartRequest{StartStage: runengine.StageSource, Quick: quick}, paths)
}

func downloadCheckProbe(settings config.Settings) runengine.BandwidthProbe {
	budget := min(settings.BandwidthTimeout(), 3*time.Second)
	if settings.RequireH3 {
		return probe.H3BandwidthProber{ConnectTimeout: settings.ConnectTimeout(), TotalTimeout: budget, MaxBytes: runengine.DownloadCheckBytes, URL: settings.EffectiveBandwidthTestURL()}
	}
	return probe.BandwidthProber{ConnectTimeout: settings.ConnectTimeout(), TotalTimeout: budget, MaxBytes: runengine.DownloadCheckBytes, URL: settings.EffectiveBandwidthTestURL()}
}

func (a *App) TestDownloadAddress(settings config.Settings) (model.DownloadCheck, error) {
	settings.Normalize()
	if err := settings.Validate(); err != nil {
		return model.DownloadCheck{}, err
	}
	if a.manager.Current() != "" {
		return model.DownloadCheck{}, runengine.ErrAlreadyRunning
	}
	var candidates []model.Candidate
	add := func(results []model.ProbeResult) {
		for _, result := range results {
			if !settings.AllowsPort(result.Candidate.Port) || !settings.AllowsCountry(result.Candidate.Country) {
				continue
			}
			if settings.RequireH3 && result.HTTP3.SuccessRate >= settings.HTTPMinSuccessRate {
				for _, endpoint := range result.Endpoints {
					candidates = append(candidates, model.Candidate{IP: endpoint.IP, Port: result.Candidate.Port})
				}
			} else if !settings.RequireH3 && result.HTTPS.SuccessRate >= settings.HTTPMinSuccessRate {
				candidates = append(candidates, result.Candidate)
			}
		}
	}
	checkpoint, ok, err := a.store.Checkpoint()
	if err == nil && ok && checkpoint.Settings.RequireH3 == settings.RequireH3 {
		if settings.RequireH3 && checkpoint.Settings.EffectiveVerificationHost() == settings.EffectiveVerificationHost() {
			add(checkpoint.HTTP3Results)
		} else if !settings.RequireH3 {
			add(checkpoint.HTTPSResults)
		}
	}
	for _, summary := range a.store.History() {
		if summary.State == "completed" && model.NormalizeMode(summary.Mode) == settings.ProbeMode() && (!settings.RequireH3 || summary.WorkerHost == settings.EffectiveVerificationHost()) {
			add(summary.Results)
			if len(candidates) > 0 {
				break
			}
		}
	}
	return runengine.TestDownload(a.appContext(), settings, candidates, nil, downloadCheckProbe(settings)), nil
}

func (a *App) startRun(request runengine.StartRequest, paths []string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.restarting {
		return "", fmt.Errorf("应用正在重启以完成更新")
	}
	if a.runPending || a.manager.Current() != "" {
		return "", runengine.ErrAlreadyRunning
	}
	settings := a.store.Settings()
	if err := settings.Validate(); err != nil {
		return "", err
	}
	var localFiles *source.ParseResult
	if paths != nil {
		parsed, err := source.ReadLocalFiles(a.appContext(), paths, settings.RequireH3)
		if err != nil {
			return "", err
		}
		localFiles = &parsed
	}
	a.engine.Dependencies.Fetcher = newTrackingFetcher(a.store, settings)
	a.engine.Dependencies.DownloadCheck = downloadCheckProbe(settings)
	a.engine.Dependencies.TCP = probe.TCPProber{Timeout: settings.ConnectTimeout(), MinSuccessRate: settings.ConnectionMinSuccessRate}
	a.engine.Dependencies.HTTPS = probe.HTTPSProber{ConnectTimeout: settings.ConnectTimeout(), RequestTimeout: settings.RequestTimeout(), MinSuccessRate: settings.HTTPMinSuccessRate}
	bandwidthThreshold := probe.BandwidthThreshold{MinMbps: settings.MinBandwidthMbps, LineMbps: settings.LineBandwidthMbps}
	a.engine.Dependencies.Bandwidth = probe.BandwidthProber{ConnectTimeout: settings.ConnectTimeout(), TotalTimeout: settings.BandwidthTimeout(), MaxBytes: settings.MaxDownloadBytes, URL: settings.BandwidthTestURL, Threshold: bandwidthThreshold}
	a.engine.Dependencies.QUIC = probe.QUICProber{Timeout: settings.ConnectTimeout(), WorkerHost: settings.EffectiveVerificationHost(), MinSuccessRate: settings.ConnectionMinSuccessRate}
	a.engine.Dependencies.HTTP3 = probe.HTTP3Prober{ConnectTimeout: settings.ConnectTimeout(), RequestTimeout: settings.RequestTimeout(), WorkerHost: settings.EffectiveVerificationHost(), MinSuccessRate: settings.HTTPMinSuccessRate}
	a.engine.Dependencies.H3Bandwidth = probe.H3BandwidthProber{ConnectTimeout: settings.ConnectTimeout(), TotalTimeout: settings.BandwidthTimeout(), MaxBytes: settings.MaxDownloadBytes, URL: settings.EffectiveBandwidthTestURL(), Threshold: bandwidthThreshold}
	stage := request.StartStage
	if stage == "" {
		stage = runengine.StageSource
	}
	if request.ContinueBandwidth {
		stage = runengine.StageBandwidth
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
	pins := a.pinnedCandidates()
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
		info = runengine.ApplyPinnedResumeInfo(info, checkpoint, pins)
		var selected *runengine.ResumeOption
		if request.ContinueBandwidth {
			selected = runengine.DownloadResumeInfo(checkpoint, settings, networkSnapshot, a.store.Sources(), pins, time.Now())
		} else {
			for index := range info.Options {
				if info.Options[index].Stage == stage {
					selected = &info.Options[index]
					break
				}
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
	input := runengine.RunInput{LocalFiles: localFiles, Sources: a.store.Sources(), Retained: retained, Pinned: pins, Plan: runengine.RunPlan{StartStage: stage, Base: base, Network: networkSnapshot, Save: a.store.SaveCheckpoint, Quick: request.Quick && stage == runengine.StageSource, ContinueBandwidth: request.ContinueBandwidth}}
	id, err := a.manager.Start(a.appContext(), a.engine, settings, input, func(progress model.RunProgress) {
		if a.desktop != nil {
			a.desktop.Event.Emit("run:progress", progress)
		}
	}, func(summary model.RunSummary) {
		a.mu.Lock()
		defer a.mu.Unlock()
		defer func() { a.runPending = false }()
		_ = a.store.AddHistory(summary)
		if a.desktop != nil {
			a.desktop.Event.Emit("run:complete", summary)
		}
		if summary.State == "completed" && !a.restarting {
			_ = a.queue.Enqueue(a.appContext(), summary, a.store.PublishSettings(), "all")
		}
	})
	if err == nil {
		a.runPending = true
	}
	return id, err
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
	info := runengine.BuildResumeInfo(checkpoint, a.latestRunID(), a.store.Settings(), a.store.Sources(), snapshot, time.Now())
	pins := a.pinnedCandidates()
	info = runengine.ApplyPinnedResumeInfo(info, checkpoint, pins)
	info.ContinueBandwidth = runengine.DownloadResumeInfo(checkpoint, a.store.Settings(), snapshot, a.store.Sources(), pins, time.Now())
	if info.ContinueBandwidth != nil {
		info.BaseRunID = checkpoint.RunID
	}
	return info
}

func (a *App) CancelRun() bool { return a.manager.Cancel() }

func (a *App) SaveBandwidthAndStop() bool { return a.manager.SaveAndStop() }

func (a *App) CancelRunWithResults(keepResults bool) bool {
	return a.manager.CancelWithResults(keepResults)
}

func (a *App) handlePublishUpdate(update publish.Update) {
	_ = a.store.UpdatePublication(update.RunID, update.Result)
	if a.desktop != nil {
		a.desktop.Event.Emit("publish:progress", update)
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
