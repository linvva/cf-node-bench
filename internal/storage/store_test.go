package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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
	if store.Settings().AllowedCountries == nil || store.Sources() == nil || store.History()[0].Results == nil {
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
