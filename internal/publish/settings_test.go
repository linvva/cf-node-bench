package publish

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/linvva/cf-node-bench/internal/model"
)

func TestDefaultSettingsAndSecretMerge(t *testing.T) {
	settings := DefaultSettings()
	if settings.Version != SettingsVersion || settings.Cloudflare.RecordType != "A" || settings.Cloudflare.Proxied || settings.Gist.Filename != "" || settings.Telegram.ContentMode != "summary" || settings.Telegram.DeliveryMode != TelegramDeliveryDirect {
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

func TestH3TargetsKeepSharedCredentialsWithoutFallback(t *testing.T) {
	settings := DefaultSettings()
	settings.Cloudflare.APIToken, settings.Cloudflare.ZoneID, settings.Cloudflare.RecordName = "cf-secret", "shared-zone", "ordinary.example.com"
	settings.GitHub.Token, settings.GitHub.Owner, settings.GitHub.Repository, settings.GitHub.Path = "gh-secret", "owner", "repo", "ip.txt"
	settings.Gist.Token, settings.Gist.GistID, settings.Gist.Filename = "gist-secret", "ordinary-gist", "ip.txt"
	settings.Cloudflare.Enabled, settings.GitHub.Enabled, settings.Gist.Enabled = true, true, true
	if got := settings.ForMode(model.ModeH3); got.Cloudflare.Enabled || got.GitHub.Enabled || got.Gist.Enabled || got.Cloudflare.RecordName != "" || got.GitHub.Path != "" || got.Gist.GistID != "" {
		t.Fatal("empty H3 destinations fell back to ordinary settings")
	}
	view := settings.View()
	view.Cloudflare.H3 = CloudflareH3Target{RecordName: " H3.Example.com. ", RecordType: " txt "}
	view.GitHub.H3 = GitHubH3Target{Path: " lists/h3.txt "}
	view.Gist.H3 = GistH3Target{GistID: " h3-gist ", Filename: " h3.txt "}
	settings = (SaveRequest{Settings: view, TestMode: model.ModeH3}).Merge(settings)
	if err := settings.Validate(); err != nil {
		t.Fatal(err)
	}
	h3 := settings.ForMode(model.ModeH3)
	if h3.Cloudflare.RecordName != "h3.example.com" || h3.Cloudflare.RecordType != "TXT" || h3.GitHub.Path != "lists/h3.txt" || h3.Gist.GistID != "h3-gist" || h3.Gist.Filename != "h3.txt" {
		t.Fatalf("H3 target selection failed: %+v", h3.View())
	}
	if h3.Cloudflare.APIToken != settings.Cloudflare.APIToken || h3.Cloudflare.ZoneID != settings.Cloudflare.ZoneID || h3.GitHub.Token != settings.GitHub.Token || h3.GitHub.Owner != settings.GitHub.Owner || h3.GitHub.Repository != settings.GitHub.Repository || h3.GitHub.Branch != settings.GitHub.Branch || h3.Gist.Token != settings.Gist.Token || h3.Telegram != settings.Telegram {
		t.Fatal("selecting H3 changed shared credentials or account settings")
	}
	if err := h3.Validate(); err != nil {
		t.Fatalf("resolved H3 settings cannot be validated: %v", err)
	}
	ordinary := settings.ForMode(model.ModeTCPHTTPS)
	if ordinary.Cloudflare.RecordName != "ordinary.example.com" || ordinary.Cloudflare.RecordType != "A" || ordinary.GitHub.Path != "ip.txt" || ordinary.Gist.GistID != "ordinary-gist" || ordinary.Gist.Filename != "ip.txt" {
		t.Fatal("selecting H3 mutated ordinary targets")
	}
	if settings.Cloudflare.H3.RecordName != "h3.example.com" || settings.GitHub.H3.Path != "lists/h3.txt" || settings.Gist.H3.GistID != "h3-gist" {
		t.Fatal("resolving a target mutated stored destinations")
	}
	settings.Cloudflare.H3 = CloudflareH3Target{RecordType: "TXT"}
	settings.GitHub.H3 = GitHubH3Target{}
	settings.Gist.H3 = GistH3Target{}
	if got := settings.ForMode(model.ModeH3); got.Cloudflare.Enabled || got.GitHub.Enabled || got.Gist.Enabled {
		t.Fatal("clearing H3 destinations restored ordinary destinations")
	}
	encoded, err := json.Marshal(settings.View())
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"cf-secret", "gh-secret", "gist-secret"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("public view leaked shared credentials")
		}
	}
	encoded, _ = json.Marshal(settings)
	if strings.Contains(string(encoded), "testMode") {
		t.Fatal("connection-test selection persisted as configuration")
	}
}

