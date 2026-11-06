package publish

import (
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
)

const SettingsVersion = 4

const (
	TelegramDeliveryDirect = "direct"
	TelegramDeliveryRelay  = "relay"
)

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

type CloudflareH3Target struct {
	// Enabled is read only to migrate the optional destination switch from version 3.
	Enabled    bool   `json:"enabled,omitempty"`
	RecordName string `json:"recordName"`
	RecordType string `json:"recordType"`
}

type GitHubH3Target struct {
	// Enabled is read only to migrate the optional destination switch from version 3.
	Enabled bool   `json:"enabled,omitempty"`
	Path    string `json:"path"`
}

type GistH3Target struct {
	// Enabled is read only to migrate the optional destination switch from version 3.
	Enabled  bool   `json:"enabled,omitempty"`
	GistID   string `json:"gistId"`
	Filename string `json:"filename"`
}

type CloudflareSettings struct {
	H3         CloudflareH3Target `json:"h3"`
	Enabled    bool               `json:"enabled"`
	APIToken   string             `json:"apiToken,omitempty"`
	ZoneID     string             `json:"zoneId"`
	RecordName string             `json:"recordName"`
	RecordType string             `json:"recordType"`
	TTL        int                `json:"ttl"`
	Proxied    bool               `json:"proxied"`
}

type GitHubSettings struct {
	H3         GitHubH3Target `json:"h3"`
	Enabled    bool           `json:"enabled"`
	Token      string         `json:"token,omitempty"`
	Owner      string         `json:"owner"`
	Repository string         `json:"repository"`
	Branch     string         `json:"branch"`
	Path       string         `json:"path"`
}

type GistSettings struct {
	H3       GistH3Target `json:"h3"`
	Enabled  bool         `json:"enabled"`
	Token    string       `json:"token,omitempty"`
	GistID   string       `json:"gistId"`
	Filename string       `json:"filename"`
}

type TelegramSettings struct {
	Enabled      bool   `json:"enabled"`
	BotToken     string `json:"botToken,omitempty"`
	ChatID       string `json:"chatId"`
	ContentMode  string `json:"contentMode"`
	DeliveryMode string `json:"deliveryMode"`
	RelayURL     string `json:"relayUrl"`
	RelayKey     string `json:"relayKey,omitempty"`
}

type Settings struct {
	Version    int                `json:"version"`
	Output     OutputFields       `json:"output"`
	Request    RequestPolicy      `json:"request"`
	Cloudflare CloudflareSettings `json:"cloudflare"`
	GitHub     GitHubSettings     `json:"github"`
	Gist       GistSettings       `json:"gist"`
	Telegram   TelegramSettings   `json:"telegram"`
}

type CloudflareView struct {
	H3              CloudflareH3Target `json:"h3"`
	Enabled         bool               `json:"enabled"`
	TokenConfigured bool               `json:"tokenConfigured"`
	ZoneID          string             `json:"zoneId"`
	RecordName      string             `json:"recordName"`
	RecordType      string             `json:"recordType"`
	TTL             int                `json:"ttl"`
	Proxied         bool               `json:"proxied"`
}

type GitHubView struct {
	H3              GitHubH3Target `json:"h3"`
	Enabled         bool           `json:"enabled"`
	TokenConfigured bool           `json:"tokenConfigured"`
	Owner           string         `json:"owner"`
	Repository      string         `json:"repository"`
	Branch          string         `json:"branch"`
	Path            string         `json:"path"`
}

type GistView struct {
	H3              GistH3Target `json:"h3"`
	Enabled         bool         `json:"enabled"`
	TokenConfigured bool         `json:"tokenConfigured"`
	GistID          string       `json:"gistId"`
	Filename        string       `json:"filename"`
}

type TelegramView struct {
	Enabled            bool   `json:"enabled"`
	TokenConfigured    bool   `json:"tokenConfigured"`
	ChatID             string `json:"chatId"`
	ContentMode        string `json:"contentMode"`
	DeliveryMode       string `json:"deliveryMode"`
	RelayURL           string `json:"relayUrl"`
	RelayKeyConfigured bool   `json:"relayKeyConfigured"`
}

type SettingsView struct {
	Output     OutputFields   `json:"output"`
	Request    RequestPolicy  `json:"request"`
	Cloudflare CloudflareView `json:"cloudflare"`
	GitHub     GitHubView     `json:"github"`
	Gist       GistView       `json:"gist"`
	Telegram   TelegramView   `json:"telegram"`
}

