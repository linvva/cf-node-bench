package run

import (
	"strings"
	"testing"
	"time"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
	"github.com/linvva/cf-node-bench/internal/source"
)

func TestBuildResumeInfoRequiresCurrentCompleteData(t *testing.T) {
	settings := config.DefaultSettings()
	now := time.Now()
	checkpoint := Checkpoint{
		HTTPSelectionComplete: true,
		RunID:                 "current", UpdatedAt: now, Settings: settings,
		Candidates:       []model.Candidate{{IP: "1.1.1.1", Port: 443}},
		TCPResults:       []model.ProbeResult{{Candidate: model.Candidate{IP: "1.1.1.1", Port: 443}}},
		HTTPSResults:     []model.ProbeResult{{Candidate: model.Candidate{IP: "1.1.1.1", Port: 443}}},
		BandwidthResults: []model.ProbeResult{{Candidate: model.Candidate{IP: "1.1.1.1", Port: 443}}},
		Stages: []model.StageProgress{
			{Name: "source", State: "completed"}, {Name: "tcp", State: "completed"}, {Name: "https", State: "completed"},
			{Name: "retained", State: "skipped"}, {Name: "bandwidth", State: "completed"},
		},
		CompletedAt: map[string]time.Time{"source": now, "tcp": now, "https": now, "retained": now, "bandwidth": now},
	}
	info := BuildResumeInfo(checkpoint, "current", settings, nil, NetworkSnapshot{}, now)
	if len(info.Options) != 4 {
		t.Fatalf("resume option count = %d", len(info.Options))
	}
	for _, option := range info.Options {
		if !option.Available {
			t.Fatalf("stage %s should be available: %+v", option.Stage, option)
		}
	}
	if other := BuildResumeInfo(checkpoint, "other", settings, nil, NetworkSnapshot{}, now); other.BaseRunID != "" || len(other.Options) != 0 {
		t.Fatalf("stale run checkpoint was exposed: %+v", other)
	}
}

func TestEarlyStoppedCheckpointRequiresFreshSamplesAfterSettingsChange(t *testing.T) {
	for _, measuredStage := range []string{StageTCP, StageHTTPS, StageQUIC, StageHTTP3} {
		for _, change := range []string{"lower-threshold", "higher-threshold", "more-samples", "fewer-samples", "unchanged", "complete-samples"} {
			t.Run(measuredStage+"/"+change, func(t *testing.T) {
				original := config.DefaultSettings()
				original.RequireH3 = measuredStage == StageQUIC || measuredStage == StageHTTP3
				original.ConnectionMinSuccessRate, original.HTTPMinSuccessRate = 0.8, 0.8
				checkpoint := reusableCheckpoint(original)
				checkpoint.QUICResults = []model.ProbeResult{{Candidate: checkpoint.Candidates[0], Endpoints: []model.EndpointResult{{IP: "1.1.1.1"}}}}
				checkpoint.HTTP3Results = []model.ProbeResult{{Candidate: checkpoint.Candidates[0], Endpoints: []model.EndpointResult{{IP: "1.1.1.1"}}}}
				checkpoint.Stages = append(checkpoint.Stages, model.StageProgress{Name: StageQUIC, State: "completed"}, model.StageProgress{Name: StageHTTP3, State: "completed"})
				checkpoint.StageSettings = map[string]config.Settings{StageTCP: original, StageHTTPS: original, StageQUIC: original, StageHTTP3: original, StageBandwidth: original}
				stats := &checkpoint.TCPResults[0].TCP
				switch measuredStage {
				case StageHTTPS:
					stats = &checkpoint.HTTPSResults[0].HTTPS
				case StageQUIC:
					stats = &checkpoint.QUICResults[0].Endpoints[0].QUIC
				case StageHTTP3:
					stats = &checkpoint.HTTP3Results[0].Endpoints[0].HTTP3
				}
				*stats = model.ProbeStats{Attempts: 1, Skipped: 2}
				current := original
				threshold, count := &current.ConnectionMinSuccessRate, &current.ConnectionProbeCount
				if measuredStage == StageHTTPS || measuredStage == StageHTTP3 {
					threshold, count = &current.HTTPMinSuccessRate, &current.HTTPProbeCount
				}
				switch change {
				case "lower-threshold":
					*threshold = 2.0 / 3
				case "higher-threshold":
					*threshold = 1
				case "more-samples":
					*count++
				case "fewer-samples":
					*count--
				case "complete-samples":
					stats.Attempts, stats.Skipped = 3, 0
					*threshold = 2.0 / 3
				}
				// Resumed runs record current settings, while measurements retain their own snapshot.
				checkpoint.Settings = current
				blocked := change == "lower-threshold" || change == "more-samples" || change == "fewer-samples"
				info := BuildResumeInfo(checkpoint, checkpoint.RunID, current, nil, NetworkSnapshot{}, time.Now())
				pastMeasured := false
				for _, option := range info.Options {
					wantAvailable := !blocked || !pastMeasured
					if option.Available != wantAvailable {
						t.Fatalf("%s availability = %v, want %v: %+v", option.Stage, option.Available, wantAvailable, option)
					}
					if !wantAvailable && !strings.Contains(strings.Join(option.Warnings, " "), "提前淘汰") {
						t.Fatalf("missing non-bypassable sample warning: %+v", option)
					}
					if option.Stage == measuredStage {
						pastMeasured = true
					}
				}
				wantStart := StageRanking
				if blocked {
					wantStart = measuredStage
				}
				if got := runStartStage(StageRanking, current, &checkpoint); got != wantStart {
					t.Fatalf("direct engine start = %s, want %s", got, wantStart)
				}
			})
		}
	}
}

