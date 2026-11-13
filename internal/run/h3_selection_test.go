package run

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
)

func TestH3SelectsHTTPQualityAcrossCompletePoolAndReselectsOnResume(t *testing.T) {
	candidates := []model.Candidate{
		{AddressType: model.AddressIPv4, IP: "192.0.2.1", Port: 443},
		{AddressType: model.AddressIPv4, IP: "192.0.2.2", Port: 443},
		{AddressType: model.AddressIPv4, IP: "192.0.2.3", Port: 443},
		{AddressType: model.AddressIPv4, IP: "192.0.2.4", Port: 443},
	}
	engine, settings, _, quic, _, band := h3Fixture(candidates)
	settings.HTTPCandidateCount, settings.BandwidthCandidates, settings.FinalResultCount = 2, 2, 3
	settings.BandwidthConcurrency = 1
	var mu sync.Mutex
	calls := map[string]int{}
	engine.Dependencies.HTTP3 = sampleProbeFunc(func(_ context.Context, c model.Candidate, n int, attempt func()) model.ProbeStats {
		mu.Lock()
		calls[c.IP]++
		mu.Unlock()
		for range n {
			attempt()
		}
		latency := map[string]float64{"192.0.2.1": 100, "192.0.2.2": 1, "192.0.2.3": 10, "192.0.2.4": 20}[c.IP]
		successes := n
		if c.IP == "192.0.2.2" {
			successes-- // Faster, but less available than the other candidates.
		}
		return model.ProbeStats{Attempts: n, Successes: successes, SuccessRate: float64(successes) / float64(n), P95MS: latency}
	})
	input := h3RunInput()
	var saved Checkpoint
	input.Plan.Save = func(c Checkpoint) error { saved = c; return nil }
	summary := engine.Run(t.Context(), "full-http", settings, input, nil)
	if len(summary.Results) != 2 || len(calls) != len(candidates) || len(saved.HTTP3Results) != len(candidates) || !saved.HTTPSelectionComplete {
		t.Fatalf("HTTP stage did not measure and preserve the complete pool: results=%d calls=%v saved=%+v", len(summary.Results), calls, saved)
	}
	if len(band.calls) != 2 || band.calls["192.0.2.3:443"] != 1 || band.calls["192.0.2.4:443"] != 1 {
		t.Fatalf("download pool ignored later HTTP quality or success rate: %v", band.calls)
	}
	httpStage, _ := saved.Stage(StageHTTP3)
	if httpStage.Input != 4 || httpStage.Passed != 4 || httpStage.Selected == nil || *httpStage.Selected != 2 || httpStage.AttemptsTotal != 4*settings.HTTPProbeCount {
		t.Fatalf("unexpected full-pool progress: %+v", httpStage)
	}
	base := saved
	settings.HTTPCandidateCount, settings.BandwidthCandidates = 3, 3
	input.Plan = RunPlan{StartStage: StageBandwidth, Base: &base, Save: func(c Checkpoint) error { saved = c; return nil }}
	summary = engine.Run(t.Context(), "larger-pool", settings, input, nil)
	if summary.StartStage != StageBandwidth || len(summary.Results) != 3 || !slices.Contains(summary.ReusedStages, StageHTTP3) {
		t.Fatalf("complete HTTP snapshot could not be reselected: %+v", summary)
	}
	if band.calls["192.0.2.1:443"] != 1 || band.calls["192.0.2.2:443"] != 0 || band.calls["192.0.2.3:443"] != 2 || band.calls["192.0.2.4:443"] != 2 {
		t.Fatalf("expanded download pool did not use next best measured candidate: %v", band.calls)
	}
	for _, c := range candidates {
		if calls[c.IP] != 1 || quic.calls[c.Key()] != 1 {
			t.Fatalf("download resume repeated connection or HTTP probes: HTTP=%v QUIC=%v", calls, quic.calls)
		}
	}
	httpStage, _ = saved.Stage(StageHTTP3)
	baseStage, _ := base.Stage(StageHTTP3)
	if httpStage.Selected == nil || *httpStage.Selected != 3 || len(base.HTTP3Results) != 4 || baseStage.Selected == nil || *baseStage.Selected != 2 {
		t.Fatalf("reselected progress or complete base snapshot lost: stage=%+v base=%d", httpStage, len(base.HTTP3Results))
	}
}

