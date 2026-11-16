package run

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
)

func cancellationFixture(h3 bool, candidates []model.Candidate) (Engine, config.Settings) {
	if h3 {
		engine, settings, _, _, _, _ := h3Fixture(candidates)
		return engine, settings
	}
	return Engine{Dependencies: Dependencies{Fetcher: fakeFetcher{candidates: candidates}, TCP: fakeTCP{}, HTTPS: fakeHTTPS{}, Bandwidth: fakeBandwidth{}}}, config.DefaultSettings()
}

func TestManagerCancellationKeepsOnlyCompletedResults(t *testing.T) {
	for _, h3 := range []bool{false, true} {
		for _, keep := range []bool{false, true} {
			t.Run(fmt.Sprintf("h3=%t/keep=%t", h3, keep), func(t *testing.T) {
				candidates := []model.Candidate{
					{AddressType: model.AddressIPv4, IP: "192.0.2.1", Port: 443},
					{AddressType: model.AddressIPv4, IP: "192.0.2.2", Port: 443},
					{AddressType: model.AddressIPv4, IP: "192.0.2.3", Port: 443},
				}
				pinned := []model.Candidate{
					{AddressType: model.AddressIPv4, IP: "192.0.2.9", Port: 443, Country: "CN"},
					{AddressType: model.AddressIPv4, IP: "192.0.2.10", Port: 8443, Country: "CN"},
				}
				engine, settings := cancellationFixture(h3, candidates)
				settings.BandwidthConcurrency, settings.BandwidthCandidates, settings.FinalResultCount = 2, 3, 1
				settings.BlockedCountries, settings.AllowedPorts = []string{"CN"}, []int{443}
				http := sampleProbeFunc(func(_ context.Context, c model.Candidate, n int, attempt func()) model.ProbeStats {
					for range n {
						attempt()
					}
					if c.IP == "192.0.2.9" {
						return model.ProbeStats{Attempts: n, Failures: map[model.FailureReason]int{model.FailureTLS: n}}
					}
					return model.ProbeStats{Attempts: n, Successes: n, SuccessRate: 1}
				})
				engine.Dependencies.HTTPS, engine.Dependencies.HTTP3 = http, http
				blocked, available := make(chan struct{}), make(chan struct{})
				var availableOnce sync.Once
				probe := downloadProbeFunc(func(ctx context.Context, c model.Candidate) model.BandwidthStats {
					if c.IP == "192.0.2.3" {
						close(blocked)
						<-ctx.Done()
					}
					// A cancelled download can have non-zero bytes and speed.
					return model.BandwidthStats{Bytes: 1024, Mbps: 100, DurationMS: 100}
				})
				engine.Dependencies.Bandwidth, engine.Dependencies.H3Bandwidth = probe, probe
				input := h3RunInput()
				input.Pinned = pinned
				var last Checkpoint
				input.Plan.Save = func(c Checkpoint) error { last = c; return nil }
				manager := &Manager{}
				done := make(chan model.RunSummary, 1)
				maxAvailable := 0
				if _, err := manager.Start(t.Context(), engine, settings, input, func(p model.RunProgress) {
					maxAvailable = max(maxAvailable, p.AvailableResults)
					if p.AvailableResults == 3 {
						availableOnce.Do(func() { close(available) })
					}
				}, func(s model.RunSummary) { done <- s }); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { manager.Cancel() })
				waitCancellationSignal(t, blocked)
				waitCancellationSignal(t, available)
				var cancelled bool
				if keep {
					cancelled = manager.CancelWithResults(true)
				} else {
					cancelled = manager.Cancel()
				}
				if !cancelled {
					t.Fatal("running task was not cancelled")
				}
				summary := waitCancellationSummary(t, done)
				if summary.State != "cancelled" || summary.PartialResults != keep || maxAvailable != 3 {
					t.Fatalf("unexpected cancellation: summary=%+v available=%d", summary, maxAvailable)
				}
				if !keep && len(summary.Results) != 0 {
					t.Fatalf("default cancellation kept results: %+v", summary.Results)
				}
				if keep {
					if len(summary.Results) != 3 {
						t.Fatalf("expected one fresh result and two extra pinned results: %+v", summary.Results)
					}
					if findResult(summary.Results, candidates[2].Key()) != nil {
						t.Fatal("cancelled download was kept as a complete result")
					}
					if r := findResult(summary.Results, pinned[0].Key()); r == nil || r.Status != "failed_gate" || r.Score != 0 {
						t.Fatalf("completed failed pin missing: %+v", r)
					}
					if r := findResult(summary.Results, pinned[1].Key()); r == nil || r.Status != "qualified" {
						t.Fatalf("completed qualified pin missing: %+v", r)
					}
				}
				if len(last.BandwidthResults) != 0 {
					t.Fatalf("partial bandwidth persisted: %+v", last.BandwidthResults)
				}
				if _, ok := last.Stage(StageBandwidth); ok {
					t.Fatal("partial bandwidth stage is reusable")
				}
			})
		}
	}
}

