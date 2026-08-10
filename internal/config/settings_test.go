package config

import (
	"testing"

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
	settings.BandwidthCandidates = settings.TCPCandidateCount + 1
	if err := settings.Validate(); err == nil {
		t.Fatal("expected candidate relationship error")
	}
}

func TestScoreWeightValidationAndMigration(t *testing.T) {
	settings := DefaultSettings()
	settings.ScoreWeights.TCP = 0
	settings.ScoreWeights.Bandwidth += 15
	if err := settings.Validate(); err != nil {
		t.Fatalf("zero weight should be valid: %v", err)
	}
	settings.ScoreWeights.HTTPS++
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
	if settings.TCPProbeCount != 5 || settings.HTTPSProbeCount != 5 || settings.LegacyProbeCount != 0 {
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
