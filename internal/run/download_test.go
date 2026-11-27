package run

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
	"github.com/linvva/cf-node-bench/internal/source"
)

func TestQuickSelectionStopsAfterFillingTarget(t *testing.T) {
	for _, h3 := range []bool{false, true} {
		t.Run(fmt.Sprintf("h3=%t", h3), func(t *testing.T) {
			candidates := downloadCandidates(8)
			engine, settings := cancellationFixture(h3, candidates)
			settings.HTTPCandidateCount, settings.BandwidthCandidates, settings.FinalResultCount = 2, 2, 2
			settings.BandwidthConcurrency = 1
			connection := &h3ProbeStub{calls: map[string]int{}}
			http := &h3ProbeStub{calls: map[string]int{}}
			download := &h3BandwidthStub{calls: map[string]int{}, failures: map[string]bool{candidates[0].Key(): true}}
			engine.Dependencies.TCP, engine.Dependencies.QUIC = connection, connection
			engine.Dependencies.HTTPS, engine.Dependencies.HTTP3 = http, http
			engine.Dependencies.Bandwidth, engine.Dependencies.H3Bandwidth = download, download
			input := h3RunInput()
			input.LocalFiles = &source.ParseResult{Candidates: slices.Clone(candidates)}
			input.Pinned = []model.Candidate{candidates[7]}
			input.Retained = []model.ProbeResult{{Candidate: candidates[6], TCP: model.ProbeStats{SuccessRate: 1}, Status: "qualified"}}
			input.Plan.Quick = true
			var saved Checkpoint
			input.Plan.Save = func(checkpoint Checkpoint) error {
				saved = checkpoint
				return nil
			}
			summary := engine.Run(t.Context(), "quick", settings, input, nil)
			if !summary.Quick || !summary.LocalFiles || len(summary.Results) != 3 || saved.HTTPSelectionComplete || saved.Download != nil {
				t.Fatalf("unexpected final quick run: state=%s results=%d full HTTP=%t", summary.State, len(summary.Results), saved.HTTPSelectionComplete)
			}
			if len(http.calls) != 6 || http.calls[candidates[4].Key()] != 0 || download.calls[candidates[6].Key()] != 1 || download.calls[candidates[7].Key()] != 1 {
				t.Fatalf("later batch or extra nodes were handled incorrectly: HTTP=%v downloads=%v", http.calls, download.calls)
			}
			if len(connection.calls) != 8 && (h3 || len(connection.calls) != 7) {
				t.Fatalf("unexpected connection probes: %v", connection.calls)
			}
			if restart := incompleteHTTPRestartStage(saved, settings, StageBandwidth); restart == "" {
				t.Fatal("an incomplete HTTP pool should require new HTTP measurements")
			}
			if restart := incompleteHTTPRestartStage(saved, settings, StageRanking); restart != "" {
				t.Fatalf("ranking should reuse completed quick downloads, got %q", restart)
			}
		})
	}
}

func TestBandwidthProgressContinuesWithoutRepeatingMeasurements(t *testing.T) {
	for _, h3 := range []bool{false, true} {
		for _, quick := range []bool{false, true} {
			t.Run(fmt.Sprintf("h3=%t/quick=%t", h3, quick), func(t *testing.T) {
				candidates := downloadCandidates(6)
				engine, settings := cancellationFixture(h3, candidates)
				settings.HTTPCandidateCount, settings.BandwidthCandidates, settings.FinalResultCount = 2, 3, 3
				settings.BandwidthConcurrency = 1
				http := &h3ProbeStub{calls: map[string]int{}}
				engine.Dependencies.HTTPS, engine.Dependencies.HTTP3 = http, http
				input := h3RunInput()
				input.Plan.Quick = quick
				var encoded []byte
				input.Plan.Save = func(checkpoint Checkpoint) error {
					var err error
					encoded, err = json.Marshal(checkpoint)
					return err
				}
				run := func(id string, stopAt int) (model.RunSummary, Checkpoint) {
					ctx, cancel := context.WithCancelCause(t.Context())
					defer cancel(nil)
					probe := downloadProbeFunc(func(_ context.Context, c model.Candidate) model.BandwidthStats {
						if stopAt > 0 && c.Key() == candidates[stopAt].Key() {
							cancel(errSaveDownload)
							return model.BandwidthStats{Failure: model.FailureCancelled}
						}
						if c.Key() == candidates[0].Key() {
							return model.BandwidthStats{Failure: model.FailureBandwidthBelowMin}
						}
						return model.BandwidthStats{Bytes: 1024 * 1024, Mbps: 100}
					})
					engine.Dependencies.Bandwidth, engine.Dependencies.H3Bandwidth = probe, probe
					summary := engine.Run(ctx, id, settings, input, nil)
					var saved Checkpoint
					if err := json.Unmarshal(encoded, &saved); err != nil {
						t.Fatal(err)
					}
					return summary, saved
				}
				first, saved := run("first", 1)
				if first.State != "cancelled" || !first.BandwidthSaved || len(first.Results) != 0 || saved.Download == nil || len(saved.Download.Measurements) != 1 {
					t.Fatalf("first stop should save one failed measurement: summary=%+v", first)
				}
				option := DownloadResumeInfo(saved, settings, input.Plan.Network, input.Sources, nil, time.Now())
				if option == nil || !option.Available || option.Input != 1 {
					t.Fatalf("unexpected continuation: %+v", option)
				}
				changed := settings
				changed.MinBandwidthMbps++
				if option := DownloadResumeInfo(saved, changed, input.Plan.Network, input.Sources, nil, time.Now()); option == nil || option.Available {
					t.Fatalf("changed threshold should require new measurements: %+v", option)
				}
				input.Plan.Base, input.Plan.ContinueBandwidth = &saved, true
				second, next := run("second", 3)
				if second.State != "cancelled" || !second.BandwidthSaved || next.Download == nil || len(next.Download.Measurements) != 3 {
					t.Fatalf("second stop should keep all three finished measurements: summary=%+v", second)
				}
				input.Plan.Base = &next
				calls := &h3BandwidthStub{calls: map[string]int{}}
				engine.Dependencies.Bandwidth, engine.Dependencies.H3Bandwidth = calls, calls
				final := engine.Run(t.Context(), "final", settings, input, nil)
				if final.State != "completed" || final.Quick != quick || len(final.Results) != 3 {
					t.Fatalf("unexpected resumed summary: %+v", final)
				}
				for _, candidate := range candidates[:3] {
					if calls.calls[candidate.Key()] != 0 || http.calls[candidate.Key()] != 1 {
						t.Fatalf("the completed HTTP and bandwidth work was repeated: HTTP=%v downloads=%v", http.calls, calls.calls)
					}
				}
			})
		}
	}
}

