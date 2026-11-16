package main

import (
	"testing"
	"time"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
	runengine "github.com/linvva/cf-node-bench/internal/run"
	"github.com/linvva/cf-node-bench/internal/source"
)

func TestAppPinsOnlyNodesFromSavedResultsAndUsesHistoryMode(t *testing.T) {
	app := testApp(t)
	candidate := model.Candidate{AddressType: model.AddressIPv4, IP: "192.0.2.1", Port: 443}
	if err := app.store.AddHistory(model.RunSummary{RunID: "tcp", Mode: model.ModeTCPHTTPS, State: "completed", Results: []model.ProbeResult{{Candidate: candidate}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SetNodePinned("missing", candidate.Key(), true); err == nil {
		t.Fatal("missing history accepted")
	}
	if _, err := app.SetNodePinned("tcp", "192.0.2.99:443", true); err == nil {
		t.Fatal("node absent from history accepted")
	}
	settings := app.store.Settings()
	settings.RequireH3 = true
	if err := app.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	nodes, err := app.SetNodePinned("tcp", candidate.Key(), true)
	if err != nil || len(nodes) != 1 || nodes[0].Mode != model.ModeTCPHTTPS {
		t.Fatalf("history mode not preserved: %+v, %v", nodes, err)
	}
	if len(app.pinnedCandidates()) != 0 || len(app.Bootstrap().PinnedNodes) != 1 {
		t.Fatal("TCP pin leaked into H3 input or was absent from bootstrap")
	}
	settings.RequireH3 = false
	if err := app.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	if candidates := app.pinnedCandidates(); len(candidates) != 1 || candidates[0] != candidate {
		t.Fatal("pin missing from matching mode")
	}
}

func TestAppCannotModifyPinsDuringRun(t *testing.T) {
	app := testApp(t)
	done := make(chan struct{})
	_, err := app.manager.Start(t.Context(), runengine.Engine{Dependencies: runengine.Dependencies{Fetcher: waitingFetcher{}}}, config.DefaultSettings(), runengine.RunInput{Sources: []source.HTTPSource{{Enabled: true}}}, nil, func(model.RunSummary) { close(done) })
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.SetNodePinned("any", "192.0.2.1:443", true)
	app.CancelRun()
	if err == nil || err.Error() != "测速运行中不能修改固化节点" {
		t.Errorf("expected run guard, got %v", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("run did not stop")
	}
}
