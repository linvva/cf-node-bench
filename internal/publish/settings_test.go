package publish

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDefaultSettingsAndSecretMerge(t *testing.T) {
	settings := DefaultSettings()
	if settings.Version != 3 || settings.Cloudflare.RecordType != "A" || settings.Cloudflare.Proxied || settings.Gist.Filename != "ip.txt" || settings.Telegram.ContentMode != "summary" || settings.Telegram.DeliveryMode != TelegramDeliveryDirect {
		t.Fatalf("unexpected defaults: %+v", settings)
	}
	settings.Cloudflare.APIToken = "cf-old"
	settings.GitHub.Token = "gh-old"
	settings.Gist.Token = "gist-old"
	settings.Telegram.BotToken = "tg-old"
	settings.Telegram.RelayKey = "relay-old"
	view := settings.View()
	if !view.Cloudflare.TokenConfigured || !view.GitHub.TokenConfigured || !view.Gist.TokenConfigured || !view.Telegram.TokenConfigured || !view.Telegram.RelayKeyConfigured {
		t.Fatal("configured secrets were not reflected in the public view")
	}
	encoded, err := json.Marshal(view)
	if err != nil || strings.Contains(string(encoded), "cf-old") || strings.Contains(string(encoded), "gh-old") || strings.Contains(string(encoded), "gist-old") || strings.Contains(string(encoded), "tg-old") || strings.Contains(string(encoded), "relay-old") {
		t.Fatalf("public view leaked secrets: %s", encoded)
	}
	request := SaveRequest{Settings: view, GitHubToken: "gh-new", GistToken: "gist-new", TelegramRelayKey: "relay-new"}
	merged := request.Merge(settings)
	if merged.Cloudflare.APIToken != "cf-old" || merged.GitHub.Token != "gh-new" || merged.Gist.Token != "gist-new" || merged.Telegram.BotToken != "tg-old" || merged.Telegram.RelayKey != "relay-new" {
		t.Fatalf("secret merge=%+v", merged)
	}
}

func TestSettingsV1MigrationPreservesExistingTargets(t *testing.T) {
	settings := DefaultSettings()
	settings.Version = 1
	settings.Gist = GistSettings{}
	settings.Cloudflare.APIToken = "cf"
	settings.GitHub.Token = "github"
	settings.Telegram.BotToken = "telegram"
	settings.Normalize()
	if settings.Version != 3 || settings.Gist.Filename != "ip.txt" || settings.Gist.Enabled || settings.Cloudflare.APIToken != "cf" || settings.GitHub.Token != "github" || settings.Telegram.BotToken != "telegram" || settings.Telegram.DeliveryMode != TelegramDeliveryDirect {
		t.Fatalf("migration changed existing settings: %+v", settings)
	}
}

func TestSettingsV2MigrationDefaultsTelegramToDirect(t *testing.T) {
	settings := DefaultSettings()
	settings.Version = 2
	settings.Telegram.DeliveryMode = ""
	settings.Gist.Token = "gist"
	settings.Normalize()
	if settings.Version != 3 || settings.Telegram.DeliveryMode != TelegramDeliveryDirect || settings.Gist.Token != "gist" {
		t.Fatalf("migration changed v2 settings: %+v", settings)
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
	settings = DefaultSettings()
	settings.Gist.Filename = ""
	if err := settings.Validate(); err == nil {
		t.Fatal("expected empty Gist filename")
	}
	settings = DefaultSettings()
	settings.Telegram = TelegramSettings{Enabled: true, BotToken: "bot", ChatID: "chat", ContentMode: "summary", DeliveryMode: TelegramDeliveryRelay, RelayURL: "http://relay.example/telegram", RelayKey: "key"}
	if err := settings.Validate(); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("expected invalid relay URL, got %v", err)
	}
	settings.Telegram.RelayURL = "https://relay.example/telegram"
	settings.Telegram.RelayKey = ""
	if err := settings.Validate(); err == nil || !strings.Contains(err.Error(), "访问密钥") {
		t.Fatalf("expected missing relay key, got %v", err)
	}
	settings.Telegram.RelayKey = "key"
	if err := settings.Validate(); err != nil {
		t.Fatalf("valid relay settings: %v", err)
	}
}
