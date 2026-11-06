package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
	"github.com/linvva/cf-node-bench/internal/publish"
)

func TestH3PublishTargetsPersistWithSharedCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	view := store.PublishSettingsView()
	view.Cloudflare.Enabled, view.Cloudflare.ZoneID, view.Cloudflare.RecordName = true, "shared-zone", "normal.example.com"
	view.Cloudflare.H3 = publish.CloudflareH3Target{RecordName: "H3.Example.COM.", RecordType: "TXT"}
	view.GitHub.Enabled, view.GitHub.Owner, view.GitHub.Repository, view.GitHub.Path = true, "owner", "repo", "ip.txt"
	view.GitHub.H3 = publish.GitHubH3Target{Path: "nodes/h3.txt"}
	view.Gist.Enabled, view.Gist.GistID, view.Gist.Filename = true, "normal-gist", "ip.txt"
	view.Gist.H3 = publish.GistH3Target{GistID: "h3-gist", Filename: "h3.txt"}
	if _, err := store.SavePublishSettings(publish.SaveRequest{Settings: view, CloudflareToken: "shared-cf-secret", GitHubToken: "shared-gh-secret", GistToken: "shared-gist-secret", TestMode: model.ModeH3}); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	shared := store.PublishSettings()
	h3 := shared.ForMode(model.ModeH3)
	if h3.Cloudflare.RecordName != "h3.example.com" || h3.Cloudflare.RecordType != "TXT" || h3.GitHub.Path != "nodes/h3.txt" || h3.Gist.GistID != "h3-gist" || h3.Gist.Filename != "h3.txt" {
		t.Fatal("H3 targets did not survive reopening")
	}
	if h3.Cloudflare.APIToken != shared.Cloudflare.APIToken || h3.Cloudflare.ZoneID != shared.Cloudflare.ZoneID || h3.GitHub.Token != shared.GitHub.Token || h3.GitHub.Repository != shared.GitHub.Repository || h3.Gist.Token != shared.Gist.Token {
		t.Fatal("H3 did not share credentials and accounts")
	}
	settings := store.Settings()
	settings.RequireH3 = true
	if err := store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	if store.PublishSettings() != shared {
		t.Fatal("measurement switch changed publish configuration")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "testMode") || strings.Count(string(content), "shared-gist-secret") != 1 {
		t.Fatal("test selection persisted or credentials duplicated")
	}
	encodedView, _ := json.Marshal(store.PublishSettingsView())
	if strings.Contains(string(encodedView), "secret") {
		t.Fatal("publish view leaked shared credentials")
	}
	if _, err := store.ClearPublishCredential("gist"); err != nil {
		t.Fatal(err)
	}
	if selected := store.PublishSettings().ForMode(model.ModeH3); selected.Gist.Enabled || selected.Gist.Token != "" || selected.Gist.GistID != "h3-gist" {
		t.Fatal("clearing shared credential changed target or left H3 publishing enabled")
	}
}

func TestExistingUnifiedPublishSettingsDoNotActivateH3Targets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	settings := publish.DefaultSettings()
	settings.Version = 3
	settings.Gist.Enabled, settings.Gist.Token, settings.Gist.GistID, settings.Gist.Filename = true, "existing-secret", "existing-gist", "ip.txt"
	encoded, _ := json.Marshal(settings)
	var document map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"cloudflare", "github", "gist"} {
		var channel map[string]json.RawMessage
		if err := json.Unmarshal(document[name], &channel); err != nil {
			t.Fatal(err)
		}
		delete(channel, "h3")
		document[name], _ = json.Marshal(channel)
	}
	encoded, _ = json.Marshal(map[string]any{"version": dataVersion, "settings": config.DefaultSettings(), "publish": document})
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded := store.PublishSettings()
	if loaded.Cloudflare.H3.Enabled || loaded.GitHub.H3.Enabled || loaded.Gist.H3.Enabled || loaded.Gist.H3.Filename != "" {
		t.Fatal("old config unexpectedly activated independent targets")
	}
	if h3 := loaded.ForMode(model.ModeH3); h3.Gist.Enabled || h3.Gist.GistID != "" || h3.Gist.Token != "existing-secret" || h3.Gist.Filename != "" {
		t.Fatal("old ordinary Gist configuration leaked into H3")
	}
}

func TestPublicationUpdatesPreserveHistorySnapshots(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	summary := model.RunSummary{RunID: "h3-history", Mode: model.ModeH3, Publications: []model.PublicationResult{{Target: "gist", State: "queued"}}}
	if err := store.AddHistory(summary); err != nil {
		t.Fatal(err)
	}
	history := store.History()
	queued, _ := store.HistoryByID(summary.RunID)
	if err := store.UpdatePublication(summary.RunID, model.PublicationResult{Target: "gist", State: "succeeded"}); err != nil {
		t.Fatal(err)
	}
	latest, _ := store.HistoryByID(summary.RunID)
	if latest.Publications[0].State != "succeeded" || history[0].Publications[0].State != "queued" || queued.Publications[0].State != "queued" {
		t.Fatal("background publication mutated an existing history snapshot")
	}
}
