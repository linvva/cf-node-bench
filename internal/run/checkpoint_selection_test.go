package run

import (
	"strings"
	"testing"
	"time"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
)

func TestHTTPSelectionCheckpointRequiresFullCoverage(t *testing.T) {
	for _, h3 := range []bool{false, true} {
		for _, change := range []string{"legacy", "larger-pool", "lower-connection-threshold", "allow-country", "allow-port", "already-measured", "still-filtered"} {
			mode := "https"
			if h3 {
				mode = "h3"
			}
			t.Run(mode+"/"+change, func(t *testing.T) {
				settings := config.DefaultSettings()
				settings.RequireH3 = h3
				settings.ConnectionMinSuccessRate = 1
				settings.AllowedCountries = []string{"US"}
				settings.AllowedPorts = []int{443}
				checkpoint := reusableCheckpoint(settings)
				checkpoint.HTTPSelectionComplete = true
				checkpoint.QUICResults = append([]model.ProbeResult(nil), checkpoint.TCPResults...)
				checkpoint.HTTP3Results = append([]model.ProbeResult(nil), checkpoint.HTTPSResults...)
				for i := range checkpoint.QUICResults {
					checkpoint.QUICResults[i].QUIC = checkpoint.QUICResults[i].TCP
					checkpoint.QUICResults[i].Endpoints = []model.EndpointResult{{IP: checkpoint.QUICResults[i].Candidate.IP}}
				}
				checkpoint.Stages = append(checkpoint.Stages, model.StageProgress{Name: StageQUIC, State: "completed"}, model.StageProgress{Name: StageHTTP3, State: "completed"})
				extra := model.ProbeResult{Candidate: model.Candidate{IP: "192.0.2.123", Port: 443, Country: "US"}, TCP: model.ProbeStats{SuccessRate: 1}, QUIC: model.ProbeStats{SuccessRate: 1}, Endpoints: []model.EndpointResult{{IP: "192.0.2.123"}}}
				current := settings
				wantRestart := change == "legacy" || change == "lower-connection-threshold" || change == "allow-country" || change == "allow-port"
				switch change {
				case "legacy":
					checkpoint.HTTPSelectionComplete = false
				case "larger-pool":
					current.HTTPCandidateCount++
				case "lower-connection-threshold":
					extra.TCP.SuccessRate, extra.QUIC.SuccessRate = 2.0/3, 2.0/3
					current.ConnectionMinSuccessRate = 2.0 / 3
				case "allow-country", "still-filtered":
					extra.Candidate.Country = "JP"
					if change == "allow-country" {
						current.AllowedCountries = []string{"US", "JP"}
					}
				case "allow-port":
					extra.Candidate.Port = 8443
					current.AllowedPorts = []int{443, 8443}
				case "already-measured":
					checkpoint.HTTPSResults = append(checkpoint.HTTPSResults, extra)
					checkpoint.HTTP3Results = append(checkpoint.HTTP3Results, extra)
				}
				if change != "legacy" && change != "larger-pool" {
					checkpoint.TCPResults = append(checkpoint.TCPResults, extra)
					checkpoint.QUICResults = append(checkpoint.QUICResults, extra)
				}
				info := BuildResumeInfo(checkpoint, checkpoint.RunID, current, nil, NetworkSnapshot{}, time.Now())
				for _, option := range info.Options {
					late := option.Stage == StageBandwidth || option.Stage == StageRanking
					if option.Available != (!late || !wantRestart) {
						t.Fatalf("unexpected availability for %s: %+v", option.Stage, option)
					}
					if late && wantRestart && !strings.Contains(strings.Join(option.Warnings, " "), "全部候选") {
						t.Fatalf("missing full-comparison warning: %+v", option)
					}
				}
				for _, start := range []string{StageBandwidth, StageRanking} {
					want := start
					if wantRestart {
						want = StageHTTPS
						if h3 {
							want = StageHTTP3
						}
					}
					if got := runStartStage(start, current, &checkpoint); got != want {
						t.Fatalf("direct engine start = %s, want %s", got, want)
					}
				}
			})
		}
	}
}

func TestHTTPSelectionMarkerOnlySurvivesHTTPReuse(t *testing.T) {
	settings := config.DefaultSettings()
	base := reusableCheckpoint(settings)
	base.HTTPSelectionComplete = true
	for _, start := range []string{StageSource, StageTCP, StageHTTPS, StageBandwidth, StageRanking} {
		checkpoint := checkpointForRun("resumed", base.RunID, settings, nil, NetworkSnapshot{}, start, &base)
		want := start == StageBandwidth || start == StageRanking
		if checkpoint.HTTPSelectionComplete != want {
			t.Fatalf("HTTP comparison completeness after %s resume = %v, want %v", start, checkpoint.HTTPSelectionComplete, want)
		}
	}
}
