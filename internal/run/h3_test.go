package run

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
	"github.com/linvva/cf-node-bench/internal/source"
)

type h3ResolverStub struct {
	records map[string][]string
	calls   map[string]int
}

func (r *h3ResolverStub) LookupNetIP(_ context.Context, network, host string) ([]netip.Addr, error) {
	if network != "ip4" {
		panic("unexpected DNS family")
	}
	r.calls[host]++
	var values []netip.Addr
	for _, ip := range r.records[host] {
		values = append(values, netip.MustParseAddr(ip))
	}
	if len(values) == 0 {
		return nil, errors.New("no IPv4")
	}
	return values, nil
}

type h3ProbeStub struct {
	mu       sync.Mutex
	calls    map[string]int
	failures map[string]bool
	block    bool
}

func (p *h3ProbeStub) Probe(ctx context.Context, c model.Candidate, n int, attempt func()) model.ProbeStats {
	p.mu.Lock()
	p.calls[c.Key()]++
	p.mu.Unlock()
	if p.block {
		<-ctx.Done()
		return model.ProbeStats{Attempts: n}
	}
	for range n {
		attempt()
	}
	if p.failures[c.Key()] {
		return model.ProbeStats{Attempts: n, Failures: map[model.FailureReason]int{model.FailureQUIC: n}}
	}
	ip := netip.MustParseAddr(c.IP).As4()
	latency := float64(ip[3]) * 10
	return model.ProbeStats{Attempts: n, Successes: n, SuccessRate: 1, AverageMS: latency, P50MS: latency, P95MS: latency, JitterMS: latency / 10}
}

type h3BandwidthStub struct {
	mu       sync.Mutex
	calls    map[string]int
	failures map[string]bool
}