func TestKeepCancellationWithoutCompleteResults(t *testing.T) {
	for _, h3 := range []bool{false, true} {
		for _, phase := range []string{"http", "bandwidth"} {
			t.Run(fmt.Sprintf("h3=%t/phase=%s", h3, phase), func(t *testing.T) {
				candidate := model.Candidate{AddressType: model.AddressIPv4, IP: "192.0.2.1", Port: 443}
				engine, settings := cancellationFixture(h3, []model.Candidate{candidate})
				started := make(chan struct{})
				if phase == "http" {
					probe := sampleProbeFunc(func(ctx context.Context, _ model.Candidate, n int, _ func()) model.ProbeStats {
						close(started)
						<-ctx.Done()
						return model.ProbeStats{Attempts: n, Successes: n, SuccessRate: 1}
					})
					engine.Dependencies.HTTPS, engine.Dependencies.HTTP3 = probe, probe
				} else {
					probe := downloadProbeFunc(func(ctx context.Context, _ model.Candidate) model.BandwidthStats {
						close(started)
						<-ctx.Done()
						return model.BandwidthStats{Bytes: 1024, Mbps: 100}
					})
					engine.Dependencies.Bandwidth, engine.Dependencies.H3Bandwidth = probe, probe
				}
				manager := &Manager{}
				done := make(chan model.RunSummary, 1)
				input := h3RunInput()
				input.Pinned = []model.Candidate{candidate}
				available := 0
				if _, err := manager.Start(t.Context(), engine, settings, input, func(p model.RunProgress) {
					available = max(available, p.AvailableResults)
				}, func(s model.RunSummary) { done <- s }); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { manager.Cancel() })
				waitCancellationSignal(t, started)
				manager.CancelWithResults(true)
				summary := waitCancellationSummary(t, done)
				if summary.State != "cancelled" || summary.PartialResults || len(summary.Results) != 0 || available != 0 {
					t.Fatalf("incomplete node was kept: summary=%+v available=%d", summary, available)
				}
			})
		}
	}
}

func TestCancellationKeepsDownloadsCompletedBeforeRegistration(t *testing.T) {
	for _, h3 := range []bool{false, true} {
		t.Run(fmt.Sprintf("h3=%t", h3), func(t *testing.T) {
			candidates := []model.Candidate{
				{AddressType: model.AddressIPv4, IP: "192.0.2.1", Port: 443},
				{AddressType: model.AddressIPv4, IP: "192.0.2.2", Port: 443},
			}
			engine, settings := cancellationFixture(h3, candidates)
			settings.BandwidthConcurrency, settings.BandwidthCandidates, settings.FinalResultCount = 2, 2, 2
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			secondStarted, releaseSecond := make(chan struct{}), make(chan struct{})
			probe := downloadProbeFunc(func(_ context.Context, c model.Candidate) model.BandwidthStats {
				if c.IP == candidates[1].IP {
					close(secondStarted)
					<-releaseSecond
				} else {
					<-secondStarted
				}
				return model.BandwidthStats{Bytes: 1024, Mbps: 100}
			})
			engine.Dependencies.Bandwidth, engine.Dependencies.H3Bandwidth = probe, probe
			var progress *progressTracker
			cancelled := false
			progress = newProgress("completed-before-registration", time.Now(), func(p model.RunProgress) {
				if p.AvailableResults != 1 || cancelled {
					return
				}
				cancelled = true
				close(releaseSecond)
				// Both Probe calls must have returned before cancelling, while the
				// second result still waits for the first completion callback.
				deadline := time.Now().Add(5 * time.Second)
				for {
					progress.mu.Lock()
					completed := progress.value.Stages[0].AttemptsCompleted == 2
					progress.mu.Unlock()
					if completed {
						break
					}
					if time.Now().After(deadline) {
						t.Error("second bandwidth Probe did not return")
						cancel(context.Canceled)
						return
					}
					runtime.Gosched()
				}
				cancel(errKeepCancelledResults)
			})
			defer progress.stopHeartbeat()
			pipeline := stagePipeline{ctx: ctx, dependencies: engine.Dependencies, settings: settings, progress: progress}
			var results []model.ProbeResult
			for _, c := range candidates {
				result := model.ProbeResult{Candidate: c, TCP: model.ProbeStats{SuccessRate: 1}, HTTPS: model.ProbeStats{SuccessRate: 1}}
				if h3 {
					result.Mode = model.ModeH3
					result.Endpoints = []model.EndpointResult{{IP: c.IP, QUIC: model.ProbeStats{SuccessRate: 1}, HTTP3: model.ProbeStats{SuccessRate: 1}}}
				}
				results = append(results, result)
			}
			if h3 {
				p := h3Pipeline{stagePipeline: pipeline, cache: map[string]*h3StageCache{}}
				progress.start(StageBandwidth, 0)
				p.probeBatch(StageBandwidth, results)
			} else {
				pipeline.bandwidth(nil, results)
			}
			kept := progress.cancelledResults(ctx, settings)
			if !cancelled || len(kept) != 2 {
				t.Fatalf("completed downloads were lost while awaiting registration: cancelled=%t results=%+v", cancelled, kept)
			}
		})
	}
}

