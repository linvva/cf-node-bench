package config

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/linvva/cf-node-bench/internal/model"
)

func TestSettingsValidation(t *testing.T) {
	settings := DefaultSettings()
	if settings.BandwidthTestURL != DefaultBandwidthTestURL {
		t.Fatalf("default bandwidth URL = %q", settings.BandwidthTestURL)
	}
	if !settings.RetainPreviousResults {
		t.Fatal("previous results must be retained by default")
	}
	if settings.ScoreWeights != DefaultScoreWeights() {
		t.Fatalf("default score weights = %+v", settings.ScoreWeights)
	}
	if settings.AllowedCountries == nil {
		t.Fatal("default country filter must encode as an empty array")
	}
	if settings.BlockedCountries == nil {
		t.Fatal("default country blocklist must encode as an empty array")
	}
	if err := settings.Validate(); err != nil {
		t.Fatal(err)
	}
	settings.BandwidthCandidates = settings.HTTPCandidateCount + 1
	if err := settings.Validate(); err == nil {
		t.Fatal("expected candidate relationship error")
	}
}

func TestScoreWeightValidationAndMigration(t *testing.T) {
	settings := DefaultSettings()
	settings.ScoreWeights.Connection = 0
	settings.ScoreWeights.Bandwidth += 15
	if err := settings.Validate(); err != nil {
		t.Fatalf("zero weight should be valid: %v", err)
	}
	settings.ScoreWeights.HTTP++
	if err := settings.Validate(); err == nil || err.Error() != "评分权重总和必须等于 100" {
		t.Fatalf("unexpected total validation: %v", err)
	}

	legacy := DefaultSettings()
	legacy.ScoreWeights = model.ScoreWeights{}
	legacy.MigrateLegacy()
	if legacy.ScoreWeights != DefaultScoreWeights() {
		t.Fatalf("missing weights were not migrated: %+v", legacy.ScoreWeights)
	}

	explicitZero := DefaultSettings()
	explicitZero.ScoreWeights = model.ScoreWeights{}
	explicitZero.Normalize()
	if err := explicitZero.Validate(); err == nil || err.Error() != "评分权重总和必须等于 100" {
		t.Fatalf("explicit zero weights were not rejected: %v", err)
	}
}

func TestBandwidthTestURLValidationAndNormalization(t *testing.T) {
	settings := DefaultSettings()
	settings.BandwidthTestURL = "  https://downloads.example.test/file.bin?token=value  "
	settings.Normalize()
	if settings.BandwidthTestURL != "https://downloads.example.test/file.bin?token=value" {
		t.Fatalf("bandwidth URL was not normalized: %q", settings.BandwidthTestURL)
	}
	if err := settings.Validate(); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name, value, message string
	}{
		{"empty", "", "下载测速地址不能为空"},
		{"http", "http://downloads.example.test/file.bin", "下载测速地址必须是完整的 HTTPS URL"},
		{"missing host", "https:///file.bin", "下载测速地址必须是完整的 HTTPS URL"},
		{"userinfo", "https://user:secret@downloads.example.test/file.bin", "下载测速地址不能包含用户名或密码"},
		{"fragment", "https://downloads.example.test/file.bin#part", "下载测速地址不能包含片段"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			current := DefaultSettings()
			current.BandwidthTestURL = test.value
			if err := current.Validate(); err == nil || err.Error() != test.message {
				t.Fatalf("validation error = %v, want %q", err, test.message)
			}
		})
	}
}

func TestSettingsMigratesLegacyProbeCount(t *testing.T) {
	settings := DefaultSettings()
	settings.BandwidthTestURL = ""
	settings.LegacyProbeCount = 5
	settings.MigrateLegacy()
	if settings.ConnectionProbeCount != 5 || settings.HTTPProbeCount != 5 || settings.LegacyProbeCount != 0 {
		t.Fatalf("legacy probes not migrated: %+v", settings)
	}
	if settings.BandwidthTestURL != DefaultBandwidthTestURL {
		t.Fatalf("legacy bandwidth URL = %q", settings.BandwidthTestURL)
	}
}