func TestBuildResumeInfoWarnsWithoutInvalidating(t *testing.T) {
	settings := config.DefaultSettings()
	sourceSnapshot := []source.HTTPSource{{ID: "source", URL: "https://old.test", Enabled: true}}
	now := time.Now()
	checkpoint := Checkpoint{
		RunID: "current", UpdatedAt: now.Add(-31 * time.Minute), Settings: settings, Sources: sourceSnapshot,
		TCPResults: []model.ProbeResult{{Candidate: model.Candidate{IP: "1.1.1.1", Port: 443}}},
		Stages:     []model.StageProgress{{Name: "tcp", State: "completed"}}, CompletedAt: map[string]time.Time{"tcp": now.Add(-31 * time.Minute)},
		StageSettings: map[string]config.Settings{"tcp": settings}, StageNetworks: map[string]NetworkSnapshot{"tcp": {Interface: "old", IPv4: "10.0.0.2"}},
	}
	current := settings
	current.ConnectionProbeCount++
	info := BuildResumeInfo(checkpoint, "current", current, []source.HTTPSource{{ID: "source", URL: "https://new.test", Enabled: true}}, NetworkSnapshot{Interface: "new", IPv4: "192.168.1.2"}, now)
	option := info.Options[1]
	if !option.Available || len(option.Warnings) < 4 {
		t.Fatalf("expected non-blocking stale warnings: %+v", option)
	}
}

func TestBuildResumeInfoWarnsWhenPreviouslyUnavailableNetworkAppears(t *testing.T) {
	settings := config.DefaultSettings()
	now := time.Now()
	checkpoint := Checkpoint{
		RunID: "current", UpdatedAt: now, Settings: settings,
		Candidates:    []model.Candidate{{IP: "1.1.1.1", Port: 443}},
		Stages:        []model.StageProgress{{Name: "source", State: "completed"}},
		CompletedAt:   map[string]time.Time{"source": now},
		StageNetworks: map[string]NetworkSnapshot{"source": {}},
	}
	info := BuildResumeInfo(checkpoint, "current", settings, nil, NetworkSnapshot{Interface: "eth0", IPv4: "192.168.1.2"}, now)
	if len(info.Options[0].Warnings) != 1 || info.Options[0].Warnings[0] != "网络接口或本机 IPv4 已变化" {
		t.Fatalf("network transition warning missing: %+v", info.Options[0])
	}
}
