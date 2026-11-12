package run

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
)

type sampleProbeFunc func(context.Context, model.Candidate, int, func()) model.ProbeStats

func (f sampleProbeFunc) Probe(ctx context.Context, c model.Candidate, n int, onAttempt func()) model.ProbeStats {
	return f(ctx, c, n, onAttempt)
}

type downloadProbeFunc func(context.Context, model.Candidate) model.BandwidthStats

func (f downloadProbeFunc) Probe(ctx context.Context, c model.Candidate) model.BandwidthStats {
	return f(ctx, c)
}

func TestRollingStagesReplaceFailureBeforeSlowCandidateFinishes(t *testing.T) {
	for _, stage := range []string{StageHTTPS, StageHTTP3, "tcp_download", "h3_download"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			started := make(chan string, 10)
			release := make(chan struct{})
			wait := func(ctx context.Context, c model.Candidate) {
				started <- c.IP
				if c.IP == "192.0.2.1" {
					select {
					case <-release:
					case <-ctx.Done():
					}
				}
			}
			sample := sampleProbeFunc(func(ctx context.Context, c model.Candidate, n int, onAttempt func()) model.ProbeStats {
				wait(ctx, c)
				for range n {
					onAttempt()
				}
				if c.IP == "192.0.2.2" {
					return model.ProbeStats{Attempts: n, Failures: map[model.FailureReason]int{model.FailureHTTPStatus: n}}
				}
				return model.ProbeStats{Attempts: n, Successes: n, SuccessRate: 1}
			})
			download := downloadProbeFunc(func(ctx context.Context, c model.Candidate) model.BandwidthStats {
				wait(ctx, c)
				if c.IP == "192.0.2.2" {
					return model.BandwidthStats{Failure: model.FailureDownload}
				}
				return model.BandwidthStats{Bytes: 65536, Mbps: 1}
			})
			settings := config.DefaultSettings()
			settings.HTTPConcurrency, settings.BandwidthConcurrency = 2, 2
			settings.HTTPCandidateCount, settings.BandwidthCandidates = 2, 2
			progress := newProgress("rolling", time.Now(), nil)
			pipeline := stagePipeline{ctx: ctx, settings: settings, progress: progress, dependencies: Dependencies{HTTPS: sample, HTTP3: sample, Bandwidth: download, H3Bandwidth: download}}
			var groups []model.ProbeResult
			for _, ip := range []string{"192.0.2.1", "192.0.2.2", "192.0.2.3", "192.0.2.4"} {
				stats := model.ProbeStats{SuccessRate: 1}
				groups = append(groups, model.ProbeResult{Candidate: model.Candidate{IP: ip, Port: 443}, TCP: stats, HTTPS: stats, QUIC: stats, HTTP3: stats, Endpoints: []model.EndpointResult{{IP: ip, QUIC: stats, HTTP3: stats}}})
			}
			done := make(chan []model.ProbeResult, 1)
			go func() {
				switch stage {
				case StageHTTPS:
					done <- pipeline.https(groups)
				case "tcp_download":
					done <- pipeline.bandwidth(nil, groups)
				default:
					h3 := h3Pipeline{stagePipeline: pipeline, cache: map[string]*h3StageCache{}}
					name := StageHTTP3
					if stage == "h3_download" {
						name = StageBandwidth
					}
					done <- h3.selectStage(name, groups, 2)
				}
			}()
			seen := map[string]bool{}
			for len(seen) < 3 {
				select {
				case ip := <-started:
					seen[ip] = true
				case <-time.After(2 * time.Second):
					cancel()
					<-done
					t.Fatal("failed candidate was not replaced while the slow candidate was still running")
				}
			}
			if seen["192.0.2.4"] {
				t.Fatal("started more candidates than needed")
			}
			close(release)
			select {
			case results := <-done:
				var ips []string
				for _, result := range results {
					ips = append(ips, result.Candidate.IP)
				}
				if !reflect.DeepEqual(ips, []string{"192.0.2.1", "192.0.2.2", "192.0.2.3"}) {
					t.Fatalf("candidate order or target changed: %v", ips)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("stage did not stop after reaching its success target")
			}
			stages := progress.snapshot()
			current := stages[len(stages)-1]
			if current.Passed != 2 || current.Failed != 1 || current.Input != 3 || current.AttemptsCompleted != current.AttemptsTotal || len(started) != 0 {
				t.Fatalf("unexpected work or progress: %+v", current)
			}
		})
	}
}

func TestRollingCancellationDrainsInflightWithoutStartingMore(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan int, 4)
	done := make(chan []int, 1)
	go func() {
		done <- parallelUntil(ctx, []int{0, 1, 2, 3}, 2, 4, func(ctx context.Context, value int) int {
			started <- value
			<-ctx.Done()
			return value
		}, func(int) bool { return false })
	}()
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("workers did not start")
		}
	}
	cancel()
	select {
	case results := <-done:
		if !reflect.DeepEqual(results, []int{0, 1}) || len(started) != 0 {
			t.Fatalf("cancelled queue admitted more work: %v", results)
		}
	case <-time.After(time.Second):
		t.Fatal("workers did not exit after cancellation")
	}
}