func TestH3TargetValidation(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Settings)
		want string
	}{
		{"CF same normalized name", func(s *Settings) {
			s.Cloudflare.H3 = CloudflareH3Target{RecordName: " Ordinary.Example.com. ", RecordType: "TXT"}
		}, "必须与普通测速不同"},
		{"CF missing", func(s *Settings) { s.Cloudflare.H3 = CloudflareH3Target{RecordType: "TXT"} }, ""},
		{"CF URL", func(s *Settings) {
			s.Cloudflare.H3 = CloudflareH3Target{RecordName: "https://h3.example.com", RecordType: "TXT"}
		}, "完整域名"},
		{"CF unsupported type", func(s *Settings) {
			s.Cloudflare.H3 = CloudflareH3Target{RecordName: "h3.example.com", RecordType: "CNAME"}
		}, "A 或 TXT"},
		{"GitHub same file", func(s *Settings) { s.GitHub.H3 = GitHubH3Target{Path: "ip.txt"} }, "必须与普通测速不同"},
		{"GitHub empty", func(s *Settings) { s.GitHub.H3 = GitHubH3Target{} }, ""},
		{"GitHub traversal", func(s *Settings) { s.GitHub.H3 = GitHubH3Target{Path: "../h3.txt"} }, "有效相对路径"},
		{"GitHub directory", func(s *Settings) { s.GitHub.H3 = GitHubH3Target{Path: "."} }, "有效相对路径"},
		{"Gist same target", func(s *Settings) { s.Gist.H3 = GistH3Target{GistID: "ABCDEF", Filename: "ip.txt"} }, "不同的 Gist ID 或文件名"},
		{"Gist empty ID", func(s *Settings) { s.Gist.H3 = GistH3Target{Filename: "h3.txt"} }, "有效 ID"},
		{"Gist URL", func(s *Settings) {
			s.Gist.H3 = GistH3Target{GistID: "https://gist.github.com/abcdef", Filename: "h3.txt"}
		}, "有效 ID"},
		{"Gist filename path", func(s *Settings) { s.Gist.H3 = GistH3Target{GistID: "abcdef", Filename: "lists/h3.txt"} }, "有效文件名"},
		{"Gist different file", func(s *Settings) { s.Gist.H3 = GistH3Target{GistID: "abcdef", Filename: "h3.txt"} }, ""},
		{"Gist different ID", func(s *Settings) { s.Gist.H3 = GistH3Target{GistID: "fedcba", Filename: "ip.txt"} }, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := DefaultSettings()
			s.Cloudflare.RecordName, s.Gist.GistID = "ordinary.example.com", "abcdef"
			s.GitHub.Path, s.Gist.Filename = "ip.txt", "ip.txt"
			tt.edit(&s)
			err := s.Validate()
			if tt.want == "" && err != nil || tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)) {
				t.Fatalf("error=%v, want %q", err, tt.want)
			}
		})
	}
}

func TestOldPublishSettingsDoNotActivateH3Targets(t *testing.T) {
	var settings Settings
	if err := json.Unmarshal([]byte(`{"version":3,"cloudflare":{"apiToken":"cf","zoneId":"zone","recordName":"ordinary.example.com","recordType":"A","ttl":60},"github":{"token":"gh","owner":"owner","repository":"repo","branch":"main","path":"ip.txt"},"gist":{"token":"gist","gistId":"original","filename":"ip.txt"}}`), &settings); err != nil {
		t.Fatal(err)
	}
	settings.Normalize()
	if settings.Cloudflare.H3.RecordName != "" || settings.Cloudflare.H3.RecordType != "TXT" || settings.GitHub.H3.Path != "" || settings.Gist.H3 != (GistH3Target{}) {
		t.Fatal("legacy settings activated H3 destinations")
	}
	if settings.Cloudflare.RecordName != "ordinary.example.com" || settings.GitHub.Path != "ip.txt" || settings.Gist.GistID != "original" || settings.Gist.Filename != "ip.txt" {
		t.Fatal("migration changed ordinary publication destinations")
	}
}