func TestH3KeepCancellationWaitsForEveryDomainAddress(t *testing.T) {
	pool := model.Candidate{AddressType: model.AddressDomain, Host: "pool.test", Port: 443}
	ip := model.Candidate{AddressType: model.AddressIPv4, IP: "192.0.2.1", Port: 443}
	engine, settings := cancellationFixture(true, []model.Candidate{pool, ip})
	settings.BandwidthConcurrency = 2
	blocked, available := make(chan struct{}), make(chan struct{})
	var once sync.Once
	engine.Dependencies.H3Bandwidth = downloadProbeFunc(func(ctx context.Context, c model.Candidate) model.BandwidthStats {
		if c.IP == "192.0.2.2" {
			close(blocked)
			<-ctx.Done()
		}
		return model.BandwidthStats{Bytes: 1024, Mbps: 100}
	})
	manager := &Manager{}
	done := make(chan model.RunSummary, 1)
	input := h3RunInput()
	input.Pinned = []model.Candidate{pool, ip}
	maxAvailable := 0
	if _, err := manager.Start(t.Context(), engine, settings, input, func(p model.RunProgress) {
		maxAvailable = max(maxAvailable, p.AvailableResults)
		if p.AvailableResults == 1 {
			once.Do(func() { close(available) })
		}
	}, func(s model.RunSummary) { done <- s }); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Cancel() })
	waitCancellationSignal(t, blocked)
	waitCancellationSignal(t, available)
	manager.CancelWithResults(true)
	summary := waitCancellationSummary(t, done)
	if !summary.PartialResults || len(summary.Results) != 1 || summary.Results[0].Candidate.Key() != ip.Key() || maxAvailable != 1 {
		t.Fatalf("unfinished multi-address domain was kept: %+v, available=%d", summary, maxAvailable)
	}
}

func TestH3CancellationDoesNotKeepPinnedEndpointWaitingForSlot(t *testing.T) {
	settings := config.DefaultH3Settings()
	settings.BandwidthConcurrency = 1
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	progress := newProgress("waiting-for-slot", time.Now(), nil)
	good := model.ProbeResult{
		Mode:      model.ModeH3,
		Candidate: model.Candidate{AddressType: model.AddressIPv4, IP: "192.0.2.1", Port: 443},
		QUIC:      model.ProbeStats{SuccessRate: 1}, HTTP3: model.ProbeStats{SuccessRate: 1},
		Bandwidth: model.BandwidthStats{Bytes: 1024, Mbps: 100},
	}
	good.Endpoints = []model.EndpointResult{{IP: good.Candidate.IP, QUIC: good.QUIC, HTTP3: good.HTTP3, Bandwidth: good.Bandwidth}}
	progress.recordCompletedResult(good, settings)
	pin := good
	pin.Candidate.IP, pin.Pinned = "192.0.2.2", true
	pin.Endpoints = []model.EndpointResult{{IP: pin.Candidate.IP, QUIC: good.QUIC, HTTP3: good.HTTP3}}
	cache := &h3StageCache{results: map[string]*h3Measurement{}, slots: make(chan struct{}, 1)}
	cache.slots <- struct{}{}
	calls := 0
	p := h3Pipeline{stagePipeline: stagePipeline{ctx: ctx, settings: settings, progress: progress, dependencies: Dependencies{
		H3Bandwidth: downloadProbeFunc(func(context.Context, model.Candidate) model.BandwidthStats {
			calls++
			return model.BandwidthStats{Bytes: 1024, Mbps: 100}
		}),
	}}, cache: map[string]*h3StageCache{StageBandwidth: cache}}
	progress.start(StageBandwidth, 0)
	done := make(chan struct{})
	go func() {
		p.probeBatch(StageBandwidth, []model.ProbeResult{pin})
		close(done)
	}()
	// The cache entry is created before trying to acquire the occupied slot.
	deadline := time.Now().Add(5 * time.Second)
	for {
		cache.mu.Lock()
		_, queued := cache.results[pin.Candidate.Key()]
		cache.mu.Unlock()
		if queued {
			break
		}
		if time.Now().After(deadline) {
			cancel(context.Canceled)
			waitCancellationSignal(t, done)
			t.Fatal("pinned endpoint did not queue for a bandwidth slot")
		}
		runtime.Gosched()
	}
	cancel(errKeepCancelledResults)
	waitCancellationSignal(t, done)
	kept := progress.cancelledResults(ctx, settings)
	if calls != 0 || len(kept) != 1 || kept[0].Candidate.Key() != good.Candidate.Key() {
		t.Fatalf("unmeasured pinned endpoint was retained: calls=%d results=%+v", calls, kept)
	}
}

