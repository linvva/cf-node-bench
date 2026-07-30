package publish

import (
	"fmt"
	"path"
	"strings"
)

const SettingsVersion = 1

type OutputFields struct {
	Country     bool `json:"country"`
	TCPP95      bool `json:"tcpP95"`
	HTTPLatency bool `json:"httpLatency"`
	Bandwidth   bool `json:"bandwidth"`
}

type RequestPolicy struct {
	TimeoutMS    int `json:"timeoutMs"`
	MaxRetries   int `json:"maxRetries"`
	RetryDelayMS int `json:"retryDelayMs"`
}

type CloudflareSettings struct {
	Enabled    bool   `json:"enabled"`
	APIToken   string `json:"apiToken,omitempty"`
	ZoneID     string `json:"zoneId"`
	RecordName string `json:"recordName"`
	RecordType string `json:"recordType"`
	TTL        int    `json:"ttl"`
	Proxied    bool   `json:"proxied"`
}

type GitHubSettings struct {
	Enabled    bool   `json:"enabled"`
	Token      string `json:"token,omitempty"`
	Owner      string `json:"owner"`
	Repository string `json:"repository"`
	Branch     string `json:"branch"`
	Path       string `json:"path"`
}

type TelegramSettings struct {
	Enabled     bool   `json:"enabled"`
	BotToken    string `json:"botToken,omitempty"`
	ChatID      string `json:"chatId"`
	ContentMode string `json:"contentMode"`
}

type Settings struct {
	Version    int                `json:"version"`
	Output     OutputFields       `json:"output"`
	Request    RequestPolicy      `json:"request"`
	Cloudflare CloudflareSettings `json:"cloudflare"`
	GitHub     GitHubSettings     `json:"github"`
	Telegram   TelegramSettings   `json:"telegram"`
}

type CloudflareView struct {
	Enabled         bool   `json:"enabled"`
	TokenConfigured bool   `json:"tokenConfigured"`
	ZoneID          string `json:"zoneId"`
	RecordName      string `json:"recordName"`
	RecordType      string `json:"recordType"`
	TTL             int    `json:"ttl"`
	Proxied         bool   `json:"proxied"`
}

type GitHubView struct {
	Enabled         bool   `json:"enabled"`
	TokenConfigured bool   `json:"tokenConfigured"`
	Owner           string `json:"owner"`
	Repository      string `json:"repository"`
	Branch          string `json:"branch"`
	Path            string `json:"path"`
}

type TelegramView struct {
	Enabled         bool   `json:"enabled"`
	TokenConfigured bool   `json:"tokenConfigured"`
	ChatID          string `json:"chatId"`
	ContentMode     string `json:"contentMode"`
}

type SettingsView struct {
	Output     OutputFields   `json:"output"`
	Request    RequestPolicy  `json:"request"`
	Cloudflare CloudflareView `json:"cloudflare"`
	GitHub     GitHubView     `json:"github"`
	Telegram   TelegramView   `json:"telegram"`
}

type SaveRequest struct {
	Settings         SettingsView `json:"settings"`
	CloudflareToken  string       `json:"cloudflareToken"`
	GitHubToken      string       `json:"githubToken"`
	TelegramBotToken string       `json:"telegramBotToken"`
}

func DefaultSettings() Settings {
	return Settings{
		Version:    SettingsVersion,
		Output:     OutputFields{Country: true, HTTPLatency: true, Bandwidth: true},
		Request:    RequestPolicy{TimeoutMS: 10000, MaxRetries: 2, RetryDelayMS: 1000},
		Cloudflare: CloudflareSettings{RecordType: "A", TTL: 60},
		GitHub:     GitHubSettings{Branch: "main", Path: "ip.txt"},
		Telegram:   TelegramSettings{ContentMode: "summary"},
	}
}

func (s *Settings) Normalize() {
	if s.Version == 0 {
		*s = DefaultSettings()
	}
}

func (s Settings) View() SettingsView {
	return SettingsView{
		Output:     s.Output,
		Request:    s.Request,
		Cloudflare: CloudflareView{Enabled: s.Cloudflare.Enabled, TokenConfigured: s.Cloudflare.APIToken != "", ZoneID: s.Cloudflare.ZoneID, RecordName: s.Cloudflare.RecordName, RecordType: s.Cloudflare.RecordType, TTL: s.Cloudflare.TTL, Proxied: s.Cloudflare.Proxied},
		GitHub:     GitHubView{Enabled: s.GitHub.Enabled, TokenConfigured: s.GitHub.Token != "", Owner: s.GitHub.Owner, Repository: s.GitHub.Repository, Branch: s.GitHub.Branch, Path: s.GitHub.Path},
		Telegram:   TelegramView{Enabled: s.Telegram.Enabled, TokenConfigured: s.Telegram.BotToken != "", ChatID: s.Telegram.ChatID, ContentMode: s.Telegram.ContentMode},
	}
}

