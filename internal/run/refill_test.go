package run

import (
	"fmt"
	"testing"

	"github.com/linvva/cf-node-bench/internal/model"
)

func TestDownloadRefillsFromAllHTTPResultsAndCheckpoint(t *testing.T) {
	for _, h3 := range []bool{false, true} {
		t.Run(fmt.Sprintf("h3=%v", h3), func(t *testing.T) {
			var candidates []model.Candidate
			for i := 1; i <= 8; i++ {
				candidates = append(candidates, model.Candidate{AddressType: model.AddressIPv4, IP: fmt.Sprintf("192.0.2.%d", i), Port: 443})
			}
			goodPin := model.Candidate{AddressType: model.AddressIPv4, IP: "192.0.2.9", Port: 443}
			badPin := model.Candidate{AddressType: model.AddressIPv4, IP: "192.0.2.10", Port: 443}
			engine, settings, _, connection, http, bandwidth := h3Fixture(candidates)
			settings.RequireH3 = h3
			settings.HTTPCandidateCount, settings.BandwidthCandidates, settings.FinalResultCount = 2, 3, 3
			settings.ConnectionConcurrency, settings.HTTPConcurrency, settings.BandwidthConcurrency = 1, 1, 1
			engine.Dependencies.TCP, engine.Dependencies.HTTPS, engine.Dependencies.Bandwidth = connection, http, bandwidth
			for _, c := range candidates[:4] {
				bandwidth.failures[c.Key()] = true
			}
			bandwidth.failures[badPin.Key()] = true
			input := h3RunInput()
			input.Pinned = []model.Candidate{goodPin, badPin}
			var checkpoint Checkpoint
			input.Plan.Save = func(c Checkpoint) error { checkpoint = c; return nil }
			for _, resumed := range []bool{false, true} {
				if resumed {
					settings.HTTPCandidateCount = 1
					input.Plan = RunPlan{StartStage: StageBandwidth, Base: &checkpoint}
				}
				bandwidth.calls = map[string]int{}
				var updates []model.RunProgress
				summary := engine.Run(t.Context(), fmt.Sprintf("refill-%v", resumed), settings, input, func(p model.RunProgress) { updates = append(updates, p) })
				failed := findResult(summary.Results, badPin.Key())
				if summary.State != "completed" || len(summary.Results) != 5 || failed == nil || failed.Status != "failed_gate" {
					t.Fatalf("refill lost ordinary or pinned results: %+v", summary)
				}
				if len(bandwidth.calls) != 9 || bandwidth.calls[candidates[7].Key()] != 0 {
					t.Fatalf("stopped at initial batch or continued after target: %v", bandwidth.calls)
				}
				for _, c := range append(append([]model.Candidate(nil), candidates[:7]...), goodPin, badPin) {
					if bandwidth.calls[c.Key()] != 1 || connection.calls[c.Key()] != 1 || http.calls[c.Key()] != 1 {
						t.Fatalf("missing, repeated, or reused-stage probes: node=%s download=%v connection=%v HTTP=%v", c.Key(), bandwidth.calls, connection.calls, http.calls)
					}
				}
				queue := completedStage(updates, StageBandwidth)
				wantPool := 8
				if resumed {
					wantPool = 7
				}
				if queue.Queue == nil || queue.Queue.Pool != wantPool || queue.Queue.Target != 3 || queue.Queue.Passed != 3 || queue.Input != 9 {
					t.Fatalf("batch progress included extra pins or reset passes: %+v", queue)
				}
				httpStage := StageHTTPS
				if h3 {
					httpStage = StageHTTP3
				}
				var selected *int
				for _, stage := range updates[len(updates)-1].Stages {
					if stage.Name == httpStage {
						selected = stage.Selected
					}
				}
				if selected == nil || *selected != wantPool {
					t.Fatalf("HTTP selection did not grow with the download pool: %v", selected)
				}
			}
		})
	}
}
