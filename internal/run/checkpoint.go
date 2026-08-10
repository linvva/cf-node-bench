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
)

type NetworkSnapshot struct {
	Interface string `json:"interface"`
	IPv4      string `json:"ipv4"`
}

func BuildResumeInfo(checkpoint Checkpoint, currentRunID string, settings config.Settings, sources []source.HTTPSource, network NetworkSnapshot, now time.Time) ResumeInfo {
	info := ResumeInfo{Options: []ResumeOption{}}
	if checkpoint.RunID == "" || checkpoint.RunID != currentRunID {
		return info
	}
	info.BaseRunID = checkpoint.RunID
	info.UpdatedAt = checkpoint.UpdatedAt
	definitions := []struct {
		stage, requiredStage string
		input                int
		dataAvailable        bool
	}{
		{StageTCP, "source", len(checkpoint.Candidates), len(checkpoint.Candidates) > 0},
		{StageHTTPS, "tcp", len(checkpoint.TCPResults), len(checkpoint.TCPResults) > 0},
		{StageBandwidth, "retained", len(checkpoint.HTTPSResults), len(checkpoint.HTTPSResults) > 0},
		{StageRanking, "bandwidth", len(checkpoint.BandwidthResults), len(checkpoint.BandwidthResults) > 0},
	}
	for _, definition := range definitions {
		_, completed := checkpoint.Stage(definition.requiredStage)
		measuredAt := checkpoint.CompletedAt[definition.requiredStage]
		option := ResumeOption{Stage: definition.stage, Available: completed && definition.dataAvailable, Input: definition.input, MeasuredAt: measuredAt, Warnings: []string{}}
		if option.Available {
			option.Warnings = checkpointWarnings(checkpoint, definition.stage, settings, sources, network, now)
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
	if !sameEnabledSources(checkpoint.Sources, sources) {
		warnings = append(warnings, "启用的数据源已变化，候选仍来自原检查点")
	}
	if stage == StageHTTPS || stage == StageBandwidth || stage == StageRanking {
		previous := stageSettings(checkpoint, "tcp")
		if previous.ConnectTimeoutMS != settings.ConnectTimeoutMS || previous.TCPProbeCount != settings.TCPProbeCount || !sameCandidateFilters(previous, settings) {
			warnings = append(warnings, "TCP 测量或候选过滤设置已变化")
		}
	}
	if stage == StageBandwidth || stage == StageRanking {
		previous := stageSettings(checkpoint, "https")
		if previous.RequestTimeoutMS != settings.RequestTimeoutMS || previous.HTTPSProbeCount != settings.HTTPSProbeCount || previous.TCPCandidateCount != settings.TCPCandidateCount {
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
	case StageSource, StageTCP, StageHTTPS, StageBandwidth, StageRanking:
		return true
	default:
		return false
	}
}

func (c Checkpoint) Stage(name string) (model.StageProgress, bool) {
	for _, stage := range c.Stages {
		if stage.Name == name && (stage.State == "completed" || stage.State == "reused" || stage.State == "skipped") {
			return stage, true
		}
	}
	return model.StageProgress{}, false
}
