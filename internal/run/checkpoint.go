package run

import (
	"slices"
	"time"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
	"github.com/linvva/cf-node-bench/internal/source"
)

const CheckpointVersion = 1

const (
	StageSource    = "source"
	StageTCP       = "tcp"
	StageHTTPS     = "https"
	StageBandwidth = "bandwidth"
	StageRanking   = "ranking"
	StageQUIC      = "quic"
	StageHTTP3     = "http3"
)

type NetworkSnapshot struct {
	Interface string `json:"interface"`
	IPv4      string `json:"ipv4"`
}

func BuildResumeInfo(checkpoint Checkpoint, currentRunID string, settings config.Settings, sources []source.HTTPSource, network NetworkSnapshot, now time.Time) ResumeInfo {
	if settings.RequireH3 {
		return buildH3ResumeInfo(checkpoint, currentRunID, settings, sources, network, now)
	}
	info := ResumeInfo{Options: []ResumeOption{}}
	if checkpoint.RunID == "" || checkpoint.RunID != currentRunID {
		return info
	}
	info.BaseRunID = checkpoint.RunID
	info.UpdatedAt = checkpoint.UpdatedAt
	candidateCount := countIf(checkpoint.Candidates, func(c model.Candidate) bool { return c.AddressType != model.AddressDomain })
	definitions := []struct {
		stage, requiredStage string
		input                int
		dataAvailable        bool
	}{
		{StageTCP, "source", candidateCount, candidateCount > 0},
		{StageHTTPS, "tcp", len(checkpoint.TCPResults), len(checkpoint.TCPResults) > 0},
		{StageBandwidth, "retained", len(checkpoint.HTTPSResults), len(checkpoint.HTTPSResults) > 0},
		{StageRanking, "bandwidth", len(checkpoint.BandwidthResults), len(checkpoint.BandwidthResults) > 0},
	}
	for _, definition := range definitions {
		_, completed := checkpoint.Stage(definition.requiredStage)
		measuredAt := checkpoint.CompletedAt[definition.requiredStage]
		option := ResumeOption{Stage: definition.stage, Available: completed && definition.dataAvailable, Input: definition.input, MeasuredAt: measuredAt, Warnings: []string{}}
		if checkpoint.Settings.RequireH3 != settings.RequireH3 {
			option.Available = option.Available && definition.stage == StageTCP
			option.Warnings = append(option.Warnings, "H3 要求已变化，只能复用候选并从 TCP 重新探测")
		}
		if restart := earlyStoppedRestartStage(checkpoint, settings, definition.stage); option.Available && restart != "" {
			option.Available = false
			option.Warnings = append(option.Warnings, earlyStoppedRestartWarning(restart))
		}
		if option.Available {
			option.Warnings = append(option.Warnings, checkpointWarnings(checkpoint, definition.stage, settings, sources, network, now)...)
		}
		info.Options = append(info.Options, option)
	}
	return info
}

func checkpointWarnings(checkpoint Checkpoint, stage string, settings config.Settings, sources []source.HTTPSource, network NetworkSnapshot, now time.Time) []string {
	warnings := []string{}
	if !checkpoint.UpdatedAt.IsZero() && now.Sub(checkpoint.UpdatedAt) > 30*time.Minute {
		warnings = append(warnings, "检查点已超过 30 分钟，复用指标可能不能代表当前网络")
	}
	requiredStage := map[string]string{StageTCP: "source", StageHTTPS: "tcp", StageBandwidth: "retained", StageRanking: "bandwidth"}[stage]
	measuredNetwork, networkRecorded := checkpoint.StageNetworks[requiredStage]
	if networkRecorded && (measuredNetwork.Interface != network.Interface || measuredNetwork.IPv4 != network.IPv4) {
		warnings = append(warnings, "网络接口或本机 IPv4 已变化")
	}
	if !checkpoint.LocalFiles && !sameEnabledSources(checkpoint.Sources, sources) {
		warnings = append(warnings, "启用的数据源已变化，候选仍来自原检查点")
	}
	if stage == StageHTTPS || stage == StageBandwidth || stage == StageRanking {
		previous := stageSettings(checkpoint, "tcp")
		if previous.ConnectTimeoutMS != settings.ConnectTimeoutMS || previous.ConnectionProbeCount != settings.ConnectionProbeCount || !sameCandidateFilters(previous, settings) {
			warnings = append(warnings, "TCP 测量或候选过滤设置已变化")
		}
	}
	if stage == StageBandwidth || stage == StageRanking {
		previous := stageSettings(checkpoint, "https")
		if previous.RequestTimeoutMS != settings.RequestTimeoutMS || previous.HTTPProbeCount != settings.HTTPProbeCount || previous.HTTPCandidateCount != settings.HTTPCandidateCount {
			warnings = append(warnings, "HTTPS 测量或候选池设置已变化")
		}
	}
	if stage == StageRanking {
		previous := stageSettings(checkpoint, "bandwidth")
		if previous.BandwidthTimeoutMS != settings.BandwidthTimeoutMS || previous.MaxDownloadBytes != settings.MaxDownloadBytes || previous.BandwidthTestURL != settings.BandwidthTestURL || previous.BandwidthCandidates != settings.BandwidthCandidates {
			warnings = append(warnings, "带宽测量设置已变化")
		}
	}
	return warnings
}

