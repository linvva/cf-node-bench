package ranking

import (
	"testing"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
)

func TestH3RankingIgnoresTCPAndUsesH3Gates(t *testing.T) {
	valid := model.ProbeResult{Candidate: model.Candidate{IP: "valid"}, QUIC: model.ProbeStats{SuccessRate: 1, P95MS: 20}, HTTP3: model.ProbeStats{SuccessRate: 1, P95MS: 30}, Bandwidth: model.BandwidthStats{Mbps: 100}}
	weakQUIC := valid
	weakQUIC.Candidate.IP = "weakQUIC"
	weakQUIC.QUIC.SuccessRate = .5
	weakHTTP := valid
	weakHTTP.Candidate.IP = "weakHTTP"
	weakHTTP.HTTP3.SuccessRate = .5
	failedDownload := valid
	failedDownload.Candidate.IP = "failed"
	failedDownload.Bandwidth.Failure = model.FailureTimeout
	results := ScoreH3([]model.ProbeResult{weakQUIC, weakHTTP, failedDownload, valid}, 2.0/3, 2.0/3, config.DefaultH3Settings().ScoreWeights)
	if len(results) != 1 || results[0].Candidate.IP != "valid" || results[0].Mode != model.ModeH3 || results[0].Score != 100 {
		t.Fatalf("ranked=%+v", results)
	}
	if results[0].Parts.TCP != 0 || results[0].Parts.HTTPS != 0 {
		t.Fatalf("legacy scores leaked: %+v", results[0].Parts)
	}
}

func TestH3RankingUsesSharedWeights(t *testing.T) {
	fast := model.ProbeResult{Candidate: model.Candidate{IP: "fast"}, QUIC: model.ProbeStats{SuccessRate: 1, P95MS: 10}, HTTP3: model.ProbeStats{SuccessRate: 1, P95MS: 20}, Bandwidth: model.BandwidthStats{Mbps: 10}}
	wide := fast
	wide.Candidate.IP = "wide"
	wide.QUIC.P95MS = 50
	wide.Bandwidth.Mbps = 100
	if results := ScoreH3([]model.ProbeResult{wide, fast}, 1, 1, model.ScoreWeights{Connection: 100}); results[0].Candidate.IP != "fast" || results[0].Score != 100 {
		t.Fatalf("latency order=%+v", results)
	}
	wide.HTTP3.P95MS = 50
	if results := ScoreH3([]model.ProbeResult{wide, fast}, 1, 1, model.ScoreWeights{HTTP: 100}); results[0].Candidate.IP != "fast" || results[0].Score != 100 {
		t.Fatalf("request latency order=%+v", results)
	}
	if results := ScoreH3([]model.ProbeResult{fast, wide}, 1, 1, model.ScoreWeights{Bandwidth: 100}); results[0].Candidate.IP != "wide" {
		t.Fatalf("bandwidth order=%+v", results)
	}
}
