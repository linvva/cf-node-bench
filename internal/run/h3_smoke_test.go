package run

import (
	"os"
	"testing"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
	"github.com/linvva/cf-node-bench/internal/probe"
	"github.com/linvva/cf-node-bench/internal/source"
)

// Explicit opt-in acceptance check; public endpoint availability is never a CI assertion.
func TestH3NetworkSmoke(t *testing.T) {
	host, input := os.Getenv("CF_NODE_H3_SMOKE_WORKER"), os.Getenv("CF_NODE_H3_SMOKE_CANDIDATES")
	if input == "" {
		t.Skip("set CF_NODE_H3_SMOKE_CANDIDATES for a real-network check; CF_NODE_H3_SMOKE_WORKER optionally selects a verification domain")
	}
	settings := config.DefaultH3Settings()
	settings.VerificationHost = host
	if os.Getenv("CF_NODE_H3_SMOKE_FULL") != "1" {
		settings.MaxDownloadBytes = 128 * 1024
		settings.H3BandwidthTestURL = "https://h3.speed.cloudflare.com/__down?bytes=131072"
	}
	settings.Normalize()
	if err := settings.Validate(); err != nil {
		t.Fatal(err)
	}
	parsed := source.ParseH3([]byte(input), "smoke")
	if len(parsed.Failures) > 0 || len(parsed.Candidates) == 0 {
		t.Fatalf("invalid candidates: %+v", parsed.Failures)
	}
	engine := Engine{Dependencies: Dependencies{
		Fetcher:     fakeFetcher{candidates: parsed.Candidates},
		QUIC:        probe.QUICProber{Timeout: settings.ConnectTimeout(), WorkerHost: settings.EffectiveVerificationHost()},
		HTTP3:       probe.HTTP3Prober{ConnectTimeout: settings.ConnectTimeout(), RequestTimeout: settings.RequestTimeout(), WorkerHost: settings.EffectiveVerificationHost()},
		H3Bandwidth: probe.H3BandwidthProber{ConnectTimeout: settings.ConnectTimeout(), TotalTimeout: settings.BandwidthTimeout(), MaxBytes: settings.MaxDownloadBytes, URL: settings.EffectiveBandwidthTestURL()},
	}}
	summary := engine.Run(t.Context(), "network-smoke", settings, h3RunInput(), nil)
	if summary.State != "completed" {
		t.Fatalf("run state=%s", summary.State)
	}
	for _, r := range summary.Results {
		t.Logf("qualified=%s endpoints=%d QUIC=%.0fms H3=%.0fms download=%.2fMbps", r.Candidate.Key(), len(r.Endpoints), r.QUIC.P95MS, r.HTTP3.P95MS, r.Bandwidth.Mbps)
	}
	t.Logf("logical=%d physical=%d qualified=%d downloaded=%d failures=%v", summary.LogicalCandidates, summary.PhysicalCandidates, len(summary.Results), summary.DownloadedBytes, summary.Failures)
	expected := os.Getenv("CF_NODE_H3_SMOKE_EXPECT")
	if expected != "" {
		found := false
		for _, r := range summary.Results {
			if r.Candidate.Key() == expected {
				found = true
			}
		}
		if !found {
			t.Errorf("expected endpoint did not qualify: %s", expected)
		}
	}
	rejected := os.Getenv("CF_NODE_H3_SMOKE_REJECT")
	for _, r := range summary.Results {
		if r.Candidate.Key() == rejected {
			t.Errorf("endpoint expected to fail qualified: %s", rejected)
		}
	}
	if summary.Mode != model.ModeH3 {
		t.Fatal("incorrect mode")
	}
}
