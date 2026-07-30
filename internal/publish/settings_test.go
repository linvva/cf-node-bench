package publish

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDefaultSettingsAndSecretMerge(t *testing.T) {
	settings := DefaultSettings()
	if settings.Cloudflare.RecordType != "A" || settings.Cloudflare.Proxied || settings.Telegram.ContentMode != "summary" {
		t.Fatalf("unexpected defaults: %+v", settings)
	}
	settings.Cloudflare.APIToken = "cf-old"
	settings.GitHub.Token = "gh-old"
	settings.Telegram.BotToken = "tg-old"
	view := settings.View()
	if !view.Cloudflare.TokenConfigured || !view.GitHub.TokenConfigured || !view.Telegram.TokenConfigured {
		t.Fatal("configured secrets were not reflected in the public view")
	}
	encoded, err := json.Marshal(view)
	if err != nil || strings.Contains(string(encoded), "cf-old") || strings.Contains(string(encoded), "gh-old") || strings.Contains(string(encoded), "tg-old") {
		t.Fatalf("public view leaked secrets: %s", encoded)
	}
	request := SaveRequest{Settings: view, GitHubToken: "gh-new"}
	merged := request.Merge(settings)
	if merged.Cloudflare.APIToken != "cf-old" || merged.GitHub.Token != "gh-new" || merged.Telegram.BotToken != "tg-old" {
		t.Fatalf("secret merge=%+v", merged)
	}
}

func TestSettingsValidation(t *testing.T) {
	settings := DefaultSettings()
	settings.Cloudflare.RecordType = "CNAME"
	if err := settings.Validate(); err == nil {
		t.Fatal("expected invalid record type")
	}
	settings = DefaultSettings()
	settings.GitHub.Path = "../ip.txt"
	if err := settings.Validate(); err == nil {
		t.Fatal("expected invalid repository path")
	}
}
