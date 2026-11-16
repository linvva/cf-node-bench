package run

import (
	"testing"

	"github.com/linvva/cf-node-bench/internal/model"
	"github.com/linvva/cf-node-bench/internal/source"
)

func TestPinnedNodesAreExtraAndSurviveEveryGate(t *testing.T) {
	for _, h3 := range []bool{false, true} {
		for _, failure := range []string{StageTCP, StageHTTPS, StageBandwidth} {
			name := string(model.ModeTCPHTTPS)
			if h3 {
				name = string(model.ModeH3)
			}
			t.Run(name+"/"+failure, func(t *testing.T) {
				goodPin := model.Candidate{AddressType: model.AddressIPv4, IP: "192.0.2.1", Port: 8443, Country: "CN", SourceID: "saved"}
				badPin := model.Candidate{AddressType: model.AddressIPv4, IP: "192.0.2.2", Port: 8443, Country: "CN"}
				freshPin := goodPin
				freshPin.SourceID = "current"
				candidates := []model.Candidate{freshPin, {AddressType: model.AddressIPv4, IP: "192.0.2.10", Port: 443}, {AddressType: model.AddressIPv4, IP: "192.0.2.11", Port: 443}}
				engine, settings, _, connection, http, bandwidth := h3Fixture(candidates)
				settings.RequireH3 = h3
				settings.ConnectionConcurrency, settings.HTTPConcurrency, settings.BandwidthConcurrency = 1, 1, 1
				settings.HTTPCandidateCount, settings.BandwidthCandidates, settings.FinalResultCount = 1, 1, 1
				settings.AllowedPorts, settings.BlockedCountries = []int{443}, []string{"CN"}
				engine.Dependencies.TCP, engine.Dependencies.HTTPS, engine.Dependencies.Bandwidth = connection, http, bandwidth
				switch failure {
				case StageTCP:
					connection.failures[badPin.Key()] = true
				case StageHTTPS:
					http.failures[badPin.Key()] = true
				default:
					bandwidth.failures[badPin.Key()] = true
				}
				input := h3RunInput()
				input.Pinned = []model.Candidate{goodPin, badPin, goodPin}
				input.Retained = []model.ProbeResult{{Mode: settings.ProbeMode(), Candidate: goodPin, TCP: model.ProbeStats{SuccessRate: 1, P95MS: 9999}}}
				var updates []model.RunProgress
				summary := engine.Run(t.Context(), "pinned", settings, input, func(p model.RunProgress) { updates = append(updates, p) })
				if len(summary.Results) != 3 || summary.State != "completed" {
					t.Fatalf("pinned nodes consumed ordinary result slots or disappeared: %+v", summary)
				}
				good := findResult(summary.Results, goodPin.Key())
				bad := findResult(summary.Results, badPin.Key())
				if good == nil || !good.Pinned || good.Status != "qualified" || good.Candidate.SourceID != "current" || good.Score == 0 {
					t.Fatalf("healthy pinned node lost metadata or valid score: %+v", good)
				}
				if bad == nil || !bad.Pinned || bad.Status != "failed_gate" || bad.Score != 0 || bad.Parts != (model.ScoreParts{}) {
					t.Fatalf("failed pinned node was lost or presented as qualified: %+v", bad)
				}
				for _, calls := range []map[string]int{connection.calls, http.calls, bandwidth.calls} {
					if calls[goodPin.Key()] != 1 || calls[badPin.Key()] != 1 {
						t.Fatalf("pinned node skipped a stage or was measured more than once: %+v", calls)
					}
				}
				stage := completedStage(updates, StageBandwidth)
				if stage.Queue == nil || stage.Queue.Pool != 1 || stage.Queue.Target != 1 || stage.Queue.Passed != 1 || stage.Input != 3 {
					t.Fatalf("pinned downloads consumed fresh quota: %+v", stage)
				}
				if rankingStage := completedStage(updates, StageRanking); rankingStage.Passed != 2 || rankingStage.Failed != 1 {
					t.Fatalf("failed pinned node counted as ranking success: %+v", rankingStage)
				}
			})
		}
	}
}