func (p *h3BandwidthStub) Probe(_ context.Context, c model.Candidate) model.BandwidthStats {
	p.mu.Lock()
	p.calls[c.Key()]++
	p.mu.Unlock()
	if p.failures[c.Key()] {
		return model.BandwidthStats{Failure: model.FailureDownload}
	}
	ip := netip.MustParseAddr(c.IP).As4()
	return model.BandwidthStats{Bytes: 128 * 1024, Mbps: 100 / float64(ip[3]), TTFBMS: float64(ip[3]) * 10, DurationMS: 100}
}
func h3Fixture(candidates []model.Candidate) (Engine, config.Settings, *h3ResolverStub, *h3ProbeStub, *h3ProbeStub, *h3BandwidthStub) {
	resolver := &h3ResolverStub{records: map[string][]string{"pool.test": {"192.0.2.1", "192.0.2.2"}, "alias.test": {"192.0.2.1"}}, calls: map[string]int{}}
	quic := &h3ProbeStub{calls: map[string]int{}, failures: map[string]bool{}}
	http3 := &h3ProbeStub{calls: map[string]int{}, failures: map[string]bool{}}
	band := &h3BandwidthStub{calls: map[string]int{}, failures: map[string]bool{}}
	settings := config.DefaultH3Settings()
	settings.VerificationHost = "worker.test"
	return Engine{Dependencies: Dependencies{Fetcher: fakeFetcher{candidates: candidates}, Resolver: resolver, QUIC: quic, HTTP3: http3, H3Bandwidth: band}}, settings, resolver, quic, http3, band
}
func h3RunInput() RunInput {
	return RunInput{Sources: []source.HTTPSource{{ID: "input", Enabled: true}}}
}
func TestH3DomainAllAddressesAndSharedMeasurements(t *testing.T) {
	candidates := []model.Candidate{{AddressType: model.AddressDomain, Host: "pool.test", Port: 443}, {AddressType: model.AddressDomain, Host: "alias.test", Port: 443}, {AddressType: model.AddressIPv4, IP: "192.0.2.1", Port: 443}}
	engine, settings, resolver, quic, http3, band := h3Fixture(candidates)
	result := engine.Run(t.Context(), "h3", settings, h3RunInput(), nil)
	if result.Mode != model.ModeH3 || result.WorkerHost != "worker.test" || len(result.Results) != 3 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.LogicalCandidates != 3 || result.PhysicalCandidates != 2 || result.DownloadedBytes != 256*1024 {
		t.Fatalf("wrong counts: %+v", result)
	}
	if resolver.calls["pool.test"] != 1 || resolver.calls["alias.test"] != 1 {
		t.Fatal(resolver.calls)
	}
	for _, calls := range []map[string]int{quic.calls, http3.calls, band.calls} {
		if len(calls) != 2 || calls["192.0.2.1:443"] != 1 || calls["192.0.2.2:443"] != 1 {
			t.Fatalf("duplicated or lost probe: %+v", calls)
		}
	}
	for _, r := range result.Results {
		if r.Candidate.Host == "pool.test" {
			if len(r.Endpoints) != 2 || r.QUIC.P95MS != 20 || r.HTTP3.P95MS != 20 || r.Bandwidth.Mbps != 50 || r.Candidate.IP != "" || r.ResolvedAt.IsZero() || r.TCP.Attempts != 0 {
				t.Fatalf("incorrect aggregation: %+v", r)
			}
		}
	}
}
func TestH3DomainRejectedWhenAnyAddressFails(t *testing.T) {
	for _, failedStage := range []string{StageQUIC, StageHTTP3, StageBandwidth} {
		t.Run(failedStage, func(t *testing.T) {
			engine, settings, _, quic, http3, band := h3Fixture([]model.Candidate{{AddressType: model.AddressDomain, Host: "pool.test", Port: 443}, {AddressType: model.AddressIPv4, IP: "192.0.2.1", Port: 443}})
			switch failedStage {
			case StageQUIC:
				quic.failures["192.0.2.2:443"] = true
			case StageHTTP3:
				http3.failures["192.0.2.2:443"] = true
			default:
				band.failures["192.0.2.2:443"] = true
			}
			result := engine.Run(t.Context(), "partial", settings, h3RunInput(), nil)
			if len(result.Results) != 1 || result.Results[0].Candidate.IP != "192.0.2.1" {
				t.Fatalf("partially available domain escaped gate: %+v", result)
			}
		})
	}
}
func TestH3FillsCandidatesAfterFailures(t *testing.T) {
	var candidates []model.Candidate
	for _, ip := range []string{"192.0.2.1", "192.0.2.2", "192.0.2.3"} {
		candidates = append(candidates, model.Candidate{AddressType: model.AddressIPv4, IP: ip, Port: 443})
	}
	engine, settings, _, _, http3, band := h3Fixture(candidates)
	settings.HTTPCandidateCount = 2
	settings.BandwidthCandidates = 1
	settings.FinalResultCount = 1
	http3.failures["192.0.2.1:443"] = true
	band.failures["192.0.2.2:443"] = true
	result := engine.Run(t.Context(), "fill", settings, h3RunInput(), nil)
	if len(result.Results) != 1 || result.Results[0].Candidate.IP != "192.0.2.3" {
		t.Fatalf("did not fill from remaining candidates: %+v", result)
	}
}
func TestH3DNSFailureAndIPv6Excluded(t *testing.T) {
	engine, settings, resolver, _, _, _ := h3Fixture([]model.Candidate{{AddressType: model.AddressDomain, Host: "v6.test", Port: 443}, {AddressType: model.AddressDomain, Host: "empty.test", Port: 443}})
	resolver.records["v6.test"] = []string{"2001:db8::1"}
	result := engine.Run(t.Context(), "dns", settings, h3RunInput(), nil)
	if len(result.Results) != 0 || result.Failures[model.FailureDNS] != 2 {
		t.Fatalf("unexpected DNS results: %+v", result)
	}
}
func TestH3ResumeRefreshesDNSAndNeverMutatesBase(t *testing.T) {
	engine, settings, resolver, _, _, band := h3Fixture([]model.Candidate{{AddressType: model.AddressDomain, Host: "pool.test", Port: 443}})
	input := h3RunInput()
	var checkpoint Checkpoint
	input.Plan.Save = func(c Checkpoint) error { checkpoint = c; return nil }
	engine.Run(t.Context(), "original", settings, input, nil)
	original := cloneH3Results(checkpoint.BandwidthResults)
	before := len(band.calls)
	rankingInput := h3RunInput()
	rankingInput.Plan = RunPlan{StartStage: StageRanking, Base: &checkpoint}
	summary := engine.Run(t.Context(), "ranking", settings, rankingInput, nil)
	if len(summary.Results) != 1 || resolver.calls["pool.test"] != 1 || len(band.calls) != before {
		t.Fatal("ranking performed network calls")
	}
	if summary.DownloadedBytes != 0 {
		t.Fatal("ranking counted reused bytes as newly downloaded")
	}
	resolver.records["pool.test"] = []string{"192.0.2.3"}
	quicInput := h3RunInput()
	quicInput.Plan = RunPlan{StartStage: StageQUIC, Base: &checkpoint}
	summary = engine.Run(t.Context(), "new-dns", settings, quicInput, nil)
	if resolver.calls["pool.test"] != 2 || len(summary.Results) != 1 || summary.Results[0].Endpoints[0].IP != "192.0.2.3" {
		t.Fatalf("DNS not refreshed: %+v", summary)
	}
	if checkpoint.BandwidthResults[0].Endpoints[0].IP != original[0].Endpoints[0].IP || checkpoint.BandwidthResults[0].Bandwidth != original[0].Bandwidth {
		t.Fatal("base checkpoint mutated")
	}
}
func TestH3ResumeAfterVerificationHostOrProtocolChange(t *testing.T) {
	engine, settings, _, _, _, _ := h3Fixture([]model.Candidate{{AddressType: model.AddressIPv4, IP: "192.0.2.1", Port: 443}})
	input := h3RunInput()
	var checkpoint Checkpoint
	input.Plan.Save = func(c Checkpoint) error { checkpoint = c; return nil }
	engine.Run(t.Context(), "original", settings, input, nil)
	settings.VerificationHost = "other.test"
	info := BuildResumeInfo(checkpoint, "original", settings, input.Sources, NetworkSnapshot{}, time.Now())
	for _, o := range info.Options {
		if o.Stage == StageQUIC && !o.Available {
			t.Fatal("QUIC should permit fresh probe")
		}
		if o.Stage != StageQUIC && o.Available {
			t.Fatalf("cross-worker reuse allowed: %+v", o)
		}
	}
	info = BuildResumeInfo(checkpoint, "original", config.DefaultSettings(), input.Sources, NetworkSnapshot{}, time.Now())
	if len(info.Options) != 4 || !info.Options[0].Available || info.Options[0].Stage != StageTCP {
		t.Fatalf("candidate reuse unavailable: %+v", info)
	}
	for _, option := range info.Options[1:] {
		if option.Available {
			t.Fatalf("cross-protocol measured data reuse allowed: %+v", option)
		}
	}
}
func TestH3CancellationPreservesOnlyCompleteStages(t *testing.T) {
	engine, settings, _, quic, _, _ := h3Fixture([]model.Candidate{{AddressType: model.AddressIPv4, IP: "192.0.2.1", Port: 443}})
	quic.block = true
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	input := h3RunInput()
	var checkpoint Checkpoint
	input.Plan.Save = func(c Checkpoint) error { checkpoint = c; return nil }
	summary := engine.Run(ctx, "cancel", settings, input, nil)
	if summary.State != "cancelled" {
		t.Fatal(summary.State)
	}
	if _, ok := checkpoint.Stage(StageQUIC); ok || len(checkpoint.QUICResults) > 0 {
		t.Fatal("partial QUIC checkpoint saved")
	}
	if _, ok := checkpoint.Stage("source"); !ok {
		t.Fatal("completed source snapshot lost")
	}
}
func TestH3RetainedResultsReprobeAndDoNotConsumeFreshTarget(t *testing.T) {
	engine, settings, resolver, quic, _, _ := h3Fixture([]model.Candidate{{AddressType: model.AddressIPv4, IP: "192.0.2.3", Port: 443}})
	settings.HTTPCandidateCount = 1
	settings.BandwidthCandidates = 1
	settings.FinalResultCount = 5
	input := h3RunInput()
	input.Retained = []model.ProbeResult{{Mode: model.ModeH3, Candidate: model.Candidate{AddressType: model.AddressDomain, Host: "pool.test", Port: 443}}}
	result := engine.Run(t.Context(), "retained", settings, input, nil)
	if len(result.Results) != 2 || resolver.calls["pool.test"] != 1 || len(quic.calls) != 3 {
		t.Fatalf("retained results not retested: %+v", result)
	}
}