func stageSettings(checkpoint Checkpoint, stage string) config.Settings {
	if value, ok := checkpoint.StageSettings[stage]; ok {
		return value
	}
	return checkpoint.Settings
}

func sameCandidateFilters(left, right config.Settings) bool {
	leftPorts := append([]int(nil), left.AllowedPorts...)
	rightPorts := append([]int(nil), right.AllowedPorts...)
	leftAllowed := append([]string(nil), left.AllowedCountries...)
	rightAllowed := append([]string(nil), right.AllowedCountries...)
	leftBlocked := append([]string(nil), left.BlockedCountries...)
	rightBlocked := append([]string(nil), right.BlockedCountries...)
	slices.Sort(leftPorts)
	slices.Sort(rightPorts)
	slices.Sort(leftAllowed)
	slices.Sort(rightAllowed)
	slices.Sort(leftBlocked)
	slices.Sort(rightBlocked)
	return slices.Equal(leftPorts, rightPorts) && slices.Equal(leftAllowed, rightAllowed) && slices.Equal(leftBlocked, rightBlocked)
}

func sameEnabledSources(left, right []source.HTTPSource) bool {
	type identity struct{ ID, URL string }
	collect := func(values []source.HTTPSource) []identity {
		result := []identity{}
		for _, value := range values {
			if value.Enabled {
				result = append(result, identity{ID: value.ID, URL: value.URL})
			}
		}
		slices.SortFunc(result, func(a, b identity) int {
			if a.ID < b.ID {
				return -1
			}
			if a.ID > b.ID {
				return 1
			}
			if a.URL < b.URL {
				return -1
			}
			if a.URL > b.URL {
				return 1
			}
			return 0
		})
		return result
	}
	return slices.Equal(collect(left), collect(right))
}

type Checkpoint struct {
	LocalFiles       bool                       `json:"localFiles,omitempty"`
	QUICResults      []model.ProbeResult        `json:"quicResults,omitempty"`
	HTTP3Results     []model.ProbeResult        `json:"http3Results,omitempty"`
	RetainedKeys     []string                   `json:"retainedKeys,omitempty"`
	Version          int                        `json:"version"`
	RunID            string                     `json:"runId"`
	ParentRunID      string                     `json:"parentRunId,omitempty"`
	UpdatedAt        time.Time                  `json:"updatedAt"`
	Network          NetworkSnapshot            `json:"network"`
	Settings         config.Settings            `json:"settings"`
	StageSettings    map[string]config.Settings `json:"stageSettings"`
	StageNetworks    map[string]NetworkSnapshot `json:"stageNetworks"`
	Sources          []source.HTTPSource        `json:"sources"`
	Candidates       []model.Candidate          `json:"candidates"`
	TCPResults       []model.ProbeResult        `json:"tcpResults"`
	HTTPSResults     []model.ProbeResult        `json:"httpsResults"`
	BandwidthResults []model.ProbeResult        `json:"bandwidthResults"`
	Stages           []model.StageProgress      `json:"stages"`
	CompletedAt      map[string]time.Time       `json:"completedAt"`
}

type ResumeOption struct {
	Stage      string    `json:"stage"`
	Available  bool      `json:"available"`
	Input      int       `json:"input"`
	MeasuredAt time.Time `json:"measuredAt,omitempty"`
	Warnings   []string  `json:"warnings"`
}

