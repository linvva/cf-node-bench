package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/linvva/cf-node-bench/internal/model"
)

func TestLocalFileStartValidatesBeforeRunAndDoesNotSavePaths(t *testing.T) {
	for _, test := range []struct{ h3, retain bool }{{false, false}, {false, true}, {true, false}, {true, true}} {
		app := testApp(t)
		settings := app.store.Settings()
		settings.RequireH3 = test.h3
		settings.RetainPreviousResults = test.retain
		settings.AllowedPorts = []int{443}
		if err := app.SaveSettings(settings); err != nil {
			t.Fatal(err)
		}
		before := app.store.Sources()
		path := filepath.Join(t.TempDir(), "nodes.csv")
		if _, err := app.StartLocalFiles([]string{path}); err == nil || app.manager.Current() != "" || len(app.store.History()) != 0 {
			t.Fatal("missing file started a run")
		}
		// Port filtering completes a real App run without any external probe.
		previous := model.ProbeResult{Candidate: model.Candidate{AddressType: model.AddressIPv4, IP: "192.0.2.2", Port: 80}, TCP: model.ProbeStats{SuccessRate: 1}}
		if err := app.store.AddHistory(model.RunSummary{
			RunID: "previous", Mode: settings.ProbeMode(), WorkerHost: settings.EffectiveVerificationHost(),
			State: "completed", LocalFiles: true, Results: []model.ProbeResult{previous},
		}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("IP地址,端口号,IP位置\n192.0.2.1,80,HK\n"), 0600); err != nil {
			t.Fatal(err)
		}
		runID, err := app.StartLocalFiles([]string{path})
		if err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(2 * time.Second)
		var result model.RunSummary
		found := false
		for time.Now().Before(deadline) {
			if result, found = app.store.HistoryByID(runID); found {
				break
			}
			time.Sleep(time.Millisecond)
		}
		wantPortFailures := 1
		if test.retain {
			wantPortFailures++
		}
		if !found || !result.LocalFiles || result.Mode != settings.ProbeMode() || result.Failures[model.FailurePortFiltered] != wantPortFailures {
			t.Fatalf("retain=%v: expected %d port filter failures, got %+v", test.retain, wantPortFailures, result)
		}
		checkpoint, ok, err := app.store.Checkpoint()
		if err != nil || !ok || !checkpoint.LocalFiles || len(checkpoint.Sources) != 0 || checkpoint.Candidates[0].SourceID != "local:nodes.csv" {
			t.Fatalf("unexpected local checkpoint: %+v, %v", checkpoint, err)
		}
		if !reflect.DeepEqual(before, app.store.Sources()) {
			t.Fatal("local files changed persisted HTTP sources")
		}
	}
}