func TestHTTP3ProgressPlansAllUniqueIPsBeforeFirstProbe(t *testing.T) {
	settings := config.DefaultH3Settings()
	settings.HTTPConcurrency = 1
	progress := newProgress("http-progress", time.Now(), nil)
	var firstTotal int
	var calls int
	p := h3Pipeline{stagePipeline: stagePipeline{ctx: t.Context(), settings: settings, progress: progress,
		dependencies: Dependencies{HTTP3: sampleProbeFunc(func(_ context.Context, c model.Candidate, n int, attempt func()) model.ProbeStats {
			if calls == 0 {
				firstTotal = progress.snapshot()[0].AttemptsTotal
			}
			calls++
			for range n {
				attempt()
			}
			return model.ProbeStats{Attempts: n, Successes: n, SuccessRate: 1, P95MS: 10}
		})}}, cache: map[string]*h3StageCache{}}
	stats := model.ProbeStats{Attempts: 3, Successes: 3, SuccessRate: 1}
	groups := []model.ProbeResult{
		{Candidate: model.Candidate{AddressType: model.AddressDomain, Host: "pool.test", Port: 443}, Endpoints: []model.EndpointResult{{IP: "192.0.2.1", QUIC: stats}, {IP: "192.0.2.2", QUIC: stats}}},
		{Candidate: model.Candidate{AddressType: model.AddressIPv4, IP: "192.0.2.1", Port: 443}, Endpoints: []model.EndpointResult{{IP: "192.0.2.1", QUIC: stats}}},
		{Candidate: model.Candidate{AddressType: model.AddressIPv4, IP: "192.0.2.3", Port: 443}, Endpoints: []model.EndpointResult{{IP: "192.0.2.3", QUIC: stats}}},
	}
	results := p.selectStage(StageHTTP3, groups, 1)
	stage := progress.snapshot()[0]
	if len(results) != 3 || calls != 3 || firstTotal != 3*settings.HTTPProbeCount || stage.AttemptsCompleted != stage.AttemptsTotal || stage.Selected == nil || *stage.Selected != 1 {
		t.Fatalf("HTTP progress did not plan full unique IP work: results=%d calls=%d initialTotal=%d stage=%+v", len(results), calls, firstTotal, stage)
	}
}

func TestHTTP3ProgressCompletesReadyCandidatesWithoutWaitingForSlowDomainIP(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	settings := config.DefaultH3Settings()
	settings.HTTPConcurrency = 2
	release := make(chan struct{})
	updates := make(chan model.StageProgress, 1)
	progress := newProgress("http-ready", time.Now(), func(value model.RunProgress) {
		for _, stage := range value.Stages {
			if stage.Name == StageHTTP3 && stage.Passed == 1 && stage.State == "running" {
				select {
				case updates <- stage:
				default:
				}
			}
		}
	})
	defer progress.stopHeartbeat()
	var mu sync.Mutex
	calls := map[string]int{}
	p := h3Pipeline{stagePipeline: stagePipeline{ctx: ctx, settings: settings, progress: progress,
		dependencies: Dependencies{HTTP3: sampleProbeFunc(func(ctx context.Context, c model.Candidate, n int, attempt func()) model.ProbeStats {
			mu.Lock()
			calls[c.IP]++
			mu.Unlock()
			if c.IP == "192.0.2.2" {
				select {
				case <-release:
				case <-ctx.Done():
				}
			}
			for range n {
				attempt()
			}
			return model.ProbeStats{Attempts: n, Successes: n, SuccessRate: 1}
		})}}, cache: map[string]*h3StageCache{}}
	stats := model.ProbeStats{SuccessRate: 1}
	groups := []model.ProbeResult{
		{Candidate: model.Candidate{AddressType: model.AddressDomain, Host: "pool.test", Port: 443}, Endpoints: []model.EndpointResult{{IP: "192.0.2.1", QUIC: stats}, {IP: "192.0.2.2", QUIC: stats}}},
		{Candidate: model.Candidate{AddressType: model.AddressIPv4, IP: "192.0.2.1", Port: 443}, Endpoints: []model.EndpointResult{{IP: "192.0.2.1", QUIC: stats}}},
	}
	done := make(chan []model.ProbeResult, 1)
	go func() { done <- p.selectStage(StageHTTP3, groups, 2) }()
	select {
	case stage := <-updates:
		if stage.Input != 2 || stage.Passed != 1 || stage.Failed != 0 {
			cancel()
			<-done
			t.Fatalf("incomplete domain was counted as finished: %+v", stage)
		}
	case <-time.After(2 * time.Second):
		cancel()
		<-done
		t.Fatal("ready IP candidate waited for an unrelated slow domain endpoint")
	}
	close(release)
	select {
	case results := <-done:
		stage := progress.snapshot()[0]
		if len(results) != 2 || stage.Passed != 2 || stage.Failed != 0 || calls["192.0.2.1"] != 1 || calls["192.0.2.2"] != 1 {
			t.Fatalf("candidate completion or shared measurement counted twice: results=%d stage=%+v calls=%v", len(results), stage, calls)
		}
	case <-time.After(2 * time.Second):
		cancel()
		<-done
		t.Fatal("HTTP stage did not finish after the final IP completed")
	}
}
