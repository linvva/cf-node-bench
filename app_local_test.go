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
	for _, h3 := range []bool{false, true} {
		app := testApp(t)
		settings := app.store.Settings()
		settings.RequireH3 = h3
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
		if !found || !result.LocalFiles || result.Mode != settings.ProbeMode() || result.Failures[model.FailurePortFiltered] != 1 {
			t.Fatalf("local run did not use selected mode/filter: %+v", result)
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