type SaveRequest struct {
	TestMode         model.Mode   `json:"testMode,omitempty"`
	Settings         SettingsView `json:"settings"`
	CloudflareToken  string       `json:"cloudflareToken"`
	GitHubToken      string       `json:"githubToken"`
	GistToken        string       `json:"gistToken"`
	TelegramBotToken string       `json:"telegramBotToken"`
	TelegramRelayKey string       `json:"telegramRelayKey"`
}

func DefaultSettings() Settings {
	return Settings{
		Version:    SettingsVersion,
		Output:     OutputFields{Country: true, HTTPLatency: true, Bandwidth: true},
		Request:    RequestPolicy{TimeoutMS: 10000, MaxRetries: 2, RetryDelayMS: 1000},
		Cloudflare: CloudflareSettings{RecordType: "A", TTL: 60, H3: CloudflareH3Target{RecordType: "TXT"}},
		GitHub:     GitHubSettings{Branch: "main"},
		Gist:       GistSettings{},
		Telegram:   TelegramSettings{ContentMode: "summary", DeliveryMode: TelegramDeliveryDirect},
	}
}

func (s *Settings) Normalize() {
	if s.Version == 0 {
		*s = DefaultSettings()
		return
	}
	if s.Version < 2 && s.Gist.GistID != "" && strings.TrimSpace(s.Gist.Filename) == "" {
		s.Gist.Filename = "ip.txt"
	}
	if s.Version < 3 && s.Telegram.DeliveryMode == "" {
		s.Telegram.DeliveryMode = TelegramDeliveryDirect
	}
	if s.Version < 4 {
		if !s.Cloudflare.H3.Enabled {
			s.Cloudflare.H3 = CloudflareH3Target{RecordType: "TXT"}
		}
		if !s.GitHub.H3.Enabled {
			s.GitHub.H3 = GitHubH3Target{}
		}
		if !s.Gist.H3.Enabled {
			s.Gist.H3 = GistH3Target{}
		}
		if s.GitHub.Owner == "" && s.GitHub.Repository == "" && s.GitHub.Path == "ip.txt" {
			s.GitHub.Path = ""
		}
		if s.Gist.GistID == "" && s.Gist.Filename == "ip.txt" {
			s.Gist.Filename = ""
		}
	}
	if s.Cloudflare.H3.RecordType == "" {
		s.Cloudflare.H3.RecordType = "TXT"
	}
	s.Cloudflare.H3.Enabled, s.GitHub.H3.Enabled, s.Gist.H3.Enabled = false, false, false
	s.Version = SettingsVersion
}

func (s Settings) View() SettingsView {
	return SettingsView{
		Output:     s.Output,
		Request:    s.Request,
		Cloudflare: CloudflareView{H3: s.Cloudflare.H3, Enabled: s.Cloudflare.Enabled, TokenConfigured: s.Cloudflare.APIToken != "", ZoneID: s.Cloudflare.ZoneID, RecordName: s.Cloudflare.RecordName, RecordType: s.Cloudflare.RecordType, TTL: s.Cloudflare.TTL, Proxied: s.Cloudflare.Proxied},
		GitHub:     GitHubView{H3: s.GitHub.H3, Enabled: s.GitHub.Enabled, TokenConfigured: s.GitHub.Token != "", Owner: s.GitHub.Owner, Repository: s.GitHub.Repository, Branch: s.GitHub.Branch, Path: s.GitHub.Path},
		Gist:       GistView{H3: s.Gist.H3, Enabled: s.Gist.Enabled, TokenConfigured: s.Gist.Token != "", GistID: s.Gist.GistID, Filename: s.Gist.Filename},
		Telegram: TelegramView{
			Enabled: s.Telegram.Enabled, TokenConfigured: s.Telegram.BotToken != "", ChatID: s.Telegram.ChatID,
			ContentMode: s.Telegram.ContentMode, DeliveryMode: s.Telegram.DeliveryMode, RelayURL: s.Telegram.RelayURL,
			RelayKeyConfigured: s.Telegram.RelayKey != "",
		},
	}
}

