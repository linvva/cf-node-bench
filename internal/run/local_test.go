package run

import (
	"strings"
	"testing"
	"time"

	"github.com/linvva/cf-node-bench/internal/model"
	"github.com/linvva/cf-node-bench/internal/source"
)

func TestLocalFilesExcludeOtherCandidatesAndKeepResumeSnapshot(t *testing.T) {
	for _, h3 := range []bool{false, true} {
		name := "tcp_https"
		if h3 {
			name = "h3"
		}
		t.Run(name, func(t *testing.T) {
			engine, settings, _, _, _, _ := h3Fixture(nil)
			settings.RequireH3 = h3
			// An accidental HTTP fetch fails the test instead of reaching a network.
			engine.Dependencies.Fetcher = nil
			engine.Dependencies.TCP, engine.Dependencies.HTTPS, engine.Dependencies.Bandwidth = fakeTCP{}, fakeHTTPS{}, fakeBandwidth{}
			candidate := model.Candidate{AddressType: model.AddressIPv4, IP: "192.0.2.1", Port: 8443, SourceID: "local:nodes.csv"}
			var saved Checkpoint
			input := RunInput{
				LocalFiles: &source.ParseResult{Candidates: []model.Candidate{candidate}},
				Sources:    []source.HTTPSource{{ID: "http", URL: "https://unused.test", Enabled: true}},
				Retained:   []model.ProbeResult{{Candidate: model.Candidate{IP: "192.0.2.99", Port: 443}, TCP: model.ProbeStats{SuccessRate: 1}}},
				Plan:       RunPlan{Save: func(c Checkpoint) error { c.UpdatedAt = time.Now(); saved = c; return nil }},
			}
			assertLocalProgress := func(p model.RunProgress) {
				if !p.LocalFiles {
					t.Error("local progress lost its origin")
				}
			}
			result := engine.Run(t.Context(), "local", settings, input, assertLocalProgress)
			if !result.LocalFiles || result.Mode != settings.ProbeMode() || len(result.Results) != 1 || result.Results[0].Candidate.Key() != candidate.Key() {
				t.Fatalf("local run mixed candidates or lost mode: %+v", result)
			}
			if !saved.LocalFiles || len(saved.Sources) != 0 || len(saved.Candidates) != 1 {
				t.Fatal("local checkpoint retained configured sources")
			}
			for _, option := range BuildResumeInfo(saved, saved.RunID, settings, input.Sources, NetworkSnapshot{}, time.Now()).Options {
				for _, warning := range option.Warnings {
					if strings.Contains(warning, "数据源已变化") {
						t.Fatal("HTTP sources affected a local snapshot")
					}
				}
			}
			for _, stage := range []string{map[bool]string{false: StageTCP, true: StageQUIC}[h3], StageRanking} {
				base := saved
				resumed := engine.Run(t.Context(), "local-"+stage, settings, RunInput{Sources: input.Sources, Plan: RunPlan{StartStage: stage, Base: &base, Save: input.Plan.Save}}, assertLocalProgress)
				if !resumed.LocalFiles || len(resumed.Results) != 1 || resumed.Results[0].Candidate.Key() != candidate.Key() || !saved.LocalFiles {
					t.Fatalf("%s did not retain local snapshot: %+v", stage, resumed)
				}
			}
		})
	}
}