func TestH3RankingReappliesCurrentFilters(t *testing.T) {
	engine, settings, _, _, _, _ := h3Fixture([]model.Candidate{{AddressType: model.AddressIPv4, IP: "192.0.2.1", Port: 8443, Country: "JP"}, {AddressType: model.AddressIPv4, IP: "192.0.2.2", Port: 443, Country: "US"}})
	input := h3RunInput()
	var checkpoint Checkpoint
	input.Plan.Save = func(c Checkpoint) error { checkpoint = c; return nil }
	engine.Run(t.Context(), "original", settings, input, nil)
	input.Plan = RunPlan{StartStage: StageRanking, Base: &checkpoint}
	settings.AllowedPorts = []int{443}
	result := engine.Run(t.Context(), "ports", settings, input, nil)
	if len(result.Results) != 1 || result.Results[0].Candidate.Port != 443 {
		t.Fatal("old disallowed port reused")
	}
	settings.AllowedPorts = nil
	settings.BlockedCountries = []string{"JP"}
	result = engine.Run(t.Context(), "countries", settings, input, nil)
	if len(result.Results) != 1 || result.Results[0].Candidate.Country != "US" {
		t.Fatal("old disallowed country reused")
	}
}
func TestH3CancellationAtRankingCannotCompleteOrPublish(t *testing.T) {
	engine, settings, _, _, _, _ := h3Fixture([]model.Candidate{{AddressType: model.AddressIPv4, IP: "192.0.2.1", Port: 443}})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	input := h3RunInput()
	var checkpoint Checkpoint
	input.Plan.Save = func(c Checkpoint) error { checkpoint = c; return nil }
	result := engine.Run(ctx, "ranking-cancel", settings, input, func(p model.RunProgress) {
		for _, s := range p.Stages {
			if s.Name == StageRanking && s.State == "running" {
				cancel()
			}
		}
	})
	if result.State != "cancelled" || len(result.Results) != 0 {
		t.Fatal("cancelled ranking completed")
	}
	if _, ok := checkpoint.Stage(StageRanking); ok {
		t.Fatal("cancelled ranking saved")
	}
}

