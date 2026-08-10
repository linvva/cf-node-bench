package storage

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
	"github.com/linvva/cf-node-bench/internal/publish"
	runengine "github.com/linvva/cf-node-bench/internal/run"
	"github.com/linvva/cf-node-bench/internal/source"
)

func TestCheckpointPersistsAcrossStoreReopen(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := runengine.Checkpoint{
		RunID: "run-checkpoint", Candidates: []model.Candidate{{AddressType: model.AddressIPv4, IP: "1.1.1.1", Port: 443}},
		Stages: []model.StageProgress{{Name: "source", Input: 1, Passed: 1, State: "completed"}}, CompletedAt: map[string]time.Time{"source": time.Now()},
	}
	if err := store.SaveCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	checkpoint.RunID = "run-replacement"
	checkpoint.Candidates = append(checkpoint.Candidates, model.Candidate{AddressType: model.AddressIPv4, IP: "1.1.1.2", Port: 443})
	if err := store.SaveCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(filepath.Join(dir, "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := reopened.Checkpoint()
	if err != nil || !ok {
		t.Fatalf("checkpoint was not loaded: ok=%v err=%v", ok, err)
	}
	if loaded.Version != runengine.CheckpointVersion || loaded.RunID != checkpoint.RunID || len(loaded.Candidates) != 2 || loaded.UpdatedAt.IsZero() {
		t.Fatalf("unexpected checkpoint: %+v", loaded)
	}
	info, err := os.Stat(filepath.Join(dir, "checkpoint.json.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("checkpoint mode = %o", info.Mode().Perm())
	}
}

func TestCheckpointRejectsCorruptData(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "checkpoint.json.gz"), []byte("not gzip"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.Checkpoint(); err == nil || ok {
		t.Fatalf("corrupt checkpoint was accepted: ok=%v err=%v", ok, err)
	}
}

func TestCheckpointRejectsIncompatibleVersion(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(filepath.Join(dir, "checkpoint.json.gz"), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	writer := gzip.NewWriter(file)
	if err := json.NewEncoder(writer).Encode(runengine.Checkpoint{Version: runengine.CheckpointVersion + 1, RunID: "future"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.Checkpoint(); err == nil || ok {
		t.Fatalf("incompatible checkpoint was accepted: ok=%v err=%v", ok, err)
	}
}

func TestDefaultDirUsesStableApplicationName(t *testing.T) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(configDir, "CF Node Bench"); dir != want {
		t.Fatalf("default directory = %q, want %q", dir, want)
	}
}

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
	if settings.BandwidthTestURL != config.DefaultBandwidthTestURL {
		t.Fatalf("legacy bandwidth URL = %q", settings.BandwidthTestURL)
	}
	if !settings.RetainPreviousResults {
		t.Fatal("legacy settings did not enable previous-result retention")
	}
	if settings.ScoreWeights != config.DefaultScoreWeights() {
		t.Fatalf("legacy settings did not receive new default weights: %+v", settings.ScoreWeights)
	}
	if store.History()[0].ScoreWeights != config.LegacyScoreWeights() {
		t.Fatalf("legacy history did not retain old score formula: %+v", store.History()[0].ScoreWeights)
	}
}

func TestSaveSettingsNormalizesBandwidthURL(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	settings := store.Settings()
	settings.BandwidthTestURL = "  https://downloads.example.test/file.bin  "
	if err := store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	if got := store.Settings().BandwidthTestURL; got != "https://downloads.example.test/file.bin" {
		t.Fatalf("saved bandwidth URL = %q", got)
	}
}

func TestSaveSettingsPreservesDisabledPreviousResultRetention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	settings := store.Settings()
	settings.RetainPreviousResults = false
	if err := store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Settings().RetainPreviousResults {
		t.Fatal("explicitly disabled previous-result retention was not preserved")
	}
}

func TestLatestCompletedResultsSkipsCancelledAndStopsAtEmptyCompletion(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	kept := model.ProbeResult{Candidate: model.Candidate{AddressType: model.AddressIPv4, IP: "1.1.1.1", Port: 443}}
	if err := store.AddHistory(model.RunSummary{RunID: "completed", State: "completed", Results: []model.ProbeResult{kept}}); err != nil {
		t.Fatal(err)
	}
	if err := store.AddHistory(model.RunSummary{RunID: "cancelled", State: "cancelled", Results: []model.ProbeResult{{Candidate: model.Candidate{AddressType: model.AddressIPv4, IP: "1.1.1.2", Port: 443}}}}); err != nil {
		t.Fatal(err)
	}
	results := store.LatestCompletedResults()
	if len(results) != 1 || results[0].Candidate.Key() != kept.Candidate.Key() {
		t.Fatalf("cancelled run replaced completed results: %+v", results)
	}
	if err := store.AddHistory(model.RunSummary{RunID: "empty", State: "completed", Results: []model.ProbeResult{}}); err != nil {
		t.Fatal(err)
	}
	if results := store.LatestCompletedResults(); len(results) != 0 {
		t.Fatalf("empty completion fell back to older results: %+v", results)
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

func TestPublishSettingsMigrateV1AndManageGistToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	content := []byte(`{"publish":{"version":1,"output":{"country":true,"httpLatency":true,"bandwidth":true},"request":{"timeoutMs":10000,"maxRetries":2,"retryDelayMs":1000},"cloudflare":{"recordType":"A","ttl":60,"apiToken":"cf-old"},"github":{"token":"gh-old","branch":"main","path":"ip.txt"},"telegram":{"botToken":"tg-old","contentMode":"summary"}}}`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	settings := store.PublishSettings()
	if settings.Version != 3 || settings.Gist.Enabled || settings.Gist.Filename != "ip.txt" || settings.Cloudflare.APIToken != "cf-old" || settings.GitHub.Token != "gh-old" || settings.Telegram.BotToken != "tg-old" || settings.Telegram.DeliveryMode != publish.TelegramDeliveryDirect {
		t.Fatalf("migration changed existing settings: %+v", settings)
	}

	view := store.PublishSettingsView()
	view.Gist.Enabled = true
	view.Gist.GistID = "gist-id"
	saved, err := store.SavePublishSettings(publish.SaveRequest{Settings: view, GistToken: "gist-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Gist.TokenConfigured || store.PublishSettings().Gist.Token != "gist-secret" {
		t.Fatalf("Gist token was not saved: %+v", saved.Gist)
	}
	if _, err := store.SavePublishSettings(publish.SaveRequest{Settings: saved}); err != nil {
		t.Fatal(err)
	}
	if store.PublishSettings().Gist.Token != "gist-secret" {
		t.Fatal("empty Gist token input did not preserve credential")
	}
	cleared, err := store.ClearPublishCredential("gist")
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Gist.TokenConfigured || cleared.Gist.Enabled || store.PublishSettings().Gist.Token != "" {
		t.Fatalf("Gist credential was not cleared: %+v", cleared.Gist)
	}
}

func TestPublishSettingsPreserveAndClearTelegramRelayKey(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	view := store.PublishSettingsView()
	view.Telegram.Enabled = true
	view.Telegram.ChatID = "123"
	view.Telegram.DeliveryMode = publish.TelegramDeliveryRelay
	view.Telegram.RelayURL = "https://relay.example/telegram"
	saved, err := store.SavePublishSettings(publish.SaveRequest{Settings: view, TelegramBotToken: "bot-secret", TelegramRelayKey: "relay-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Telegram.TokenConfigured || !saved.Telegram.RelayKeyConfigured {
		t.Fatalf("Telegram credentials were not saved: %+v", saved.Telegram)
	}
	if _, err := store.SavePublishSettings(publish.SaveRequest{Settings: saved}); err != nil {
		t.Fatal(err)
	}
	settings := store.PublishSettings()
	if settings.Telegram.BotToken != "bot-secret" || settings.Telegram.RelayKey != "relay-secret" {
		t.Fatalf("empty inputs did not preserve credentials: %+v", settings.Telegram)
	}
	cleared, err := store.ClearPublishCredential("telegramRelay")
	if err != nil {
		t.Fatal(err)
	}
	settings = store.PublishSettings()
	if cleared.Telegram.RelayKeyConfigured || cleared.Telegram.Enabled || settings.Telegram.RelayKey != "" || settings.Telegram.BotToken != "bot-secret" {
		t.Fatalf("relay credential clear changed the wrong fields: %+v", settings.Telegram)
	}
	view = store.PublishSettingsView()
	view.Telegram.Enabled = true
	view.Telegram.DeliveryMode = publish.TelegramDeliveryDirect
	if _, err := store.SavePublishSettings(publish.SaveRequest{Settings: view, TelegramRelayKey: "relay-again"}); err != nil {
		t.Fatal(err)
	}
	cleared, err = store.ClearPublishCredential("telegram")
	if err != nil {
		t.Fatal(err)
	}
	settings = store.PublishSettings()
	if cleared.Telegram.TokenConfigured || cleared.Telegram.Enabled || settings.Telegram.BotToken != "" || settings.Telegram.RelayKey != "relay-again" {
		t.Fatalf("Bot credential clear changed the wrong fields: %+v", settings.Telegram)
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