func TestSkippedSamplesAreProgressNotNetworkFailures(t *testing.T) {
	for _, stage := range []string{StageTCP, StageHTTPS, StageQUIC, StageHTTP3} {
		t.Run(stage, func(t *testing.T) {
			progress := newProgress("skipped", time.Now(), nil)
			sample := sampleProbeFunc(func(_ context.Context, _ model.Candidate, n int, onAttempt func()) model.ProbeStats {
				onAttempt()
				return model.ProbeStats{Attempts: 1, Skipped: n - 1, Failures: map[model.FailureReason]int{model.FailureTimeout: 1}}
			})
			pipeline := stagePipeline{ctx: t.Context(), settings: config.DefaultSettings(), progress: progress, dependencies: Dependencies{TCP: sample, HTTPS: sample, QUIC: sample, HTTP3: sample}}
			candidate := model.Candidate{IP: "192.0.2.1", Port: 443}
			switch stage {
			case StageTCP:
				pipeline.tcp([]model.Candidate{candidate})
			case StageHTTPS:
				pipeline.https([]model.ProbeResult{{Candidate: candidate}})
			default:
				progress.start(stage, 0)
				h3 := h3Pipeline{stagePipeline: pipeline, cache: map[string]*h3StageCache{}}
				h3.probeBatch(stage, []model.ProbeResult{{Candidate: candidate, Endpoints: []model.EndpointResult{{IP: candidate.IP, QUIC: model.ProbeStats{SuccessRate: 1}}}}})
			}
			current := progress.snapshot()[0]
			if current.AttemptsCompleted != 1 || current.AttemptsSkipped != 2 || current.AttemptsTotal != 3 || current.Failed != 1 || progress.value.Failures[model.FailureTimeout] != 1 {
				t.Fatalf("skipped work inflated progress or failures: %+v %+v", current, progress.value.Failures)
			}
		})
	}
}

func TestH3RollingSharesInflightIPsAndLimitsPhysicalConcurrency(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan string, 10)
	release := make(chan struct{})
	var mu sync.Mutex
	active, peak := 0, 0
	calls := map[string]int{}
	probe := sampleProbeFunc(func(ctx context.Context, c model.Candidate, n int, onAttempt func()) model.ProbeStats {
		mu.Lock()
		calls[c.IP]++
		active++
		peak = max(peak, active)
		mu.Unlock()
		started <- c.IP
		select {
		case <-release:
		case <-ctx.Done():
		}
		mu.Lock()
		active--
		mu.Unlock()
		for range n {
			onAttempt()
		}
		return model.ProbeStats{Attempts: n, Successes: n, SuccessRate: 1}
	})
	settings := config.DefaultH3Settings()
	settings.HTTPConcurrency = 2
	p := h3Pipeline{stagePipeline: stagePipeline{ctx: ctx, settings: settings, progress: newProgress("shared", time.Now(), nil), dependencies: Dependencies{HTTP3: probe}}, cache: map[string]*h3StageCache{}}
	var groups []model.ProbeResult
	for _, host := range []string{"a.test", "b.test"} {
		r := model.ProbeResult{Candidate: model.Candidate{AddressType: model.AddressDomain, Host: host, Port: 443}}
		for _, ip := range []string{"192.0.2.1", "192.0.2.2", "192.0.2.3"} {
			r.Endpoints = append(r.Endpoints, model.EndpointResult{IP: ip, QUIC: model.ProbeStats{SuccessRate: 1}})
		}
		groups = append(groups, r)
	}
	done := make(chan []model.ProbeResult, 1)
	go func() { done <- p.selectStage(StageHTTP3, groups, 2) }()
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("physical probes did not start")
		}
	}
	close(release)
	select {
	case results := <-done:
		stage := p.progress.snapshot()[0]
		if len(results) != 2 || len(calls) != 3 || peak > 2 || active != 0 || stage.AttemptsTotal != 9 || stage.AttemptsCompleted != 9 {
			t.Fatalf("lost shared results or exceeded physical budget: peak=%d calls=%v stage=%+v", peak, calls, stage)
		}
		for ip, n := range calls {
			if n != 1 {
				t.Fatalf("shared IP %s probed %d times", ip, n)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("shared measurements did not finish")
	}
}

func TestQUICProgressIncludesQueuedIPs(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{}, 1)
	probe := sampleProbeFunc(func(ctx context.Context, _ model.Candidate, _ int, _ func()) model.ProbeStats {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return model.ProbeStats{}
	})
	settings := config.DefaultH3Settings()
	settings.ConnectionConcurrency = 1
	progress := newProgress("queued", time.Now(), nil)
	progress.start(StageQUIC, 0)
	p := h3Pipeline{stagePipeline: stagePipeline{ctx: ctx, settings: settings, progress: progress, dependencies: Dependencies{QUIC: probe}}, cache: map[string]*h3StageCache{}}
	done := make(chan struct{})
	go func() {
		p.probeBatch(StageQUIC, []model.ProbeResult{{Candidate: model.Candidate{Port: 443}, Endpoints: []model.EndpointResult{{IP: "192.0.2.1"}, {IP: "192.0.2.2"}, {IP: "192.0.2.3"}}}})
		close(done)
	}()
	select {
	case <-started:
		if total := progress.snapshot()[0].AttemptsTotal; total != 9 {
			t.Errorf("queued samples missing from progress: %d", total)
		}
	case <-time.After(time.Second):
		t.Error("probe did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled batch did not finish")
	}
}
