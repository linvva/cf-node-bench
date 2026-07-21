package config

import "testing"

func TestSettingsValidation(t *testing.T) {
	settings := DefaultSettings()
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

func TestSettingsMigratesLegacyProbeCount(t *testing.T) {
	settings := DefaultSettings()
	settings.LegacyProbeCount = 5
	settings.MigrateLegacy()
	if settings.TCPProbeCount != 5 || settings.HTTPSProbeCount != 5 || settings.LegacyProbeCount != 0 {
		t.Fatalf("legacy probes not migrated: %+v", settings)
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