type ResumeInfo struct {
	BaseRunID string         `json:"baseRunId,omitempty"`
	UpdatedAt time.Time      `json:"updatedAt,omitempty"`
	Options   []ResumeOption `json:"options"`
	Error     string         `json:"error,omitempty"`
}

type StartRequest struct {
	StartStage           string `json:"startStage"`
	BaseRunID            string `json:"baseRunId,omitempty"`
	AllowStaleCheckpoint bool   `json:"allowStaleCheckpoint"`
}

type RunPlan struct {
	StartStage string
	Base       *Checkpoint
	Network    NetworkSnapshot
	Save       func(Checkpoint) error
}

func ValidStartStage(stage string) bool {
	switch stage {
	case StageSource, StageTCP, StageHTTPS, StageQUIC, StageHTTP3, StageBandwidth, StageRanking:
		return true
	default:
		return false
	}
}

// Reusing candidates is safe across protocol or verification-host changes. Measured
// results are not: enforce this here as well as in the resume options shown by the UI.
func runStartStage(stage string, settings config.Settings, base *Checkpoint) string {
	if !ValidStartStage(stage) || stage == StageSource || base == nil {
		return StageSource
	}
	connectionStage := StageTCP
	if settings.RequireH3 {
		connectionStage = StageQUIC
	}
	changed := base.Settings.RequireH3 != settings.RequireH3
	if settings.RequireH3 {
		changed = changed || stage == StageTCP || stage == StageHTTPS
		for _, measuredStage := range []string{StageQUIC, StageHTTP3, StageBandwidth} {
			if stageSettings(*base, measuredStage).EffectiveVerificationHost() != settings.EffectiveVerificationHost() {
				changed = true
			}
		}
	} else {
		changed = changed || stage == StageQUIC || stage == StageHTTP3
	}
	if changed {
		if _, complete := base.Stage(StageSource); complete && len(base.Candidates) > 0 {
			return connectionStage
		}
		return StageSource
	}
	if restart := earlyStoppedRestartStage(*base, settings, stage); restart != "" {
		return restart
	}
	return stage
}

// Early rejection only proves failure under the settings that made that decision.
// A relaxed threshold or different sample count requires completing a fresh probe.
func earlyStoppedRestartStage(c Checkpoint, settings config.Settings, start string) string {
	order := []string{StageTCP, StageHTTPS, StageBandwidth, StageRanking}
	if settings.RequireH3 {
		order[0], order[1] = StageQUIC, StageHTTP3
	}
	startIndex := slices.Index(order, start)
	if startIndex < 1 {
		return ""
	}
	for index, stage := range order[:min(startIndex, 2)] {
		previous := stageSettings(c, stage)
		changed := settings.ConnectionMinSuccessRate < previous.ConnectionMinSuccessRate || settings.ConnectionProbeCount != previous.ConnectionProbeCount
		if index == 1 {
			changed = settings.HTTPMinSuccessRate < previous.HTTPMinSuccessRate || settings.HTTPProbeCount != previous.HTTPProbeCount
		}
		if changed && checkpointHasSkippedSamples(c, stage) {
			return stage
		}
	}
	return ""
}

func checkpointHasSkippedSamples(c Checkpoint, stage string) bool {
	results := c.TCPResults
	switch stage {
	case StageHTTPS:
		results = c.HTTPSResults
	case StageQUIC:
		results = c.QUICResults
	case StageHTTP3:
		results = c.HTTP3Results
	}
	for _, result := range results {
		if stage == StageTCP && result.TCP.Skipped > 0 || stage == StageHTTPS && result.HTTPS.Skipped > 0 ||
			stage == StageQUIC && result.QUIC.Skipped > 0 || stage == StageHTTP3 && result.HTTP3.Skipped > 0 {
			return true
		}
		for _, endpoint := range result.Endpoints {
			if stage == StageQUIC && endpoint.QUIC.Skipped > 0 || stage == StageHTTP3 && endpoint.HTTP3.Skipped > 0 {
				return true
			}
		}
	}
	return false
}

func earlyStoppedRestartWarning(stage string) string {
	label := map[string]string{StageTCP: "TCP", StageHTTPS: "HTTPS", StageQUIC: "QUIC", StageHTTP3: "HTTP/3"}[stage]
	return label + " 存在提前淘汰的未完成采样；降低成功率门槛或改变采样次数后，必须从该阶段重新探测"
}

