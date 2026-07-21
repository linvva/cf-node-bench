package ranking

import (
	"testing"

	"github.com/linvva/cf-node-bench/internal/model"
)

func TestScoreUsesHardAvailabilityGate(t *testing.T) {
	results := []model.ProbeResult{
		result("fast-unreliable", 0.5, 5, 5, 1000),
		result("balanced", 1, 20, 30, 100),
		result("slower", 1, 40, 50, 50),
	}
	ranked := Score(results, 2.0/3.0, 2.0/3.0)
	if len(ranked) != 2 {
		t.Fatalf("got %d eligible results", len(ranked))
	}
	if ranked[0].Candidate.IP != "balanced" {
		t.Fatalf("top = %s", ranked[0].Candidate.IP)
	}
	if ranked[0].Score < 0 || ranked[0].Score > 100 {
		t.Fatalf("score out of range: %f", ranked[0].Score)
	}
	if ranked[0].Parts.Bandwidth == ranked[1].Parts.Bandwidth {
		t.Fatal("bandwidth component not exposed")
	}
}

func TestScoreUsesIndependentSuccessThresholds(t *testing.T) {
	results := []model.ProbeResult{
		resultWithRates("https-weak", 1, 0.7),
		resultWithRates("stable", 0.9, 0.9),
	}
	ranked := Score(results, 0.8, 0.8)
	if len(ranked) != 1 || ranked[0].Candidate.IP != "stable" {
		t.Fatalf("unexpected ranked results: %+v", ranked)
	}
}

func result(ip string, success, tcp, https, mbps float64) model.ProbeResult {
	return model.ProbeResult{Candidate: model.Candidate{IP: ip}, TCP: model.ProbeStats{SuccessRate: success, P95MS: tcp}, HTTPS: model.ProbeStats{SuccessRate: success, P95MS: https, JitterMS: https / 10}, Bandwidth: model.BandwidthStats{Mbps: mbps}}
}

func resultWithRates(ip string, tcpSuccess, httpsSuccess float64) model.ProbeResult {
	return model.ProbeResult{Candidate: model.Candidate{IP: ip}, TCP: model.ProbeStats{SuccessRate: tcpSuccess, P95MS: 20}, HTTPS: model.ProbeStats{SuccessRate: httpsSuccess, P95MS: 30, JitterMS: 3}, Bandwidth: model.BandwidthStats{Mbps: 100}}
}