func (r SaveRequest) Merge(current Settings) Settings {
	next := Settings{
		Version: SettingsVersion, Output: r.Settings.Output, Request: r.Settings.Request,
		Cloudflare: CloudflareSettings{Enabled: r.Settings.Cloudflare.Enabled, APIToken: current.Cloudflare.APIToken, ZoneID: strings.TrimSpace(r.Settings.Cloudflare.ZoneID), RecordName: strings.TrimSpace(r.Settings.Cloudflare.RecordName), RecordType: strings.ToUpper(r.Settings.Cloudflare.RecordType), TTL: r.Settings.Cloudflare.TTL, Proxied: r.Settings.Cloudflare.Proxied},
		GitHub:     GitHubSettings{Enabled: r.Settings.GitHub.Enabled, Token: current.GitHub.Token, Owner: strings.TrimSpace(r.Settings.GitHub.Owner), Repository: strings.TrimSpace(r.Settings.GitHub.Repository), Branch: strings.TrimSpace(r.Settings.GitHub.Branch), Path: strings.TrimSpace(r.Settings.GitHub.Path)},
		Telegram:   TelegramSettings{Enabled: r.Settings.Telegram.Enabled, BotToken: current.Telegram.BotToken, ChatID: strings.TrimSpace(r.Settings.Telegram.ChatID), ContentMode: r.Settings.Telegram.ContentMode},
	}
	if token := strings.TrimSpace(r.CloudflareToken); token != "" {
		next.Cloudflare.APIToken = token
	}
	if token := strings.TrimSpace(r.GitHubToken); token != "" {
		next.GitHub.Token = token
	}
	if token := strings.TrimSpace(r.TelegramBotToken); token != "" {
		next.Telegram.BotToken = token
	}
	return next
}

func (s Settings) Validate() error {
	if s.Request.TimeoutMS < 1000 || s.Request.TimeoutMS > 60000 {
		return fmt.Errorf("发布请求超时必须在 1000 到 60000 ms 之间")
	}
	if s.Request.MaxRetries < 0 || s.Request.MaxRetries > 5 {
		return fmt.Errorf("发布重试次数必须在 0 到 5 之间")
	}
	if s.Request.RetryDelayMS < 0 || s.Request.RetryDelayMS > 10000 {
		return fmt.Errorf("发布重试间隔必须在 0 到 10000 ms 之间")
	}
	if s.Cloudflare.RecordType != "A" && s.Cloudflare.RecordType != "TXT" {
		return fmt.Errorf("Cloudflare 记录类型必须是 A 或 TXT")
	}
	if s.Cloudflare.TTL != 1 && (s.Cloudflare.TTL < 60 || s.Cloudflare.TTL > 86400) {
		return fmt.Errorf("Cloudflare TTL 必须为 1 或在 60 到 86400 之间")
	}
	if s.Cloudflare.Enabled && (s.Cloudflare.APIToken == "" || s.Cloudflare.ZoneID == "" || s.Cloudflare.RecordName == "") {
		return fmt.Errorf("启用 Cloudflare 前必须填写 Token、Zone ID 和记录名")
	}
	if s.GitHub.Path == "" || strings.HasPrefix(s.GitHub.Path, "/") || path.Clean(s.GitHub.Path) != s.GitHub.Path || strings.HasPrefix(s.GitHub.Path, "../") {
		return fmt.Errorf("GitHub 文件路径必须是仓库内的有效相对路径")
	}
	if s.GitHub.Enabled && (s.GitHub.Token == "" || s.GitHub.Owner == "" || s.GitHub.Repository == "" || s.GitHub.Branch == "") {
		return fmt.Errorf("启用 GitHub 前必须填写 Token、仓库、分支和文件路径")
	}
	if s.Telegram.ContentMode != "summary" && s.Telegram.ContentMode != "details" {
		return fmt.Errorf("Telegram 内容模式必须是 summary 或 details")
	}
	if s.Telegram.Enabled && (s.Telegram.BotToken == "" || s.Telegram.ChatID == "") {
		return fmt.Errorf("启用 Telegram 前必须填写 Bot Token 和 Chat ID")
	}
	return nil
}