func (r SaveRequest) Merge(current Settings) Settings {
	next := Settings{
		Version: SettingsVersion, Output: r.Settings.Output, Request: r.Settings.Request,
		Cloudflare: CloudflareSettings{H3: CloudflareH3Target{RecordName: normalizeRecordName(r.Settings.Cloudflare.H3.RecordName), RecordType: strings.ToUpper(strings.TrimSpace(r.Settings.Cloudflare.H3.RecordType))}, Enabled: r.Settings.Cloudflare.Enabled, APIToken: current.Cloudflare.APIToken, ZoneID: strings.TrimSpace(r.Settings.Cloudflare.ZoneID), RecordName: strings.TrimSpace(r.Settings.Cloudflare.RecordName), RecordType: strings.ToUpper(r.Settings.Cloudflare.RecordType), TTL: r.Settings.Cloudflare.TTL, Proxied: r.Settings.Cloudflare.Proxied},
		GitHub:     GitHubSettings{H3: GitHubH3Target{Path: strings.TrimSpace(r.Settings.GitHub.H3.Path)}, Enabled: r.Settings.GitHub.Enabled, Token: current.GitHub.Token, Owner: strings.TrimSpace(r.Settings.GitHub.Owner), Repository: strings.TrimSpace(r.Settings.GitHub.Repository), Branch: strings.TrimSpace(r.Settings.GitHub.Branch), Path: strings.TrimSpace(r.Settings.GitHub.Path)},
		Gist:       GistSettings{H3: GistH3Target{GistID: strings.TrimSpace(r.Settings.Gist.H3.GistID), Filename: strings.TrimSpace(r.Settings.Gist.H3.Filename)}, Enabled: r.Settings.Gist.Enabled, Token: current.Gist.Token, GistID: strings.TrimSpace(r.Settings.Gist.GistID), Filename: strings.TrimSpace(r.Settings.Gist.Filename)},
		Telegram: TelegramSettings{
			Enabled: r.Settings.Telegram.Enabled, BotToken: current.Telegram.BotToken, ChatID: strings.TrimSpace(r.Settings.Telegram.ChatID),
			ContentMode: r.Settings.Telegram.ContentMode, DeliveryMode: r.Settings.Telegram.DeliveryMode,
			RelayURL: strings.TrimRight(strings.TrimSpace(r.Settings.Telegram.RelayURL), "/"), RelayKey: current.Telegram.RelayKey,
		},
	}
	if token := strings.TrimSpace(r.CloudflareToken); token != "" {
		next.Cloudflare.APIToken = token
	}
	if token := strings.TrimSpace(r.GitHubToken); token != "" {
		next.GitHub.Token = token
	}
	if token := strings.TrimSpace(r.GistToken); token != "" {
		next.Gist.Token = token
	}
	if token := strings.TrimSpace(r.TelegramBotToken); token != "" {
		next.Telegram.BotToken = token
	}
	if key := strings.TrimSpace(r.TelegramRelayKey); key != "" {
		next.Telegram.RelayKey = key
	}
	return next
}

func (s Settings) Validate() error {
	if err := s.validateH3Targets(); err != nil {
		return err
	}
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
	if s.Cloudflare.Enabled && (s.Cloudflare.APIToken == "" || s.Cloudflare.ZoneID == "") {
		return fmt.Errorf("启用 Cloudflare 前必须填写 Token 和 Zone ID")
	}
	if s.GitHub.Path != "" && (s.GitHub.Path == "." || s.GitHub.Path == ".." || strings.HasPrefix(s.GitHub.Path, "/") || path.Clean(s.GitHub.Path) != s.GitHub.Path || strings.HasPrefix(s.GitHub.Path, "../")) {
		return fmt.Errorf("GitHub 文件路径必须是仓库内的有效相对路径")
	}
	if s.GitHub.Enabled && (s.GitHub.Token == "" || s.GitHub.Owner == "" || s.GitHub.Repository == "" || s.GitHub.Branch == "") {
		return fmt.Errorf("启用 GitHub 前必须填写 Token、仓库和分支")
	}
	if s.Gist.GistID != "" || s.Gist.Filename != "" {
		if strings.TrimSpace(s.Gist.GistID) == "" || strings.ContainsAny(s.Gist.GistID, "/\\?# \t\r\n") {
			return fmt.Errorf("Gist ID 必须为有效 ID，不含地址或路径")
		}
		if strings.TrimSpace(s.Gist.Filename) == "" || s.Gist.Filename == "." || s.Gist.Filename == ".." || strings.ContainsAny(s.Gist.Filename, "/\\\r\n") {
			return fmt.Errorf("Gist 文件名必须是有效文件名，不含路径")
		}
	}
	if s.Gist.Enabled && s.Gist.Token == "" {
		return fmt.Errorf("启用 Gist 前必须填写 Token")
	}
	if s.Telegram.ContentMode != "summary" && s.Telegram.ContentMode != "details" {
		return fmt.Errorf("Telegram 内容模式必须是 summary 或 details")
	}
	if s.Telegram.DeliveryMode != TelegramDeliveryDirect && s.Telegram.DeliveryMode != TelegramDeliveryRelay {
		return fmt.Errorf("Telegram 投递模式必须是 direct 或 relay")
	}
	if s.Telegram.RelayURL != "" {
		parsed, err := url.ParseRequestURI(s.Telegram.RelayURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Path != "/telegram" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("Telegram 中继 URL 必须是以 /telegram 结尾且不含凭据和查询参数的 HTTPS 地址")
		}
	}
	if s.Telegram.Enabled && (s.Telegram.BotToken == "" || s.Telegram.ChatID == "") {
		return fmt.Errorf("启用 Telegram 前必须填写 Bot Token 和 Chat ID")
	}
	if s.Telegram.Enabled && s.Telegram.DeliveryMode == TelegramDeliveryRelay && (s.Telegram.RelayURL == "" || s.Telegram.RelayKey == "") {
		return fmt.Errorf("启用 Telegram 专属中继前必须填写中继 URL 和访问密钥")
	}
	return nil
}

