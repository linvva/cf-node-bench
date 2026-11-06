package run

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
)

func TestProtocolChangeReusesOnlyCandidates(t *testing.T) {
	for _, requireH3 := range []bool{false, true} {
		settings := config.DefaultSettings()
		settings.RequireH3 = requireH3
		t.Run(string(settings.ProbeMode()), func(t *testing.T) {
			engine, original, _, quic, http3, band := h3Fixture([]model.Candidate{
				{AddressType: model.AddressIPv4, IP: "192.0.2.1", Port: 443},
				{AddressType: model.AddressDomain, Host: "pool.test", Port: 443},
			})
			original.RequireH3 = !requireH3
			engine.Dependencies.TCP = quic
			engine.Dependencies.HTTPS = http3
			engine.Dependencies.Bandwidth = band
			input := h3RunInput()
			var checkpoint Checkpoint
			input.Plan.Save = func(c Checkpoint) error { checkpoint = c; return nil }
			engine.Run(t.Context(), "original", original, input, nil)
			settings.VerificationHost = original.VerificationHost
			info := BuildResumeInfo(checkpoint, "original", settings, input.Sources, NetworkSnapshot{}, time.Now())
			connectionStage := StageTCP
			if requireH3 {
				connectionStage = StageQUIC
			}
			for _, option := range info.Options {
				if option.Available != (option.Stage == connectionStage) {
					t.Fatalf("cross-protocol resume option: %+v", option)
				}
			}
			if info.Options[0].Input != 1 && !requireH3 {
				t.Fatal("TCP candidate count included domains")
			}
			before := quic.calls["192.0.2.1:443"]
			fetchCalls := 0
			engine.Dependencies.Fetcher = countingFetcher{calls: &fetchCalls}
			base := checkpoint
			input.Plan.Base, input.Plan.StartStage = &base, StageRanking
			result := engine.Run(t.Context(), "switched", settings, input, nil)
			if result.State != "completed" || result.Mode != settings.ProbeMode() || result.StartStage != connectionStage || fetchCalls != 0 || quic.calls["192.0.2.1:443"] != before+1 {
				t.Fatalf("protocol switch reused old measurements: %+v, fetch=%d", result, fetchCalls)
			}
			if !slices.Equal(result.ReusedStages, []string{StageSource}) {
				t.Fatalf("cross-protocol stages reused: %v", result.ReusedStages)
			}
			if !requireH3 {
				if len(result.Results) != 1 || result.Results[0].Candidate.AddressType == model.AddressDomain {
					t.Fatalf("TCP probed domain input: %+v", result.Results)
				}
				if _, ok := checkpoint.Stage(StageQUIC); ok || len(checkpoint.QUICResults) != 0 {
					t.Fatal("old QUIC measurements survived new TCP checkpoint")
				}
			} else if len(checkpoint.TCPResults) != 0 {
				t.Fatal("old TCP measurements survived new QUIC checkpoint")
			}
		})
	}
}

func TestVerificationHostChangeCannotBypassFreshProbes(t *testing.T) {
	engine, settings, _, quic, _, _ := h3Fixture([]model.Candidate{{AddressType: model.AddressIPv4, IP: "192.0.2.1", Port: 443}})
	input := h3RunInput()
	var checkpoint Checkpoint
	input.Plan.Save = func(c Checkpoint) error { checkpoint = c; return nil }
	engine.Run(t.Context(), "original", settings, input, nil)
	settings.VerificationHost = "other.test"
	input.Plan = RunPlan{Base: &checkpoint, StartStage: StageRanking}
	result := engine.Run(t.Context(), "new-host", settings, input, nil)
	if result.StartStage != StageQUIC || result.WorkerHost != "other.test" || quic.calls["192.0.2.1:443"] != 2 {
		t.Fatalf("verification-host change bypassed probes: %+v", result)
	}
}

