package run

import (
	"context"
	"testing"
	"time"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
	"github.com/linvva/cf-node-bench/internal/source"
)

type fakeFetcher struct{ candidates []model.Candidate }

func (f fakeFetcher) Fetch(context.Context, source.HTTPSource) (source.ParseResult, error) {
	if f.candidates != nil {
		return source.ParseResult{Candidates: f.candidates}, nil
	}
	return source.ParseResult{Candidates: []model.Candidate{{AddressType: model.AddressIPv4, IP: "1.1.1.1", Port: 443}}}, nil
}

type fakeTCP struct {
	delay      time.Duration
	attempts   *int
	candidates *[]model.Candidate
}

func (f fakeTCP) Probe(ctx context.Context, c model.Candidate, n int) model.ProbeStats {
	if f.attempts != nil {
		*f.attempts = n
	}
	if f.candidates != nil {
		*f.candidates = append(*f.candidates, c)
	}
	select {
	case <-time.After(f.delay):
		return model.ProbeStats{Attempts: n, Successes: n, SuccessRate: 1, P95MS: 10}
	case <-ctx.Done():
		return model.ProbeStats{Attempts: n, Failures: map[model.FailureReason]int{model.FailureCancelled: 1}}
	}
}

func TestEngineFiltersBlockedCountryBeforeTCP(t *testing.T) {
	settings := config.DefaultSettings()
	settings.BlockedCountries = []string{"CN"}
	settings.TCPConcurrency = 1
	candidates := []model.Candidate{
		{AddressType: model.AddressIPv4, IP: "1.1.1.1", Port: 443, Country: "CN"},
		{AddressType: model.AddressIPv4, IP: "8.8.8.8", Port: 443, Country: "US"},
	}
	var probed []model.Candidate
	engine := Engine{Dependencies: Dependencies{
		Fetcher: fakeFetcher{candidates: candidates}, TCP: fakeTCP{candidates: &probed}, HTTPS: fakeHTTPS{}, Bandwidth: fakeBandwidth{},
	}}
	var updates []model.RunProgress
	summary := engine.Run(t.Context(), "country-filter", settings, []source.HTTPSource{{Enabled: true}, {Enabled: true}}, func(progress model.RunProgress) {
		updates = append(updates, progress)
	})
	if len(probed) != 1 || probed[0].Country != "US" {
		t.Fatalf("blocked candidate reached TCP probe: %+v", probed)
	}
	if summary.Failures[model.FailureCountryFiltered] != 1 {
		t.Fatalf("country filter count missing: %+v", summary.Failures)
	}
	filterStage := completedStage(updates, "filter")
	if filterStage.Input != 2 || filterStage.Passed != 1 || filterStage.Failed != 1 {
		t.Fatalf("filter stage counts are inconsistent: %+v", filterStage)
	}
}

type timeoutTCP struct{}

func (timeoutTCP) Probe(_ context.Context, _ model.Candidate, attempts int) model.ProbeStats {
	return model.ProbeStats{Attempts: attempts, Failures: map[model.FailureReason]int{model.FailureTimeout: attempts}}
}

func TestProgressCountsFailedNodesInsteadOfFailedAttempts(t *testing.T) {
	settings := config.DefaultSettings()
	engine := Engine{Dependencies: Dependencies{Fetcher: fakeFetcher{}, TCP: timeoutTCP{}, HTTPS: fakeHTTPS{}, Bandwidth: fakeBandwidth{}}}
	var updates []model.RunProgress
	summary := engine.Run(t.Context(), "node-failures", settings, []source.HTTPSource{{Enabled: true}}, func(progress model.RunProgress) {
		updates = append(updates, progress)
	})
	if summary.Failures[model.FailureTimeout] != 1 {
		t.Fatalf("one failed node must contribute one timeout, got %+v", summary.Failures)
	}
	tcpStage := completedStage(updates, "tcp")
	if tcpStage.Input != 1 || tcpStage.Passed != 0 || tcpStage.Failed != 1 {
		t.Fatalf("TCP stage counts are inconsistent: %+v", tcpStage)
	}
}

func completedStage(updates []model.RunProgress, name string) model.StageProgress {
	for index := len(updates) - 1; index >= 0; index-- {
		for _, stage := range updates[index].Stages {
			if stage.Name == name && stage.State == "completed" {
				return stage
			}
		}
	}
	return model.StageProgress{}
}

type fakeHTTPS struct{ attempts *int }

func (f fakeHTTPS) Probe(_ context.Context, _ model.Candidate, attempts int) model.ProbeStats {
	if f.attempts != nil {
		*f.attempts = attempts
	}
	return model.ProbeStats{Attempts: 3, Successes: 3, SuccessRate: 1, P95MS: 20}
}

type fakeBandwidth struct{}

func (fakeBandwidth) Probe(context.Context, model.Candidate) model.BandwidthStats {
	return model.BandwidthStats{Bytes: 1024, Mbps: 100}
}

func TestEngineCompletesPipeline(t *testing.T) {
	settings := config.DefaultSettings()
	settings.TCPProbeCount = 2
	settings.HTTPSProbeCount = 4
	var tcpAttempts, httpsAttempts int
	engine := Engine{Dependencies: Dependencies{Fetcher: fakeFetcher{}, TCP: fakeTCP{attempts: &tcpAttempts}, HTTPS: fakeHTTPS{attempts: &httpsAttempts}, Bandwidth: fakeBandwidth{}}}
	var updates []model.RunProgress
	summary := engine.Run(t.Context(), "test", settings, []source.HTTPSource{{Enabled: true}}, func(p model.RunProgress) { updates = append(updates, p) })
	if summary.State != "completed" || len(summary.Results) != 1 {
		t.Fatalf("summary: %+v", summary)
	}
	if len(updates) == 0 || len(updates[len(updates)-1].Stages) != 6 {
		t.Fatalf("progress updates missing: %+v", updates)
	}
	seenIncrementalTCP := false
	for _, update := range updates {
		for _, stage := range update.Stages {
			if stage.Name == "tcp" && stage.State == "running" && stage.Passed == 1 {
				seenIncrementalTCP = true
			}
		}
	}
	if !seenIncrementalTCP {
		t.Fatal("TCP progress must update before the stage completes")
	}
	if tcpAttempts != 2 || httpsAttempts != 4 {
		t.Fatalf("independent probe counts not applied: tcp=%d https=%d", tcpAttempts, httpsAttempts)
	}
}

func TestEngineCancellation(t *testing.T) {
	settings := config.DefaultSettings()
	engine := Engine{Dependencies: Dependencies{Fetcher: fakeFetcher{}, TCP: fakeTCP{delay: time.Second}, HTTPS: fakeHTTPS{}, Bandwidth: fakeBandwidth{}}}
	ctx, cancel := context.WithCancel(t.Context())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	started := time.Now()
	summary := engine.Run(ctx, "cancel", settings, []source.HTTPSource{{Enabled: true}}, nil)
	if summary.State != "cancelled" {
		t.Fatalf("state=%s", summary.State)
	}
	if summary.Results == nil {
		t.Fatal("cancelled summary results must encode as an empty array")
	}
	if time.Since(started) > 300*time.Millisecond {
		t.Fatal("pipeline cancellation was not prompt")
	}
}