func TestDownloadPreflightSamplesUniqueAddressesAndKeepsDiagnosticsSeparate(t *testing.T) {
	candidates := downloadCandidates(7)
	settings := config.DefaultSettings()
	settings.BandwidthTestURL = "https://downloads.example.test/test"
	probe := downloadProbeFunc(func(_ context.Context, candidate model.Candidate) model.BandwidthStats {
		switch candidate.Key() {
		case candidates[0].Key():
			return model.BandwidthStats{Failure: model.FailureTLSHandshakeTimeout}
		case candidates[1].Key():
			return model.BandwidthStats{Failure: model.FailureHTTPStatus}
		case candidates[2].Key():
			return model.BandwidthStats{Bytes: DownloadCheckBytes - 1}
		default:
			return model.BandwidthStats{Bytes: DownloadCheckBytes}
		}
	})
	duplicates := append([]model.Candidate{candidates[0]}, candidates...)
	check := TestDownload(t.Context(), settings, duplicates, nil, probe)
	if check.Host != "downloads.example.test" || check.Sampled != 5 || check.Passed != 2 ||
		check.Failures[model.FailureTLSHandshakeTimeout] != 1 || check.Failures[model.FailureHTTPStatus] != 1 || check.Failures[model.FailureDownload] != 1 {
		t.Fatalf("unexpected preflight: %+v", check)
	}
	engine, _ := cancellationFixture(false, candidates)
	engine.Dependencies.DownloadCheck = probe
	summary := engine.Run(t.Context(), "preflight", settings, h3RunInput(), nil)
	if summary.DownloadCheck == nil || summary.DownloadCheck.Sampled != 5 || summary.Failures[model.FailureTLSHandshakeTimeout] != 0 {
		t.Fatalf("preflight diagnostics must stay out of run totals: %+v", summary)
	}
}

func downloadCandidates(count int) []model.Candidate {
	candidates := make([]model.Candidate, count)
	for i := range candidates {
		candidates[i] = model.Candidate{AddressType: model.AddressIPv4, IP: fmt.Sprintf("192.0.2.%d", i+1), Port: 443}
	}
	return candidates
}

func TestH3ContinuationReusesCompletedEndpointsOfAnUnfinishedDomain(t *testing.T) {
	domain := model.Candidate{AddressType: model.AddressDomain, Host: "pool.test", Port: 443}
	engine, settings, _, _, _, _ := h3Fixture([]model.Candidate{domain})
	settings.BandwidthCandidates, settings.FinalResultCount, settings.BandwidthConcurrency = 1, 1, 1
	ctx, stop := context.WithCancelCause(t.Context())
	defer stop(nil)
	input := h3RunInput()
	var saved Checkpoint
	input.Plan.Save = func(checkpoint Checkpoint) error {
		saved = checkpoint
		return nil
	}
	engine.Dependencies.H3Bandwidth = downloadProbeFunc(func(_ context.Context, candidate model.Candidate) model.BandwidthStats {
		if candidate.IP == "192.0.2.2" {
			stop(errSaveDownload)
			return model.BandwidthStats{Failure: model.FailureCancelled}
		}
		return model.BandwidthStats{Bytes: 1024 * 1024, Mbps: 100}
	})
	if summary := engine.Run(ctx, "partial-domain", settings, input, nil); !summary.BandwidthSaved {
		t.Fatalf("expected saved bandwidth, got state %q", summary.State)
	}
	if len(saved.Download.Measurements) != 1 || saved.Download.Finished[domain.Key()] {
		t.Fatalf("only one endpoint should be complete: %+v", saved.Download)
	}
	calls := &h3BandwidthStub{calls: map[string]int{}}
	engine.Dependencies.H3Bandwidth = calls
	input.Plan.Base, input.Plan.ContinueBandwidth = &saved, true
	summary := engine.Run(t.Context(), "resumed-domain", settings, input, nil)
	initial, remaining := model.Candidate{IP: "192.0.2.1", Port: 443}, model.Candidate{IP: "192.0.2.2", Port: 443}
	if len(summary.Results) != 1 || len(summary.Results[0].Endpoints) != 2 || calls.calls[initial.Key()] != 0 || calls.calls[remaining.Key()] != 1 {
		t.Fatalf("expected the remaining IPv4 to complete the domain: results=%v probes=%v", summary.Results, calls.calls)
	}
}