func TestRankingCancellationCanKeepReusedCompleteBandwidth(t *testing.T) {
	for _, h3 := range []bool{false, true} {
		t.Run(fmt.Sprintf("h3=%t", h3), func(t *testing.T) {
			candidate := model.Candidate{AddressType: model.AddressIPv4, IP: "192.0.2.1", Port: 443}
			engine, settings := cancellationFixture(h3, []model.Candidate{candidate})
			input := h3RunInput()
			var base Checkpoint
			input.Plan.Save = func(c Checkpoint) error { base = c; return nil }
			if summary := engine.Run(t.Context(), "complete", settings, input, nil); summary.State != "completed" {
				t.Fatalf("fixture run failed: %+v", summary)
			}
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			input.Plan = RunPlan{StartStage: StageRanking, Base: &base}
			summary := engine.Run(ctx, "ranking-cancel", settings, input, func(p model.RunProgress) {
				for _, stage := range p.Stages {
					if stage.Name == StageRanking && stage.State == "running" {
						cancel(errKeepCancelledResults)
					}
				}
			})
			if summary.State != "cancelled" || !summary.PartialResults || len(summary.Results) != 1 || summary.Results[0].Status != "qualified" {
				t.Fatalf("complete reused measurements were lost: %+v", summary)
			}
		})
	}
}

func TestManagerAcceptedCancellationWinsCompletionRace(t *testing.T) {
	for _, h3 := range []bool{false, true} {
		for _, keep := range []bool{false, true} {
			for _, qualified := range []bool{false, true} {
				t.Run(fmt.Sprintf("h3=%t/keep=%t/qualified=%t", h3, keep, qualified), func(t *testing.T) {
					candidate := model.Candidate{AddressType: model.AddressIPv4, IP: "192.0.2.1", Port: 443}
					engine, settings := cancellationFixture(h3, []model.Candidate{candidate})
					if !qualified {
						probe := downloadProbeFunc(func(context.Context, model.Candidate) model.BandwidthStats {
							return model.BandwidthStats{Failure: model.FailureDownload}
						})
						engine.Dependencies.Bandwidth, engine.Dependencies.H3Bandwidth = probe, probe
					}
					input := h3RunInput()
					input.Pinned = []model.Candidate{candidate}
					manager := &Manager{}
					done := make(chan model.RunSummary, 1)
					accepted := false
					if _, err := manager.Start(t.Context(), engine, settings, input, func(p model.RunProgress) {
						if p.State == "completed" {
							accepted = manager.CancelWithResults(keep)
						}
					}, func(s model.RunSummary) { done <- s }); err != nil {
						t.Fatal(err)
					}
					summary := waitCancellationSummary(t, done)
					partial := keep && qualified
					resultCount := 0
					if partial {
						resultCount = 1
					}
					if !accepted || summary.State != "cancelled" || summary.PartialResults != partial || len(summary.Results) != resultCount {
						t.Fatalf("accepted cancellation became a completed run: accepted=%t summary=%+v", accepted, summary)
					}
				})
			}
		}
	}
}

func waitCancellationSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for cancellation test signal")
	}
}

func waitCancellationSummary(t *testing.T, done <-chan model.RunSummary) model.RunSummary {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled run did not finish")
		return model.RunSummary{}
	}
}