func TestH3CancelledResumeKeepsCompleteStagesAvailable(t *testing.T) {
	for _, stage := range []string{StageQUIC, StageHTTP3, StageBandwidth, StageRanking} {
		t.Run(stage, func(t *testing.T) {
			engine, settings, _, _, _, _ := h3Fixture([]model.Candidate{{AddressType: model.AddressIPv4, IP: "192.0.2.1", Port: 443}})
			input := h3RunInput()
			var saved Checkpoint
			input.Plan.Save = func(c Checkpoint) error { saved = c; return nil }
			engine.Run(t.Context(), "original", settings, input, nil)
			base := saved
			input.Plan.StartStage, input.Plan.Base = stage, &base
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			summary := engine.Run(ctx, "resumed", settings, input, func(p model.RunProgress) {
				for _, current := range p.Stages {
					if current.Name == stage && current.State == "running" {
						cancel()
					}
				}
			})
			if summary.State != "cancelled" || saved.RunID != summary.RunID {
				t.Fatalf("cancelled run lost its checkpoint: state=%s saved=%s current=%s", summary.State, saved.RunID, summary.RunID)
			}
			if _, complete := saved.Stage(stage); complete {
				t.Fatal("unfinished stage persisted as complete")
			}
			info := BuildResumeInfo(saved, summary.RunID, settings, input.Sources, NetworkSnapshot{}, time.Now())
			for _, option := range info.Options {
				if option.Stage == stage && option.Available {
					return
				}
			}
			t.Fatalf("complete prerequisite stages cannot be reused: %+v", info)
		})
	}
}