// ForMode fixes destinations at enqueue time without duplicating credentials.
func (s Settings) ForMode(mode model.Mode) Settings {
	if mode == model.ModeH3 {
		s.Cloudflare.RecordName, s.Cloudflare.RecordType = s.Cloudflare.H3.RecordName, s.Cloudflare.H3.RecordType
		if s.Cloudflare.RecordType == "" {
			s.Cloudflare.RecordType = "TXT"
		}
		s.GitHub.Path = s.GitHub.H3.Path
		s.Gist.GistID, s.Gist.Filename = s.Gist.H3.GistID, s.Gist.H3.Filename
	}
	s.Cloudflare.Enabled = s.Cloudflare.Enabled && strings.TrimSpace(s.Cloudflare.RecordName) != ""
	s.GitHub.Enabled = s.GitHub.Enabled && strings.TrimSpace(s.GitHub.Path) != ""
	s.Gist.Enabled = s.Gist.Enabled && strings.TrimSpace(s.Gist.GistID) != "" && strings.TrimSpace(s.Gist.Filename) != ""
	s.Cloudflare.H3 = CloudflareH3Target{RecordType: "TXT"}
	s.GitHub.H3 = GitHubH3Target{}
	s.Gist.H3 = GistH3Target{}
	return s
}

func (s Settings) validateH3Targets() error {
	if h3 := s.Cloudflare.H3; strings.TrimSpace(h3.RecordName) != "" {
		if !config.ValidDomain(normalizeRecordName(h3.RecordName)) {
			return fmt.Errorf("H3 Cloudflare 记录名必须是完整域名，不含协议、端口或路径")
		}
		if h3.RecordType != "A" && h3.RecordType != "TXT" {
			return fmt.Errorf("H3 Cloudflare 记录类型必须是 A 或 TXT")
		}
		if normalizeRecordName(h3.RecordName) == normalizeRecordName(s.Cloudflare.RecordName) {
			return fmt.Errorf("H3 Cloudflare 记录名必须与普通测速不同")
		}
	}
	if h3 := s.GitHub.H3; h3.Path != "" {
		if h3.Path == "" || h3.Path == "." || strings.HasPrefix(h3.Path, "/") || path.Clean(h3.Path) != h3.Path || h3.Path == ".." || strings.HasPrefix(h3.Path, "../") {
			return fmt.Errorf("H3 GitHub 文件路径必须是仓库内的有效相对路径")
		}
		if h3.Path == s.GitHub.Path {
			return fmt.Errorf("H3 GitHub 文件路径必须与普通测速不同")
		}
	}
	if h3 := s.Gist.H3; h3.GistID != "" || h3.Filename != "" {
		if strings.TrimSpace(h3.GistID) == "" || strings.ContainsAny(h3.GistID, "/\\?# \t\r\n") {
			return fmt.Errorf("H3 Gist ID 必须为有效 ID，不含地址或路径")
		}
		if strings.TrimSpace(h3.Filename) == "" || h3.Filename == "." || h3.Filename == ".." || strings.ContainsAny(h3.Filename, "/\\\r\n") {
			return fmt.Errorf("H3 Gist 文件名必须是有效文件名，不含路径")
		}
		if strings.EqualFold(strings.TrimSpace(h3.GistID), strings.TrimSpace(s.Gist.GistID)) && h3.Filename == s.Gist.Filename {
			return fmt.Errorf("H3 Gist 必须使用不同的 Gist ID 或文件名")
		}
	}
	return nil
}

func normalizeRecordName(name string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
}
