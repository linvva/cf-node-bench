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

func (f fakeTCP) Probe(ctx context.Context, c model.Candidate, n int, onAttempt func()) model.ProbeStats {
	if f.attempts != nil {
		*f.attempts = n
	}
	if f.candidates != nil {
		*f.candidates = append(*f.candidates, c)
	}
	select {
	case <-time.After(f.delay):
		for range n {
			if onAttempt != nil {
				onAttempt()
			}
		}
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
	summary := engine.Run(t.Context(), "country-filter", settings, RunInput{Sources: []source.HTTPSource{{Enabled: true}, {Enabled: true}}}, func(progress model.RunProgress) {
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

func (timeoutTCP) Probe(_ context.Context, _ model.Candidate, attempts int, onAttempt func()) model.ProbeStats {
	for range attempts {
		if onAttempt != nil {
			onAttempt()
		}
	}
	return model.ProbeStats{Attempts: attempts, Failures: map[model.FailureReason]int{model.FailureTimeout: attempts}}
}

func TestProgressCountsFailedNodesInsteadOfFailedAttempts(t *testing.T) {
	settings := config.DefaultSettings()
	engine := Engine{Dependencies: Dependencies{Fetcher: fakeFetcher{}, TCP: timeoutTCP{}, HTTPS: fakeHTTPS{}, Bandwidth: fakeBandwidth{}}}
	var updates []model.RunProgress
	summary := engine.Run(t.Context(), "node-failures", settings, RunInput{Sources: []source.HTTPSource{{Enabled: true}}}, func(progress model.RunProgress) {
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

func (f fakeHTTPS) Probe(_ context.Context, _ model.Candidate, attempts int, onAttempt func()) model.ProbeStats {
	if f.attempts != nil {
		*f.attempts = attempts
	}
	for range attempts {
		if onAttempt != nil {
			onAttempt()
		}
	}
	return model.ProbeStats{Attempts: 3, Successes: 3, SuccessRate: 1, P95MS: 20}
}

type selectiveHTTPS struct {
	failed map[string]bool
	probed *[]string
}

type httpsCall struct {
	candidate model.Candidate
	attempts  int
}

type trackingHTTPS struct {
	calls  *[]httpsCall
	failed map[string]bool
}

func (f trackingHTTPS) Probe(_ context.Context, candidate model.Candidate, attempts int, onAttempt func()) model.ProbeStats {
	*f.calls = append(*f.calls, httpsCall{candidate: candidate, attempts: attempts})
	for range attempts {
		if onAttempt != nil {
			onAttempt()
		}
	}
	if f.failed[candidate.Key()] {
		return model.ProbeStats{Attempts: attempts, Failures: map[model.FailureReason]int{model.FailureTLS: attempts}}
	}
	return model.ProbeStats{Attempts: attempts, Successes: attempts, SuccessRate: 1, AverageMS: 11, P50MS: 10, P95MS: 12, JitterMS: 1}
}

func (f selectiveHTTPS) Probe(_ context.Context, candidate model.Candidate, attempts int, onAttempt func()) model.ProbeStats {
	*f.probed = append(*f.probed, candidate.Key())
	for range attempts {
		if onAttempt != nil {
			onAttempt()
		}
	}
	if f.failed[candidate.Key()] {
		return model.ProbeStats{Attempts: attempts, Failures: map[model.FailureReason]int{model.FailureHTTPStatus: attempts}}
	}
	return model.ProbeStats{Attempts: attempts, Successes: attempts, SuccessRate: 1, P95MS: 20}
}

type fakeBandwidth struct{}

func (fakeBandwidth) Probe(context.Context, model.Candidate) model.BandwidthStats {
	return model.BandwidthStats{Bytes: 1024, Mbps: 100}
}

type selectiveBandwidth struct {
	failed map[string]bool
	probed *[]string
}

func (f selectiveBandwidth) Probe(_ context.Context, candidate model.Candidate) model.BandwidthStats {
	*f.probed = append(*f.probed, candidate.Key())
	if f.failed[candidate.Key()] {
		return model.BandwidthStats{Failure: model.FailureDownload}
	}
	return model.BandwidthStats{Bytes: 1024, Mbps: 100}
}

func TestRetainedResultsTakePriorityAndUseExtraBandwidthQuota(t *testing.T) {
	settings := config.DefaultSettings()
	settings.TCPConcurrency = 1
	settings.HTTPSConcurrency = 1
	settings.BandwidthConcurrency = 1
	settings.TCPCandidateCount = 3
	settings.BandwidthCandidates = 3
	settings.FinalResultCount = 3
	fresh := []model.Candidate{
		{AddressType: model.AddressIPv4, IP: "1.1.1.1", Port: 443, Country: "US", SourceID: "fresh"},
		{AddressType: model.AddressIPv4, IP: "1.1.1.2", Port: 443, Country: "US", SourceID: "fresh"},
		{AddressType: model.AddressIPv4, IP: "1.1.1.3", Port: 443, Country: "US", SourceID: "fresh"},
	}
	retainedCandidate := model.Candidate{AddressType: model.AddressIPv4, IP: "1.1.1.10", Port: 443, Country: "JP", SourceID: "old"}
	retained := []model.ProbeResult{
		{Candidate: retainedCandidate, TCP: model.ProbeStats{Attempts: 3, Successes: 3, SuccessRate: 1, P95MS: 1}, HTTPS: model.ProbeStats{Attempts: 3, Successes: 3, SuccessRate: 1, P95MS: 999}, Bandwidth: model.BandwidthStats{Mbps: 1}, Score: 99},
		{Candidate: model.Candidate{AddressType: model.AddressIPv4, IP: fresh[0].IP, Port: fresh[0].Port, Country: "ZZ", SourceID: "old"}, TCP: model.ProbeStats{SuccessRate: 1}},
		{Candidate: model.Candidate{AddressType: model.AddressIPv4, IP: "1.1.1.11", Port: 80, Country: "US"}, TCP: model.ProbeStats{SuccessRate: 1}},
		{Candidate: model.Candidate{AddressType: model.AddressIPv4, IP: "1.1.1.12", Port: 443, Country: "US"}, TCP: model.ProbeStats{SuccessRate: 0.5}},
	}
	var tcpCandidates []model.Candidate
	var httpsCalls []httpsCall
	var bandwidthCalls []string
	engine := Engine{Dependencies: Dependencies{
		Fetcher:   fakeFetcher{candidates: fresh},
		TCP:       fakeTCP{candidates: &tcpCandidates},
		HTTPS:     trackingHTTPS{calls: &httpsCalls, failed: map[string]bool{}},
		Bandwidth: selectiveBandwidth{failed: map[string]bool{}, probed: &bandwidthCalls},
	}}
	var updates []model.RunProgress
	summary := engine.Run(t.Context(), "retained-fast-path", settings, RunInput{Sources: []source.HTTPSource{{Enabled: true}}, Retained: retained}, func(progress model.RunProgress) {
		updates = append(updates, progress)
	})

	if len(tcpCandidates) != len(fresh)-1 {
		t.Fatalf("TCP probed retained candidates: %+v", tcpCandidates)
	}
	retainedHTTPSCalls := 0
	duplicateFastCalls := 0
	duplicateFullCalls := 0
	for _, call := range httpsCalls {
		if call.candidate.Key() == retainedCandidate.Key() && call.attempts == 1 {
			retainedHTTPSCalls++
		}
		if call.candidate.Key() == fresh[0].Key() {
			if call.attempts == 1 && call.candidate.Country == "US" && call.candidate.SourceID == "fresh" {
				duplicateFastCalls++
			}
			if call.attempts == settings.HTTPSProbeCount {
				duplicateFullCalls++
			}
		}
	}
	if retainedHTTPSCalls != 1 || duplicateFastCalls != 1 || duplicateFullCalls != 0 {
		t.Fatalf("unexpected HTTPS paths: %+v", httpsCalls)
	}
	if len(bandwidthCalls) != 4 || bandwidthCalls[0] != retainedCandidate.Key() || bandwidthCalls[1] != fresh[0].Key() {
		t.Fatalf("retained bandwidth was not extra and first: %v", bandwidthCalls)
	}
	retainedResult := findResult(summary.Results, retainedCandidate.Key())
	if retainedResult == nil || retainedResult.TCP.P95MS != 1 || retainedResult.HTTPS.Attempts != 1 || retainedResult.HTTPS.P95MS != 12 || retainedResult.Bandwidth.Mbps != 100 {
		t.Fatalf("retained metrics were not refreshed correctly: %+v", retainedResult)
	}
	if summary.Failures[model.FailurePortFiltered] != 1 || summary.Failures[model.FailureTCP] != 1 {
		t.Fatalf("retained gates were not counted: %+v", summary.Failures)
	}
	retainedStage := completedStage(updates, "retained")
	if retainedStage.Input != 2 || retainedStage.Passed != 2 || retainedStage.Failed != 0 || retainedStage.AttemptsCompleted != 2 || retainedStage.AttemptsTotal != 2 {
		t.Fatalf("retained progress is inconsistent: %+v", retainedStage)
	}
	bandwidthStage := completedStage(updates, "bandwidth")
	if bandwidthStage.Input != 4 || bandwidthStage.Passed != 4 || bandwidthStage.Failed != 0 {
		t.Fatalf("bandwidth progress did not include extra retained result: %+v", bandwidthStage)
	}
	if len(summary.Results) != settings.FinalResultCount {
		t.Fatalf("final result cap was not applied: %d", len(summary.Results))
	}
}

func TestRetainedFailuresAreEliminated(t *testing.T) {
	settings := config.DefaultSettings()
	settings.HTTPSConcurrency = 1
	settings.BandwidthConcurrency = 1
	settings.BandwidthCandidates = 1
	settings.FinalResultCount = 1
	retained := []model.ProbeResult{
		{Candidate: model.Candidate{AddressType: model.AddressIPv4, IP: "1.1.1.20", Port: 443}, TCP: model.ProbeStats{SuccessRate: 1}},
		{Candidate: model.Candidate{AddressType: model.AddressIPv4, IP: "1.1.1.21", Port: 443}, TCP: model.ProbeStats{SuccessRate: 1}},
	}
	var httpsCalls []httpsCall
	var bandwidthCalls []string
	engine := Engine{Dependencies: Dependencies{
		Fetcher: fakeFetcher{candidates: []model.Candidate{}},
		TCP:     fakeTCP{},
		HTTPS: trackingHTTPS{
			calls:  &httpsCalls,
			failed: map[string]bool{retained[0].Candidate.Key(): true},
		},
		Bandwidth: selectiveBandwidth{
			failed: map[string]bool{retained[1].Candidate.Key(): true},
			probed: &bandwidthCalls,
		},
	}}
	var updates []model.RunProgress
	summary := engine.Run(t.Context(), "retained-failures", settings, RunInput{Retained: retained}, func(progress model.RunProgress) {
		updates = append(updates, progress)
	})

	if len(summary.Results) != 0 || len(httpsCalls) != 2 || len(bandwidthCalls) != 1 || bandwidthCalls[0] != retained[1].Candidate.Key() {
		t.Fatalf("failed retained candidates were not eliminated: results=%+v https=%+v bandwidth=%v", summary.Results, httpsCalls, bandwidthCalls)
	}
	retainedStage := completedStage(updates, "retained")
	if retainedStage.Input != 2 || retainedStage.Passed != 1 || retainedStage.Failed != 1 {
		t.Fatalf("retained failure progress is inconsistent: %+v", retainedStage)
	}
	bandwidthStage := completedStage(updates, "bandwidth")
	if bandwidthStage.Input != 1 || bandwidthStage.Passed != 0 || bandwidthStage.Failed != 1 {
		t.Fatalf("retained bandwidth failure progress is inconsistent: %+v", bandwidthStage)
	}
}

func findResult(results []model.ProbeResult, key string) *model.ProbeResult {
	for index := range results {
		if results[index].Candidate.Key() == key {
			return &results[index]
		}
	}
	return nil
}

func TestHTTPSRefillsFromRemainingTCPCandidates(t *testing.T) {
	settings := config.DefaultSettings()
	settings.TCPConcurrency = 1
	settings.HTTPSConcurrency = 1
	settings.BandwidthConcurrency = 1
	settings.TCPCandidateCount = 3
	settings.BandwidthCandidates = 1
	settings.FinalResultCount = 1
	candidates := []model.Candidate{
		{AddressType: model.AddressIPv4, IP: "1.1.1.1", Port: 443},
		{AddressType: model.AddressIPv4, IP: "1.1.1.2", Port: 443},
		{AddressType: model.AddressIPv4, IP: "1.1.1.3", Port: 443},
		{AddressType: model.AddressIPv4, IP: "1.1.1.4", Port: 443},
		{AddressType: model.AddressIPv4, IP: "1.1.1.5", Port: 443},
	}
	var probed []string
	engine := Engine{Dependencies: Dependencies{
		Fetcher: fakeFetcher{candidates: candidates},
		TCP:     fakeTCP{},
		HTTPS: selectiveHTTPS{
			failed: map[string]bool{candidates[0].Key(): true, candidates[1].Key(): true},
			probed: &probed,
		},
		Bandwidth: fakeBandwidth{},
	}}
	var updates []model.RunProgress
	summary := engine.Run(t.Context(), "https-refill", settings, RunInput{Sources: []source.HTTPSource{{Enabled: true}}}, func(progress model.RunProgress) {
		updates = append(updates, progress)
	})

	if len(summary.Results) != 1 {
		t.Fatalf("expected the configured bandwidth result after HTTPS refill, got %d", len(summary.Results))
	}
	if len(probed) != 5 {
		t.Fatalf("expected all 5 candidates to be tried while refilling, got %v", probed)
	}
	seen := make(map[string]bool, len(probed))
	for _, candidate := range probed {
		if seen[candidate] {
			t.Fatalf("candidate was probed more than once: %s", candidate)
		}
		seen[candidate] = true
	}
	stage := completedStage(updates, "https")
	expectedAttempts := len(probed) * settings.HTTPSProbeCount
	if stage.Input != 5 || stage.Passed != 3 || stage.Failed != 2 || stage.AttemptsCompleted != expectedAttempts || stage.AttemptsTotal != expectedAttempts {
		t.Fatalf("HTTPS refill progress is inconsistent: %+v", stage)
	}
}

func TestHTTPSRefillStopsWhenTCPCandidatesAreExhausted(t *testing.T) {
	settings := config.DefaultSettings()
	settings.TCPConcurrency = 1
	settings.HTTPSConcurrency = 1
	settings.BandwidthConcurrency = 1
	settings.TCPCandidateCount = 3
	settings.BandwidthCandidates = 3
	settings.FinalResultCount = 3
	candidates := []model.Candidate{
		{AddressType: model.AddressIPv4, IP: "1.1.1.1", Port: 443},
		{AddressType: model.AddressIPv4, IP: "1.1.1.2", Port: 443},
		{AddressType: model.AddressIPv4, IP: "1.1.1.3", Port: 443},
		{AddressType: model.AddressIPv4, IP: "1.1.1.4", Port: 443},
	}
	var probed []string
	engine := Engine{Dependencies: Dependencies{
		Fetcher: fakeFetcher{candidates: candidates},
		TCP:     fakeTCP{},
		HTTPS: selectiveHTTPS{
			failed: map[string]bool{candidates[0].Key(): true, candidates[1].Key(): true, candidates[2].Key(): true},
			probed: &probed,
		},
		Bandwidth: fakeBandwidth{},
	}}
	var updates []model.RunProgress
	summary := engine.Run(t.Context(), "https-exhausted", settings, RunInput{Sources: []source.HTTPSource{{Enabled: true}}}, func(progress model.RunProgress) {
		updates = append(updates, progress)
	})

	if len(summary.Results) != 1 || len(probed) != 4 {
		t.Fatalf("expected one result after exhausting 4 candidates, got results=%d probed=%v", len(summary.Results), probed)
	}
	stage := completedStage(updates, "https")
	if stage.Input != 4 || stage.Passed != 1 || stage.Failed != 3 {
		t.Fatalf("exhausted HTTPS progress is inconsistent: %+v", stage)
	}
}

func TestBandwidthRefillsFromRemainingHTTPSCandidates(t *testing.T) {
	settings := config.DefaultSettings()
	settings.TCPConcurrency = 1
	settings.HTTPSConcurrency = 1
	settings.BandwidthConcurrency = 1
	settings.TCPCandidateCount = 5
	settings.BandwidthCandidates = 3
	settings.FinalResultCount = 3
	candidates := []model.Candidate{
		{AddressType: model.AddressIPv4, IP: "1.1.1.1", Port: 443},
		{AddressType: model.AddressIPv4, IP: "1.1.1.2", Port: 443},
		{AddressType: model.AddressIPv4, IP: "1.1.1.3", Port: 443},
		{AddressType: model.AddressIPv4, IP: "1.1.1.4", Port: 443},
		{AddressType: model.AddressIPv4, IP: "1.1.1.5", Port: 443},
	}
	var probed []string
	engine := Engine{Dependencies: Dependencies{
		Fetcher: fakeFetcher{candidates: candidates},
		TCP:     fakeTCP{},
		HTTPS:   fakeHTTPS{},
		Bandwidth: selectiveBandwidth{
			failed: map[string]bool{candidates[0].Key(): true, candidates[1].Key(): true},
			probed: &probed,
		},
	}}
	var updates []model.RunProgress
	summary := engine.Run(t.Context(), "bandwidth-refill", settings, RunInput{Sources: []source.HTTPSource{{Enabled: true}}}, func(progress model.RunProgress) {
		updates = append(updates, progress)
	})

	if len(summary.Results) != 3 {
		t.Fatalf("expected 3 successful bandwidth results, got %d", len(summary.Results))
	}
	if len(probed) != 5 {
		t.Fatalf("expected all 5 candidates to be tried while refilling, got %v", probed)
	}
	seen := make(map[string]bool, len(probed))
	for _, candidate := range probed {
		if seen[candidate] {
			t.Fatalf("candidate was probed more than once: %s", candidate)
		}
		seen[candidate] = true
	}
	stage := completedStage(updates, "bandwidth")
	if stage.Input != 5 || stage.Passed != 3 || stage.Failed != 2 || stage.AttemptsCompleted != 5 || stage.AttemptsTotal != 5 {
		t.Fatalf("bandwidth refill progress is inconsistent: %+v", stage)
	}
}

func TestEngineCompletesPipeline(t *testing.T) {
	settings := config.DefaultSettings()
	settings.TCPProbeCount = 2
	settings.HTTPSProbeCount = 4
	var tcpAttempts, httpsAttempts int
	engine := Engine{Dependencies: Dependencies{Fetcher: fakeFetcher{}, TCP: fakeTCP{attempts: &tcpAttempts}, HTTPS: fakeHTTPS{attempts: &httpsAttempts}, Bandwidth: fakeBandwidth{}}}
	var updates []model.RunProgress
	summary := engine.Run(t.Context(), "test", settings, RunInput{Sources: []source.HTTPSource{{Enabled: true}}}, func(p model.RunProgress) { updates = append(updates, p) })
	if summary.State != "completed" || len(summary.Results) != 1 {
		t.Fatalf("summary: %+v", summary)
	}
	if len(updates) == 0 || len(updates[len(updates)-1].Stages) != 7 {
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
	tcpStage := completedStage(updates, "tcp")
	if tcpStage.AttemptsCompleted != 2 || tcpStage.AttemptsTotal != 2 {
		t.Fatalf("TCP attempt progress: %+v", tcpStage)
	}
}

func TestEngineCancellation(t *testing.T) {
	settings := config.DefaultSettings()
	engine := Engine{Dependencies: Dependencies{Fetcher: fakeFetcher{}, TCP: fakeTCP{delay: time.Second}, HTTPS: fakeHTTPS{}, Bandwidth: fakeBandwidth{}}}
	ctx, cancel := context.WithCancel(t.Context())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	started := time.Now()
	summary := engine.Run(ctx, "cancel", settings, RunInput{Sources: []source.HTTPSource{{Enabled: true}}}, nil)
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

func TestProgressEmitsOutsideStateLock(t *testing.T) {
	var progress *progressTracker
	emittedWhileLocked := false
	progress = newProgress("lock-test", time.Now(), func(model.RunProgress) {
		if !progress.mu.TryLock() {
			emittedWhileLocked = true
			return
		}
		progress.mu.Unlock()
	})
	progress.start("source", 1)
	progress.summary(time.Now(), nil, "completed")
	if emittedWhileLocked {
		t.Fatal("progress event emitted while holding the state lock")
	}
}

func TestProgressHeartbeatUpdatesSlowStage(t *testing.T) {
	updates := make(chan model.RunProgress, 8)
	progress := newProgress("heartbeat-test", time.Now(), func(value model.RunProgress) { updates <- value })
	progress.startProbe("https", 2, 3)
	deadline := time.After(time.Second)
	for {
		select {
		case update := <-updates:
			if len(update.Stages) > 0 && update.Stages[0].DurationMS > 0 {
				progress.summary(time.Now(), nil, "completed")
				return
			}
		case <-deadline:
			progress.summary(time.Now(), nil, "cancelled")
			t.Fatal("slow stage did not emit a heartbeat update")
		}
	}
}