func TestRunUsesFixedSharedSettingsSnapshot(t *testing.T) {
	for _, requireH3 := range []bool{false, true} {
		engine, settings, _, quic, http3, band := h3Fixture([]model.Candidate{{AddressType: model.AddressIPv4, IP: "192.0.2.1", Port: 443, Country: "US"}})
		settings.RequireH3 = requireH3
		settings.VerificationHost = ""
		settings.AllowedPorts = []int{443}
		settings.AllowedCountries = []string{"US"}
		settings.BlockedCountries = []string{"CN"}
		settings.ConnectionProbeCount, settings.HTTPProbeCount = 2, 4
		engine.Dependencies.TCP, engine.Dependencies.HTTPS, engine.Dependencies.Bandwidth = quic, http3, band
		t.Run(string(settings.ProbeMode()), func(t *testing.T) {
			originalMode, originalWeights := settings.ProbeMode(), settings.ScoreWeights
			input := h3RunInput()
			var checkpoint Checkpoint
			input.Plan.Save = func(c Checkpoint) error { checkpoint = c; return nil }
			var mutate sync.Once
			result := engine.Run(t.Context(), "snapshot", settings, input, func(model.RunProgress) {
				mutate.Do(func() {
					settings.RequireH3 = !requireH3
					settings.VerificationHost = "changed.test"
					settings.AllowedPorts[0] = 8443
					settings.AllowedCountries[0] = "JP"
					settings.BlockedCountries[0] = "US"
					settings.ConnectionProbeCount, settings.HTTPProbeCount = 9, 9
					settings.ScoreWeights = model.ScoreWeights{Bandwidth: 100}
				})
			})
			if result.Mode != originalMode || len(result.Results) != 1 || result.ScoreWeights != originalWeights {
				t.Fatalf("run settings changed mid-run: %+v", result)
			}
			if checkpoint.Settings.AllowedPorts[0] != 443 || checkpoint.Settings.AllowedCountries[0] != "US" || checkpoint.Settings.BlockedCountries[0] != "CN" || checkpoint.Settings.ConnectionProbeCount != 2 || checkpoint.Settings.HTTPProbeCount != 4 {
				t.Fatalf("checkpoint settings changed mid-run: %+v", checkpoint.Settings)
			}
			measured := result.Results[0]
			connection, request := measured.TCP, measured.HTTPS
			if requireH3 {
				connection, request = measured.QUIC, measured.HTTP3
				if result.WorkerHost != "h3.speed.cloudflare.com" {
					t.Fatalf("blank verification host did not use public default: %s", result.WorkerHost)
				}
			}
			if connection.Attempts != 2 || request.Attempts != 4 {
				t.Fatalf("shared sample counts not respected: connection=%d request=%d", connection.Attempts, request.Attempts)
			}
		})
	}
}

func TestH3ResumeWarningsUseH3Settings(t *testing.T) {
	engine, settings, _, _, _, _ := h3Fixture([]model.Candidate{{AddressType: model.AddressIPv4, IP: "192.0.2.1", Port: 443}})
	input := h3RunInput()
	var checkpoint Checkpoint
	input.Plan.Save = func(c Checkpoint) error { checkpoint = c; return nil }
	engine.Run(t.Context(), "original", settings, input, nil)
	changed := settings
	changed.ConnectTimeoutMS++
	changed.RequestTimeoutMS++
	changed.BandwidthTestURL = "https://other.test/download"
	info := BuildResumeInfo(checkpoint, "original", changed, input.Sources, NetworkSnapshot{}, time.Now())
	for _, option := range info.Options[1:] {
		if len(option.Warnings) != 1 {
			t.Fatalf("ordinary settings produced H3 warnings: %+v", option)
		}
	}
	changed.H3ConnectTimeoutMS++
	changed.H3RequestTimeoutMS++
	changed.H3BandwidthTestURL = "https://other.test/h3-download"
	info = BuildResumeInfo(checkpoint, "original", changed, input.Sources, NetworkSnapshot{}, time.Now())
	if option := info.Options[3]; !option.Available || len(option.Warnings) != 4 {
		t.Fatalf("H3 settings warnings missing: %+v", option)
	}
}

func TestRunProgressReportsActualProtocol(t *testing.T) {
	for _, requireH3 := range []bool{false, true} {
		engine, settings, _, quic, http3, bandwidth := h3Fixture([]model.Candidate{{IP: "192.0.2.1", Port: 443}})
		settings.RequireH3 = requireH3
		engine.Dependencies.TCP, engine.Dependencies.HTTPS, engine.Dependencies.Bandwidth = quic, http3, bandwidth
		var events []model.RunProgress
		engine.Run(t.Context(), "progress-protocol", settings, h3RunInput(), func(progress model.RunProgress) { events = append(events, progress) })
		if len(events) == 0 {
			t.Fatal("no progress")
		}
		for _, event := range events {
			if event.Mode != settings.ProbeMode() {
				t.Fatalf("progress for %s omitted actual protocol: %+v", settings.ProbeMode(), event)
			}
		}
	}
}
