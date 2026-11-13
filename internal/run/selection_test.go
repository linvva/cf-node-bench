package run

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
	"github.com/linvva/cf-node-bench/internal/source"
)

func TestHTTPSSelectsBestAcrossCompletePoolAndCanExpandFromCheckpoint(t *testing.T) {
	settings := config.DefaultSettings()
	settings.ConnectionConcurrency, settings.HTTPConcurrency, settings.BandwidthConcurrency = 1, 1, 1
	settings.HTTPCandidateCount, settings.BandwidthCandidates, settings.FinalResultCount = 2, 2, 2
	candidates := []model.Candidate{
		{IP: "192.0.2.1", Port: 443}, {IP: "192.0.2.2", Port: 443}, {IP: "192.0.2.3", Port: 443},
		{IP: "192.0.2.4", Port: 443}, {IP: "192.0.2.5", Port: 443},
	}
	latencies := map[string]float64{"192.0.2.1": 300, "192.0.2.2": 200, "192.0.2.3": 1, "192.0.2.4": 20, "192.0.2.5": 10}
	httpCalls := 0
	var downloaded []string
	engine := Engine{Dependencies: Dependencies{
		Fetcher: fakeFetcher{candidates: candidates}, TCP: fakeTCP{},
		HTTPS: sampleProbeFunc(func(_ context.Context, c model.Candidate, n int, attempt func()) model.ProbeStats {
			httpCalls++
			for range n {
				attempt()
			}
			rate := 1.0
			if c.IP == "192.0.2.3" {
				rate = 2.0 / 3
			}
			return model.ProbeStats{Attempts: n, SuccessRate: rate, P95MS: latencies[c.IP]}
		}),
		Bandwidth: downloadProbeFunc(func(_ context.Context, c model.Candidate) model.BandwidthStats {
			downloaded = append(downloaded, c.IP)
			return model.BandwidthStats{Bytes: 65536, Mbps: 100}
		}),
	}}
	var checkpoint Checkpoint
	var updates []model.RunProgress
	summary := engine.Run(t.Context(), "all-http", settings, RunInput{Sources: []source.HTTPSource{{Enabled: true}}, Plan: RunPlan{Save: func(c Checkpoint) error { checkpoint = c; return nil }}}, func(p model.RunProgress) { updates = append(updates, p) })
	if summary.State != "completed" || httpCalls != 5 || !reflect.DeepEqual(downloaded, []string{"192.0.2.5", "192.0.2.4"}) {
		t.Fatalf("did not select globally best HTTP candidates: state=%s calls=%d downloads=%v", summary.State, httpCalls, downloaded)
	}
	stage := completedStage(updates, StageHTTPS)
	if stage.Input != 5 || stage.Passed != 5 || stage.Selected == nil || *stage.Selected != 2 || len(checkpoint.HTTPSResults) != 5 || !checkpoint.HTTPSelectionComplete {
		t.Fatalf("lost full measurements or selection count: %+v checkpoint=%+v", stage, checkpoint)
	}
	for _, p := range updates {
		for _, s := range p.Stages {
			if s.Name == StageHTTPS && s.State == "running" && (s.Input != 5 || s.AttemptsTotal != 15) {
				t.Fatalf("full HTTP progress omitted queued candidates: %+v", s)
			}
		}
	}
	settings.HTTPCandidateCount, settings.BandwidthCandidates, settings.FinalResultCount = 4, 4, 4
	downloaded = nil
	resumed := engine.Run(t.Context(), "expanded-http", settings, RunInput{Plan: RunPlan{StartStage: StageBandwidth, Base: &checkpoint}}, nil)
	if resumed.StartStage != StageBandwidth || httpCalls != 5 || !reflect.DeepEqual(downloaded, []string{"192.0.2.5", "192.0.2.4", "192.0.2.2", "192.0.2.1"}) {
		t.Fatalf("expanded pool reused a truncated checkpoint: start=%s HTTP=%d downloads=%v", resumed.StartStage, httpCalls, downloaded)
	}
}

func TestDownloadQueuePlansTargetAndReplenishesByPassRate(t *testing.T) {
	var updates []model.StageQueue
	input := make([]int, 30)
	for i := range input {
		input[i] = i
	}
	results := parallelUntil(t.Context(), input, 1, 4, func(_ context.Context, i int) int { return i }, func(i int) bool { return i >= 2 }, func(q model.StageQueue) { updates = append(updates, q) })
	if len(results) != 6 || len(updates) == 0 || updates[0].Planned != 4 || updates[0].Pending != 4 || updates[0].Pool != 30 {
		t.Fatalf("initial queue or stopping condition incorrect: results=%v updates=%+v", results, updates)
	}
	batched := false
	for i := 1; i < len(updates); i++ {
		if updates[i].Planned-updates[i-1].Planned > 1 {
			batched = true
		}
		if updates[i].Active > 1 || updates[i].Planned > len(input) {
			t.Fatalf("queue exceeded capacity: %+v", updates[i])
		}
	}
	last := updates[len(updates)-1]
	if !batched || last.Passed != 4 || last.Active != 0 || last.Pending != 0 {
		t.Fatalf("adaptive batch refill or queue drain failed: %+v", updates)
	}
	parallelUntil(t.Context(), []int{1, 2}, 3, 30, func(_ context.Context, i int) int { return i }, func(int) bool { return true }, func(q model.StageQueue) { last = q })
	if last.Target != 30 || last.Passed != 2 || last.Planned != 2 || last.Pending != 0 || last.Active != 0 {
		t.Fatalf("exhausted candidates silently lowered the configured target: %+v", last)
	}
}

func TestDownloadQueueStopsAdmissionAtTargetAndKeepsInflightResults(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	release := make(chan struct{})
	reached := make(chan model.StageQueue, 1)
	done := make(chan []int, 1)
	go func() {
		done <- parallelUntil(ctx, []int{0, 1, 2, 3, 4}, 2, 2, func(ctx context.Context, i int) int {
			if i == 0 {
				select {
				case <-release:
				case <-ctx.Done():
				}
			}
			return i
		}, func(int) bool { return true }, func(q model.StageQueue) {
			if q.Passed == 2 && q.Active == 1 {
				select {
				case reached <- q:
				default:
				}
			}
		})
	}()
	select {
	case queue := <-reached:
		if queue.Pending != 0 {
			t.Errorf("reached target but still has queued work: %+v", queue)
		}
	case <-time.After(time.Second):
		cancel()
		<-done
		t.Fatal("download tail fell back to one worker")
	}
	select {
	case <-done:
		t.Fatal("returned before the running measurement completed")
	default:
	}
	close(release)
	select {
	case results := <-done:
		if !reflect.DeepEqual(results, []int{0, 1, 2}) {
			t.Fatalf("dropped in-flight results or admitted work past target: %v", results)
		}
	case <-time.After(time.Second):
		cancel()
		<-done
		t.Fatal("in-flight drain did not finish")
	}
}
