package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/linvva/cf-node-bench/internal/model"
	"github.com/linvva/cf-node-bench/internal/publish"
)

func configureH3Gist(t *testing.T, app *App) {
	t.Helper()
	view := app.store.PublishSettingsView()
	view.Gist.Enabled, view.Gist.GistID, view.Gist.Filename = true, "normal-gist", "ip.txt"
	view.Gist.H3 = publish.GistH3Target{GistID: "h3-gist", Filename: "h3.txt"}
	if _, err := app.SavePublishSettings(publish.SaveRequest{Settings: view, GistToken: "shared-gist-token"}); err != nil {
		t.Fatal(err)
	}
}

func TestAppConnectionTestSelectsH3GistWithoutChangingSharedSettings(t *testing.T) {
	app := testApp(t)
	configureH3Gist(t, app)
	paths := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer shared-gist-token" {
			t.Error("connection test wrote data or lost shared credential")
		}
		paths <- r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "accessible-gist", "files": map[string]any{}})
	}))
	defer server.Close()
	app.publish.GitHubBaseURL = server.URL
	for _, mode := range []model.Mode{model.ModeH3, model.ModeTCPHTTPS} {
		settings := app.store.Settings()
		settings.RequireH3 = mode != model.ModeH3
		if err := app.SaveSettings(settings); err != nil {
			t.Fatal(err)
		}
		request := publish.SaveRequest{Settings: app.store.PublishSettingsView(), TestMode: mode}
		if err := app.TestPublishTarget("gist", request); err != nil {
			t.Fatal(err)
		}
	}
	if <-paths != "/gists/h3-gist" || <-paths != "/gists/normal-gist" || app.store.PublishSettings().Gist.GistID != "normal-gist" {
		t.Fatal("test target followed measurement switch or altered settings")
	}
	if err := app.TestPublishTarget("gist", publish.SaveRequest{TestMode: "unknown"}); err == nil {
		t.Fatal("unknown test protocol accepted")
	}
}

func TestAppHistoricalH3PublicationUsesItsOwnTargetWhenH3IsOff(t *testing.T) {
	app := testApp(t)
	configureH3Gist(t, app)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gists/h3-gist" || r.Header.Get("Authorization") != "Bearer shared-gist-token" {
			t.Error("history publication selected current switch instead of run protocol")
		}
		files := map[string]any{}
		if r.Method == http.MethodPatch {
			var payload struct {
				Files map[string]json.RawMessage `json:"files"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || len(payload.Files) != 1 || payload.Files["h3.txt"] == nil {
				t.Error("history publication changed the ordinary file")
			}
			files["h3.txt"] = map[string]string{"filename": "h3.txt", "raw_url": "https://gist.githubusercontent.com/test/h3-gist/raw/revision/h3.txt"}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "h3-gist", "files": files})
	}))
	defer server.Close()
	app.publish.GitHubBaseURL = server.URL
	summary := model.RunSummary{RunID: "historical-h3", Mode: model.ModeH3, State: "completed", Results: []model.ProbeResult{{Mode: model.ModeH3, Candidate: model.Candidate{IP: "192.0.2.1", Port: 443}}}}
	if err := app.store.AddHistory(summary); err != nil {
		t.Fatal(err)
	}
	if app.store.Settings().RequireH3 {
		t.Fatal("test requires ordinary measurement setting")
	}
	if err := app.PublishRun(summary.RunID, "gist"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		saved, _ := app.store.HistoryByID(summary.RunID)
		for _, result := range saved.Publications {
			if result.Target == "gist" && result.State == "succeeded" {
				if result.URL != "https://gist.githubusercontent.com/test/h3-gist/raw/revision/h3.txt" {
					t.Fatal("history retained the wrong raw URL")
				}
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("history publication did not succeed")
}

func TestAppMissingH3TargetDoesNotRequestOrdinaryDestination(t *testing.T) {
	app := testApp(t)
	view := app.store.PublishSettingsView()
	view.Cloudflare.Enabled, view.Cloudflare.ZoneID, view.Cloudflare.RecordName = true, "zone", "normal.example.com"
	view.GitHub.Enabled, view.GitHub.Owner, view.GitHub.Repository, view.GitHub.Path = true, "owner", "repo", "ip.txt"
	view.Gist.Enabled, view.Gist.GistID, view.Gist.Filename = true, "normal-gist", "ip.txt"
	if _, err := app.SavePublishSettings(publish.SaveRequest{Settings: view, CloudflareToken: "shared-cf", GitHubToken: "shared-gh", GistToken: "shared-gist"}); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	app.publish.CloudflareBaseURL, app.publish.GitHubBaseURL = server.URL, server.URL
	summary := model.RunSummary{RunID: "historical-h3-no-target", Mode: model.ModeH3, State: "completed", Results: []model.ProbeResult{{Mode: model.ModeH3, Candidate: model.Candidate{IP: "192.0.2.1", Port: 443}}}}
	if err := app.store.AddHistory(summary); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"cloudflare", "github", "gist"} {
		request := publish.SaveRequest{Settings: app.store.PublishSettingsView(), TestMode: model.ModeH3}
		if err := app.TestPublishTarget(target, request); err == nil || !strings.Contains(err.Error(), "未配置") {
			t.Fatalf("connection test accepted missing %s destination: %v", target, err)
		}
		if err := app.PublishRun(summary.RunID, target); err != publish.ErrNoTargets {
			t.Fatalf("historical publication accepted missing %s destination: %v", target, err)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("missing H3 targets requested ordinary destinations")
	}
	stored, _ := app.store.HistoryByID(summary.RunID)
	if len(stored.Publications) != 0 {
		t.Fatal("rejected publication changed history")
	}
}
