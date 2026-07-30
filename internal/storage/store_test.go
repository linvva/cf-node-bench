package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/linvva/cf-node-bench/internal/model"
	"github.com/linvva/cf-node-bench/internal/publish"
	"github.com/linvva/cf-node-bench/internal/source"
)

func TestUpdateSourceStatusPreservesEditableFields(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	sources := []source.HTTPSource{{ID: "source", Name: "edited", URL: "https://example.test/nodes", Enabled: false}}
	if err := store.SaveSources(sources); err != nil {
		t.Fatal(err)
	}
	fetched := time.Now().UTC().Truncate(time.Millisecond)
	if err := store.UpdateSourceStatus("source", fetched, "可用", 12); err != nil {
		t.Fatal(err)
	}
	updated := store.Sources()[0]
	if updated.Name != "edited" || updated.URL != sources[0].URL || updated.Enabled || updated.NodeCount != 12 {
		t.Fatalf("unexpected source: %+v", updated)
	}
}

func TestOpenNormalizesLegacyNullCollections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	content := []byte(`{"settings":{"tcpConcurrency":64,"httpsConcurrency":16,"bandwidthConcurrency":3,"connectTimeoutMs":1200,"requestTimeoutMs":4000,"bandwidthTimeoutMs":12000,"probeCount":3,"tcpCandidateCount":150,"bandwidthCandidates":30,"finalResultCount":15,"maxDownloadBytes":20971520,"allowedPorts":[443],"allowedCountries":null},"sources":null,"history":[{"runId":"old","results":null,"failures":null}]}`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if store.Settings().AllowedCountries == nil || store.Sources() == nil || store.History()[0].Results == nil || store.History()[0].Publications == nil {
		t.Fatal("legacy null collections were not normalized")
	}
	settings := store.Settings()
	if settings.TCPProbeCount != 3 || settings.HTTPSProbeCount != 3 || settings.LegacyProbeCount != 0 {
		t.Fatalf("legacy probe count was not migrated: %+v", settings)
	}
	if settings.SourceTimeoutMS == 0 || settings.BlockedCountries == nil {
		t.Fatalf("new defaults were not merged into legacy settings: %+v", settings)
	}
}

func TestPublishSettingsPreserveAndClearTokens(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	view := store.PublishSettingsView()
	view.Cloudflare.Enabled = true
	view.Cloudflare.ZoneID = "zone"
	view.Cloudflare.RecordName = "cf.example.com"
	saved, err := store.SavePublishSettings(publish.SaveRequest{Settings: view, CloudflareToken: "secret-token"})
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Cloudflare.TokenConfigured || store.PublishSettings().Cloudflare.APIToken != "secret-token" {
		t.Fatalf("token was not saved: %+v", saved.Cloudflare)
	}
	if _, err := store.SavePublishSettings(publish.SaveRequest{Settings: saved}); err != nil {
		t.Fatal(err)
	}
	if store.PublishSettings().Cloudflare.APIToken != "secret-token" {
		t.Fatal("empty token input did not preserve credential")
	}
	cleared, err := store.ClearPublishCredential("cloudflare")
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Cloudflare.TokenConfigured || cleared.Cloudflare.Enabled || store.PublishSettings().Cloudflare.APIToken != "" {
		t.Fatalf("credential was not cleared: %+v", cleared.Cloudflare)
	}
}

func TestPublicationHistoryUpdateDoesNotChangeRunState(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	summary := model.RunSummary{RunID: "run-1", State: "completed", Results: []model.ProbeResult{}, Failures: map[model.FailureReason]int{}, Publications: []model.PublicationResult{}}
	if err := store.AddHistory(summary); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdatePublication("run-1", model.PublicationResult{Target: "github", State: "failed", Message: "denied"}); err != nil {
		t.Fatal(err)
	}
	updated, ok := store.HistoryByID("run-1")
	if !ok || updated.State != "completed" || len(updated.Publications) != 1 || updated.Publications[0].State != "failed" {
		t.Fatalf("unexpected history: %+v", updated)
	}
}
