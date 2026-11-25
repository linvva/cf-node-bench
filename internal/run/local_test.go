package run

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/linvva/cf-node-bench/internal/model"
	"github.com/linvva/cf-node-bench/internal/source"
)

type localBandwidthThresholdStub struct{ BandwidthProbe }

func (p localBandwidthThresholdStub) Probe(ctx context.Context, candidate model.Candidate) model.BandwidthStats {
	stats := p.BandwidthProbe.Probe(ctx, candidate)
	if candidate.IP != "192.0.2.1" && candidate.IP != "192.0.2.3" {
		stats.Failure = model.FailureBandwidthBelowMin
		stats.Mbps = 0
	}
	return stats
}

func TestLocalFilesContinueAfterBandwidthThresholdFailures(t *testing.T) {
	for _, h3 := range []bool{false, true} {
		engine, settings, _, connection, http, bandwidth := h3Fixture(nil)
		settings.RequireH3 = h3
		t.Run(string(settings.ProbeMode()), func(t *testing.T) {
			settings.MinBandwidthMbps = 10
			settings.HTTPCandidateCount, settings.BandwidthCandidates, settings.FinalResultCount = 1, 1, 1
			failed := model.Candidate{AddressType: model.AddressIPv4, IP: "192.0.2.2", Port: 443}
			if h3 {
				failed = model.Candidate{AddressType: model.AddressDomain, Host: "pool.test", Port: 443}
			}
			good := model.Candidate{AddressType: model.AddressIPv4, IP: "192.0.2.3", Port: 443}
			retained := model.Candidate{AddressType: model.AddressIPv4, IP: "192.0.2.4", Port: 443}
			pin := model.Candidate{AddressType: model.AddressIPv4, IP: "192.0.2.5", Port: 443}
			prober := localBandwidthThresholdStub{BandwidthProbe: bandwidth}
			engine.Dependencies.TCP, engine.Dependencies.HTTPS, engine.Dependencies.Bandwidth = connection, http, prober
			engine.Dependencies.H3Bandwidth = prober
			input := RunInput{
				LocalFiles: &source.ParseResult{Candidates: []model.Candidate{failed, good}},
				Retained: []model.ProbeResult{{Mode: settings.ProbeMode(), Candidate: retained,
					TCP: model.ProbeStats{SuccessRate: 1}}},
				Pinned: []model.Candidate{pin},
			}
			var checkpoint Checkpoint
			input.Plan.Save = func(c Checkpoint) error { checkpoint = c; return nil }
			result := engine.Run(t.Context(), "local-threshold", settings, input, nil)
			pinned := findResult(result.Results, pin.Key())
			if result.State != "completed" || len(result.Results) != 2 || findResult(result.Results, good.Key()) == nil ||
				pinned == nil || !pinned.Pinned || pinned.Status != "failed_gate" || pinned.Bandwidth.Failure != model.FailureBandwidthBelowMin ||
				result.Failures[model.FailureBandwidthBelowMin] != 3 {
				t.Fatalf("threshold failure did not refill or retain pinned result: %+v", result)
			}
			if h3 {
				group := findResult(checkpoint.BandwidthResults, failed.Key())
				if group == nil || len(group.Endpoints) != 2 || group.Endpoints[0].Bandwidth.Failure != "" ||
					group.Endpoints[1].Bandwidth.Failure != model.FailureBandwidthBelowMin {
					t.Fatalf("H3 group did not preserve both endpoint outcomes: %+v", group)
				}
			}
		})
	}
}

func TestLocalFilesRetestPreviousCandidatesAndKeepResumeSnapshot(t *testing.T) {
	for _, h3 := range []bool{false, true} {
		name := "tcp_https"
		if h3 {
			name = "h3"
		}
		t.Run(name, func(t *testing.T) {
			engine, settings, _, connection, http, bandwidth := h3Fixture(nil)
			settings.RequireH3 = h3
			// An accidental HTTP fetch fails the test instead of reaching a network.
			engine.Dependencies.Fetcher = nil
			engine.Dependencies.TCP, engine.Dependencies.HTTPS, engine.Dependencies.Bandwidth = connection, http, bandwidth
			candidate := model.Candidate{AddressType: model.AddressIPv4, IP: "192.0.2.1", Port: 8443, SourceID: "local:nodes.csv"}
			previous := model.Candidate{AddressType: model.AddressIPv4, IP: "192.0.2.99", Port: 443}
			var saved Checkpoint
			input := RunInput{
				LocalFiles: &source.ParseResult{Candidates: []model.Candidate{candidate}},
				Sources:    []source.HTTPSource{{ID: "http", URL: "https://unused.test", Enabled: true}},
				Retained:   []model.ProbeResult{{Mode: settings.ProbeMode(), Candidate: previous, TCP: model.ProbeStats{SuccessRate: 1}}},
				Plan:       RunPlan{Save: func(c Checkpoint) error { c.UpdatedAt = time.Now(); saved = c; return nil }},
			}
			assertLocalProgress := func(p model.RunProgress) {
				if !p.LocalFiles {
					t.Error("local progress lost its origin")
				}
			}
			result := engine.Run(t.Context(), "local", settings, input, assertLocalProgress)
			if !result.LocalFiles || result.Mode != settings.ProbeMode() || len(result.Results) != 2 ||
				findResult(result.Results, candidate.Key()) == nil || findResult(result.Results, previous.Key()) == nil {
				t.Fatalf("local run lost file or retained candidate: %+v", result)
			}
			wantConnectionCalls := 0
			if h3 {
				wantConnectionCalls = 1
			}
			if key := previous.Key(); connection.calls[key] != wantConnectionCalls || http.calls[key] != 1 || bandwidth.calls[key] != 1 {
				t.Fatalf("unexpected retained probe calls: connection=%d HTTP=%d bandwidth=%d", connection.calls[key], http.calls[key], bandwidth.calls[key])
			}
			if !saved.LocalFiles || len(saved.Sources) != 0 || len(saved.Candidates) != 2 ||
				len(saved.RetainedKeys) != 1 || saved.RetainedKeys[0] != previous.Key() {
				t.Fatalf("unexpected local checkpoint candidates: %+v", saved)
			}
			for _, option := range BuildResumeInfo(saved, saved.RunID, settings, input.Sources, NetworkSnapshot{}, time.Now()).Options {
				for _, warning := range option.Warnings {
					if strings.Contains(warning, "数据源已变化") {
						t.Fatal("HTTP sources affected a local snapshot")
					}
				}
			}
			for _, stage := range []string{map[bool]string{false: StageTCP, true: StageQUIC}[h3], StageRanking} {
				base := saved
				resumed := engine.Run(t.Context(), "local-"+stage, settings, RunInput{Sources: input.Sources, Plan: RunPlan{StartStage: stage, Base: &base, Save: input.Plan.Save}}, assertLocalProgress)
				if !resumed.LocalFiles || len(resumed.Results) != 2 || !saved.LocalFiles ||
					findResult(resumed.Results, candidate.Key()) == nil || findResult(resumed.Results, previous.Key()) == nil {
					t.Fatalf("%s did not retain local snapshot: %+v", stage, resumed)
				}
			}
		})
	}
}