func TestLegacyH3SwitchMigration(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		s := DefaultSettings()
		s.Version = 3
		s.GitHub.Path, s.Gist.Filename = "ip.txt", "ip.txt"
		s.Cloudflare.H3 = CloudflareH3Target{Enabled: enabled, RecordName: "h3.example.com", RecordType: "TXT"}
		s.GitHub.H3 = GitHubH3Target{Enabled: enabled, Path: "h3.txt"}
		s.Gist.H3 = GistH3Target{Enabled: enabled, GistID: "h3-gist", Filename: "h3.txt"}
		s.Normalize()
		if s.Cloudflare.H3.Enabled || s.GitHub.H3.Enabled || s.Gist.H3.Enabled {
			t.Fatal("legacy switch survived migration")
		}
		if s.GitHub.Path != "" || s.Gist.Filename != "" {
			t.Fatal("unconfigured legacy default filenames became destinations")
		}
		if enabled {
			if s.Cloudflare.H3.RecordName != "h3.example.com" || s.GitHub.H3.Path != "h3.txt" || s.Gist.H3.GistID != "h3-gist" || s.Gist.H3.Filename != "h3.txt" {
				t.Fatal("enabled legacy destinations were lost")
			}
		} else if s.Cloudflare.H3.RecordName != "" || s.GitHub.H3.Path != "" || s.Gist.H3 != (GistH3Target{}) {
			t.Fatal("disabled legacy destinations became active")
		}
		before := s
		s.Normalize()
		if s != before {
			t.Fatal("current destinations changed when normalized again")
		}
		encoded, _ := json.Marshal(s.View())
		var view map[string]map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &view); err != nil {
			t.Fatal(err)
		}
		for _, channel := range []string{"cloudflare", "github", "gist"} {
			if strings.Contains(string(view[channel]["h3"]), "enabled") {
				t.Fatal("legacy switch exposed in current view")
			}
		}
	}
}

func TestH3OnlyTargetsAreValidAndKeepOrdinaryDisabled(t *testing.T) {
	s := DefaultSettings()
	s.Cloudflare.Enabled, s.Cloudflare.APIToken, s.Cloudflare.ZoneID = true, "cf", "zone"
	s.Cloudflare.H3 = CloudflareH3Target{RecordName: "h3.example.com", RecordType: "TXT"}
	s.GitHub.Enabled, s.GitHub.Token, s.GitHub.Owner, s.GitHub.Repository = true, "gh", "owner", "repo"
	s.GitHub.H3.Path = "h3.txt"
	s.Gist.Enabled, s.Gist.Token = true, "gist"
	s.Gist.H3 = GistH3Target{GistID: "h3-gist", Filename: "h3.txt"}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	ordinary := s.ForMode(model.ModeTCPHTTPS)
	if ordinary.Cloudflare.Enabled || ordinary.GitHub.Enabled || ordinary.Gist.Enabled {
		t.Fatal("H3-only targets enabled ordinary publication")
	}
	h3 := s.ForMode(model.ModeH3)
	if !h3.Cloudflare.Enabled || !h3.GitHub.Enabled || !h3.Gist.Enabled {
		t.Fatal("configured H3 targets disabled")
	}
	if err := h3.Validate(); err != nil {
		t.Fatal(err)
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
	if settings.Version != SettingsVersion || settings.Gist.Filename != "" || settings.Gist.Enabled || settings.Cloudflare.APIToken != "cf" || settings.GitHub.Token != "github" || settings.Telegram.BotToken != "telegram" || settings.Telegram.DeliveryMode != TelegramDeliveryDirect {
		t.Fatalf("migration changed existing settings: %+v", settings)
	}
}

func TestSettingsV2MigrationDefaultsTelegramToDirect(t *testing.T) {
	settings := DefaultSettings()
	settings.Version = 2
	settings.Telegram.DeliveryMode = ""
	settings.Gist.Token = "gist"
	settings.Normalize()
	if settings.Version != SettingsVersion || settings.Telegram.DeliveryMode != TelegramDeliveryDirect || settings.Gist.Token != "gist" {
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
	settings.Gist.GistID = "partial-gist"
	if err := settings.Validate(); err == nil {
		t.Fatal("expected partial Gist destination to fail")
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
