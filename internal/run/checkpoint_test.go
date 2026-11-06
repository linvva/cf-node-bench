package run

import (
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
		RunID: "current", UpdatedAt: now, Settings: settings,
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