func TestCountryBlocklistTakesPrecedence(t *testing.T) {
	settings := DefaultSettings()
	settings.BlockedCountries = []string{"CN"}
	if settings.AllowsCountry("CN") || !settings.AllowsCountry("US") {
		t.Fatal("country blocklist is not applied")
	}
	settings.AllowedCountries = []string{"CN"}
	if err := settings.Validate(); err == nil {
		t.Fatal("expected overlapping country filters to fail validation")
	}
}

func TestH3SettingsPublicDefaultAndSharedWeights(t *testing.T) {
	settings := DefaultH3Settings()
	if err := settings.Validate(); err != nil {
		t.Fatal(err)
	}
	if settings.EffectiveVerificationHost() != "h3.speed.cloudflare.com" {
		t.Fatal("missing public default")
	}
	if settings.ConnectTimeout() != 3*time.Second || settings.RequestTimeout() != 6*time.Second || settings.EffectiveBandwidthTestURL() != DefaultH3BandwidthTestURL {
		t.Fatal("wrong H3 defaults")
	}
	settings.VerificationHost = " worker.example.test. "
	settings.Normalize()
	if settings.VerificationHost != "worker.example.test" {
		t.Fatal(settings.VerificationHost)
	}
	if err := settings.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"https://worker.test", "worker.test/path", "worker.test:443", "192.0.2.1", "bad_.test", "-bad.test"} {
		invalid := settings
		invalid.VerificationHost = host
		if invalid.Validate() == nil {
			t.Fatalf("invalid host %s accepted", host)
		}
	}
	settings.ScoreWeights.TCP = 1
	if settings.Validate() == nil {
		t.Fatal("legacy metric accepted in shared scoring")
	}
	settings = DefaultH3Settings()
	settings.VerificationHost = "worker.test"
	settings.HTTPCandidateCount = 1
	if settings.Validate() == nil {
		t.Fatal("invalid H3 pool bounds accepted")
	}
}

func TestSettingsJSONMigration(t *testing.T) {
	for _, tt := range []struct {
		raw         string
		h3          bool
		concurrency int
		connection  int
		http        int
	}{
		{`{"tcpConcurrency":72,"tcpProbeCount":4,"scoreWeights":{"tcp":20,"https":20,"jitter":10,"reliability":30,"bandwidth":20}}`, false, 72, 20, 20},
		{`{"mode":"h3","workerHost":" Worker.Example.test. ","quicConcurrency":48,"connectTimeoutMs":4500,"requestTimeoutMs":7000,"bandwidthTestUrl":"https://download.test/file","scoreWeights":{"quic":15,"http3":25,"jitter":5,"reliability":15,"bandwidth":40}}`, true, 48, 15, 25},
	} {
		var s Settings
		if err := json.Unmarshal([]byte(tt.raw), &s); err != nil {
			t.Fatal(err)
		}
		if s.RequireH3 != tt.h3 || s.ConnectionConcurrency != tt.concurrency || s.ScoreWeights.Connection != tt.connection || s.ScoreWeights.HTTP != tt.http {
			t.Fatalf("unexpected migration: %+v", s)
		}
		if err := s.Validate(); err != nil {
			t.Fatal(err)
		}
		if tt.h3 && (s.H3ConnectTimeoutMS != 4500 || s.H3RequestTimeoutMS != 7000 || s.H3BandwidthTestURL != "https://download.test/file" || s.VerificationHost != "worker.example.test") {
			t.Fatal("H3-specific settings lost")
		}
		data, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		var roundTrip Settings
		if err = json.Unmarshal(data, &roundTrip); err != nil {
			t.Fatal(err)
		}
		if roundTrip.ScoreWeights != s.ScoreWeights || roundTrip.RequireH3 != s.RequireH3 {
			t.Fatal("round trip changed settings")
		}
	}
}

func TestCurrentSettingsJSONRejectsZeroWeights(t *testing.T) {
	var settings Settings
	if err := json.Unmarshal([]byte(`{"requireH3":true,"scoreWeights":{"connection":0,"http":0,"bandwidth":0,"reliability":0,"jitter":0}}`), &settings); err != nil {
		t.Fatal(err)
	}
	if err := settings.Validate(); err == nil {
		t.Fatal("current settings silently replaced invalid weights with defaults")
	}
}