func (c Checkpoint) Stage(name string) (model.StageProgress, bool) {
	for _, stage := range c.Stages {
		if stage.Name == name && (stage.State == "completed" || stage.State == "reused" || stage.State == "skipped") {
			return stage, true
		}
	}
	return model.StageProgress{}, false
}

func buildH3ResumeInfo(c Checkpoint, currentID string, settings config.Settings, sources []source.HTTPSource, network NetworkSnapshot, now time.Time) ResumeInfo {
	info := ResumeInfo{Options: []ResumeOption{}}
	if c.RunID == "" || c.RunID != currentID {
		return info
	}
	info.BaseRunID, info.UpdatedAt = c.RunID, c.UpdatedAt
	for _, definition := range []struct {
		stage, required string
		count           int
	}{
		{StageQUIC, "source", len(c.Candidates)}, {StageHTTP3, StageQUIC, len(c.QUICResults)},
		{StageBandwidth, StageHTTP3, len(c.HTTP3Results)}, {StageRanking, StageBandwidth, len(c.BandwidthResults)},
	} {
		_, complete := c.Stage(definition.required)
		option := ResumeOption{Stage: definition.stage, Available: complete && definition.count > 0, Input: definition.count, MeasuredAt: c.CompletedAt[definition.required], Warnings: []string{}}
		measured := stageSettings(c, definition.required)
		if c.Settings.RequireH3 != settings.RequireH3 {
			option.Available = option.Available && definition.stage == StageQUIC
			option.Warnings = append(option.Warnings, "H3 要求已变化，只能复用候选并从 QUIC 重新探测")
		}
		if definition.stage != StageQUIC && measured.EffectiveVerificationHost() != settings.EffectiveVerificationHost() {
			option.Available = false
			option.Warnings = append(option.Warnings, "验证域名已变化，必须从 QUIC 重新探测")
		}
		if restart := earlyStoppedRestartStage(c, settings, definition.stage); option.Available && restart != "" {
			option.Available = false
			option.Warnings = append(option.Warnings, earlyStoppedRestartWarning(restart))
		}
		if option.Available {
			if now.Sub(c.UpdatedAt) > 30*time.Minute {
				option.Warnings = append(option.Warnings, "检查点已超过 30 分钟，DNS 和指标可能已变化")
			}
			if previous, ok := c.StageNetworks[definition.required]; ok && previous != network {
				option.Warnings = append(option.Warnings, "网络接口或本机 IPv4 已变化")
			}
			if !c.LocalFiles && !sameEnabledSources(c.Sources, sources) {
				option.Warnings = append(option.Warnings, "启用的数据源已变化，候选仍来自原检查点")
			}
			if definition.stage != StageQUIC {
				previous := stageSettings(c, StageQUIC)
				if previous.H3ConnectTimeoutMS != settings.H3ConnectTimeoutMS || previous.ConnectionProbeCount != settings.ConnectionProbeCount || !sameCandidateFilters(previous, settings) {
					option.Warnings = append(option.Warnings, "QUIC 测量或候选过滤设置已变化")
				}
			}
			if definition.stage == StageBandwidth || definition.stage == StageRanking {
				previous := stageSettings(c, StageHTTP3)
				if previous.HTTPProbeCount != settings.HTTPProbeCount || previous.H3RequestTimeoutMS != settings.H3RequestTimeoutMS || previous.HTTPCandidateCount != settings.HTTPCandidateCount {
					option.Warnings = append(option.Warnings, "HTTP/3 测量设置已变化")
				}
			}
			if definition.stage == StageRanking {
				previous := stageSettings(c, StageBandwidth)
				if previous.H3BandwidthTestURL != settings.H3BandwidthTestURL || previous.MaxDownloadBytes != settings.MaxDownloadBytes || previous.BandwidthTimeoutMS != settings.BandwidthTimeoutMS || previous.BandwidthCandidates != settings.BandwidthCandidates {
					option.Warnings = append(option.Warnings, "H3 带宽测量设置已变化")
				}
			}
			// DNS is deliberately a snapshot for these stages; do not trigger network requests while ranking.
			if definition.stage != StageQUIC {
				option.Warnings = append(option.Warnings, "复用本轮 IPv4 解析快照；如需刷新 DNS，请从 QUIC 开始")
			}
		}
		info.Options = append(info.Options, option)
	}
	return info
}
