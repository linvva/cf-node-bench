package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
	"github.com/linvva/cf-node-bench/internal/publish"
	runengine "github.com/linvva/cf-node-bench/internal/run"
	"github.com/linvva/cf-node-bench/internal/source"
	"github.com/linvva/cf-node-bench/internal/storage"
)

func testApp(t *testing.T) *App {
	t.Helper()
	store, err := storage.Open(t.TempDir() + "/data.json")
	if err != nil {
		t.Fatal(err)
	}
	return NewApp(store)
}
func TestAppH3RequirementSharesSettingsAndPublishing(t *testing.T) {
	app := testApp(t)
	settings := app.store.Settings()
	settings.ConnectionConcurrency = 32
	publication := app.store.PublishSettings()
	settings.RequireH3 = true
	if err := app.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	bootstrap := app.Bootstrap()
	if !bootstrap.Settings.RequireH3 || bootstrap.Settings.EffectiveVerificationHost() != "h3.speed.cloudflare.com" {
		t.Fatal("H3 public default unavailable")
	}
	if app.store.PublishSettings() != publication {
		t.Fatal("H3 switch changed publication configuration")
	}
	if _, err := app.StartRun(runengine.StartRequest{StartStage: runengine.StageTCP}); err == nil {
		t.Fatal("TCP stage accepted with H3 requirement")
	}
	settings.RequireH3 = false
	if err := app.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	if app.store.Settings().ConnectionConcurrency != 32 {
		t.Fatal("shared settings lost")
	}
	if _, err := app.StartRun(runengine.StartRequest{StartStage: runengine.StageHTTP3}); err == nil {
		t.Fatal("H3 stage accepted without H3 requirement")
	}
}

type waitingFetcher struct{}

func (waitingFetcher) Fetch(ctx context.Context, _ source.HTTPSource) (source.ParseResult, error) {
	<-ctx.Done()
	return source.ParseResult{}, ctx.Err()
}
func TestAppCannotChangeH3RequirementDuringRun(t *testing.T) {
	app := testApp(t)
	done := make(chan struct{})
	_, err := app.manager.Start(t.Context(), runengine.Engine{Dependencies: runengine.Dependencies{Fetcher: waitingFetcher{}}}, config.DefaultSettings(), runengine.RunInput{Sources: []source.HTTPSource{{Enabled: true}}}, nil, func(model.RunSummary) { close(done) })
	if err != nil {
		t.Fatal(err)
	}
	settings := app.store.Settings()
	settings.RequireH3 = true
	if err = app.SaveSettings(settings); err == nil {
		t.Error("mode switch during run accepted")
	}
	app.CancelRun()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled run did not stop")
	}
	if err = app.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
}

func TestAppPublishActionsShareCredentialAcrossModes(t *testing.T) {
	app := testApp(t)
	publication := app.store.PublishSettings()
	publication.GitHub.Owner, publication.GitHub.Repository = "owner", "repo"
	publication.GitHub.Path, publication.GitHub.H3.Path = "ip.txt", "h3.txt"
	if _, err := app.SavePublishSettings(publish.SaveRequest{Settings: publication.View(), GitHubToken: "shared-test-token"}); err != nil {
		t.Fatal(err)
	}
	settings := app.store.Settings()
	settings.RequireH3 = true
	if err := app.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	authorization := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization <- r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	app.publish.GitHubBaseURL = server.URL
	for _, mode := range []model.Mode{model.ModeTCPHTTPS, model.ModeH3} {
		request := publish.SaveRequest{Settings: app.store.PublishSettings().View(), TestMode: mode}
		request.Settings.Gist.GistID = "unfinished-other-target"
		if err := app.TestPublishTarget("github", request); err != nil {
			t.Fatal(err)
		}
		if got := <-authorization; got != "Bearer shared-test-token" {
			t.Fatal("connection test lost shared credential")
		}
	}
	if _, err := app.ClearPublishCredential("github"); err != nil {
		t.Fatal(err)
	}
	settings.RequireH3 = false
	if err := app.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	if app.store.PublishSettings().GitHub.Token != "" {
		t.Fatal("cleared credential reappeared")
	}
}

func TestAppMigratedActiveCheckpointRemainsResumable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")
	old := map[string]any{"activeMode": "h3", "history": []model.RunSummary{{RunID: "later-tcp", Mode: model.ModeTCPHTTPS, State: "completed"}, {RunID: "selected-h3", Mode: model.ModeH3, State: "completed"}}}
	body, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(dir, "checkpoint-h3.json.gz"))
	if err != nil {
		t.Fatal(err)
	}
	writer := gzip.NewWriter(file)
	checkpoint := runengine.Checkpoint{Version: runengine.CheckpointVersion, RunID: "selected-h3", Settings: config.DefaultH3Settings(), Candidates: []model.Candidate{{AddressType: model.AddressIPv4, IP: "192.0.2.1", Port: 443}}, Stages: []model.StageProgress{{Name: "source", State: "completed"}}, CompletedAt: map[string]time.Time{"source": time.Now()}}
	if err := json.NewEncoder(writer).Encode(checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(store)
	bootstrap := app.Bootstrap()
	if bootstrap.CurrentResultRunID != "selected-h3" || bootstrap.History[0].RunID != "later-tcp" {
		t.Fatal("current result or chronological history changed")
	}
	info := bootstrap.Resume
	if len(info.Options) == 0 {
		t.Fatal("selected H3 checkpoint hidden because a later TCP history is first")
	}
}