func TestPinnedNodesSurviveStageResumesAndCanBeUnpinned(t *testing.T) {
	for _, h3 := range []bool{false, true} {
		name := string(model.ModeTCPHTTPS)
		if h3 {
			name = string(model.ModeH3)
		}
		t.Run(name, func(t *testing.T) {
			pin := model.Candidate{AddressType: model.AddressIPv4, IP: "192.0.2.2", Port: 443}
			fresh := model.Candidate{AddressType: model.AddressIPv4, IP: "192.0.2.10", Port: 443}
			engine, settings, _, connection, http, bandwidth := h3Fixture([]model.Candidate{pin, fresh})
			settings.RequireH3 = h3
			settings.ConnectionConcurrency, settings.HTTPConcurrency, settings.BandwidthConcurrency = 1, 1, 1
			settings.HTTPCandidateCount, settings.BandwidthCandidates, settings.FinalResultCount = 1, 1, 1
			connection.failures[pin.Key()] = true
			engine.Dependencies.TCP, engine.Dependencies.HTTPS, engine.Dependencies.Bandwidth = connection, http, bandwidth
			input := h3RunInput()
			input.Pinned = []model.Candidate{pin}
			var checkpoint Checkpoint
			input.Plan.Save = func(c Checkpoint) error { checkpoint = c; return nil }
			initial := engine.Run(t.Context(), "initial", settings, input, nil)
			connectionStage, httpStage := StageTCP, StageHTTPS
			if h3 {
				connectionStage, httpStage = StageQUIC, StageHTTP3
			}
			for _, stage := range []string{connectionStage, httpStage, StageBandwidth, StageRanking} {
				beforeConnection, beforeHTTP, beforeBandwidth := connection.calls[pin.Key()], http.calls[pin.Key()], bandwidth.calls[pin.Key()]
				resumed := engine.Run(t.Context(), "resume-"+stage, settings, RunInput{Pinned: []model.Candidate{pin}, Plan: RunPlan{StartStage: stage, Base: &checkpoint}}, nil)
				if len(resumed.Results) != 2 || findResult(resumed.Results, pin.Key()) == nil || findResult(resumed.Results, pin.Key()).Status != "failed_gate" {
					t.Fatalf("%s dropped failed pinned node: %+v", stage, resumed)
				}
				connectionCalls, httpCalls, bandwidthCalls := 0, 0, 0
				if stage == connectionStage {
					connectionCalls = 1
				}
				if stage == connectionStage || stage == httpStage {
					httpCalls = 1
				}
				if stage != StageRanking {
					bandwidthCalls = 1
				}
				if connection.calls[pin.Key()]-beforeConnection != connectionCalls || http.calls[pin.Key()]-beforeHTTP != httpCalls || bandwidth.calls[pin.Key()]-beforeBandwidth != bandwidthCalls {
					t.Fatalf("%s did not respect reused stages", stage)
				}
			}
			unpinned := engine.Run(t.Context(), "unpin", settings, RunInput{Plan: RunPlan{StartStage: StageRanking, Base: &checkpoint}}, nil)
			if len(unpinned.Results) != 1 || findResult(unpinned.Results, pin.Key()) != nil {
				t.Fatalf("old pinned flag survived removal: %+v", unpinned)
			}
			engine.Dependencies.Fetcher = fakeFetcher{candidates: []model.Candidate{}}
			before := connection.calls[pin.Key()]
			next := engine.Run(t.Context(), "next-full-run", settings, RunInput{Sources: input.Sources, Retained: initial.Results}, nil)
			if findResult(next.Results, pin.Key()) != nil || connection.calls[pin.Key()] != before {
				t.Fatalf("failed node was automatically retained after unpinning: %+v", next)
			}
		})
	}
}

func TestPinnedH3DNSFailureAndLocalFileRun(t *testing.T) {
	pin := model.Candidate{AddressType: model.AddressDomain, Host: "missing.test", Port: 8443, Country: "CN"}
	fresh := model.Candidate{AddressType: model.AddressIPv4, IP: "192.0.2.10", Port: 443}
	engine, settings, _, _, _, _ := h3Fixture(nil)
	engine.Dependencies.Fetcher = nil
	settings.BlockedCountries = []string{"CN"}
	settings.AllowedPorts = []int{443}
	var updates []model.RunProgress
	summary := engine.Run(t.Context(), "local-pinned", settings, RunInput{LocalFiles: &source.ParseResult{Candidates: []model.Candidate{fresh}}, Pinned: []model.Candidate{pin}}, func(p model.RunProgress) { updates = append(updates, p) })
	result := findResult(summary.Results, pin.Key())
	if !summary.LocalFiles || len(summary.Results) != 2 || result == nil || result.Status != "failed_gate" || !result.Pinned || len(result.Endpoints) != 0 || summary.Failures[model.FailureDNS] != 1 {
		t.Fatalf("DNS failure dropped pinned domain: %+v", summary)
	}
	dns := completedStage(updates, "dns")
	if dns.Input != 2 || dns.Passed != 1 || dns.Failed != 1 {
		t.Fatalf("retained DNS failure counted as success: %+v", dns)
	}
	if summary.Failures[model.FailureQUIC] != 0 || summary.Failures[model.FailureProtocol] != 0 || summary.Failures[model.FailureDownload] != 0 {
		t.Fatalf("unresolved node recorded unattempted probe failures: %+v", summary.Failures)
	}
}

func TestPinnedResumeRequiresEveryReusedMeasurement(t *testing.T) {
	pin := model.Candidate{IP: "192.0.2.1", Port: 443}
	checkpoint := Checkpoint{Candidates: []model.Candidate{pin}, TCPResults: []model.ProbeResult{{Candidate: pin}}}
	info := ResumeInfo{Options: []ResumeOption{{Stage: StageTCP, Available: true}, {Stage: StageHTTPS, Available: true}, {Stage: StageBandwidth, Available: true}, {Stage: StageRanking, Available: true}}}
	checked := ApplyPinnedResumeInfo(info, checkpoint, []model.Candidate{pin})
	for _, option := range checked.Options {
		want := option.Stage == StageTCP || option.Stage == StageHTTPS
		if option.Available != want || !want && len(option.Warnings) == 0 {
			t.Fatalf("unsafe resume availability: %+v", option)
		}
	}
	if !info.Options[2].Available {
		t.Fatal("mutated caller's resume options")
	}
}
