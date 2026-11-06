package config

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/linvva/cf-node-bench/internal/model"
)

const DefaultBandwidthTestURL = "https://speed.cloudflare.com/__down?bytes=99999999"
const DefaultH3BandwidthTestURL = "https://h3.speed.cloudflare.com/__down?bytes=99999999"

type Settings struct {
	RequireH3                bool               `json:"requireH3"`
	VerificationHost         string             `json:"verificationHost"`
	H3ConnectTimeoutMS       int                `json:"h3ConnectTimeoutMs"`
	H3RequestTimeoutMS       int                `json:"h3RequestTimeoutMs"`
	H3BandwidthTestURL       string             `json:"h3BandwidthTestUrl"`
	ConnectionConcurrency    int                `json:"connectionConcurrency"`
	HTTPConcurrency          int                `json:"httpConcurrency"`
	BandwidthConcurrency     int                `json:"bandwidthConcurrency"`
	ConnectTimeoutMS         int                `json:"connectTimeoutMs"`
	RequestTimeoutMS         int                `json:"requestTimeoutMs"`
	BandwidthTimeoutMS       int                `json:"bandwidthTimeoutMs"`
	SourceTimeoutMS          int                `json:"sourceTimeoutMs"`
	SourceRetries            int                `json:"sourceRetries"`
	ConnectionProbeCount     int                `json:"connectionProbeCount"`
	HTTPProbeCount           int                `json:"httpProbeCount"`
	ConnectionMinSuccessRate float64            `json:"connectionMinSuccessRate"`
	HTTPMinSuccessRate       float64            `json:"httpMinSuccessRate"`
	HTTPCandidateCount       int                `json:"httpCandidateCount"`
	BandwidthCandidates      int                `json:"bandwidthCandidates"`
	FinalResultCount         int                `json:"finalResultCount"`
	MaxDownloadBytes         int64              `json:"maxDownloadBytes"`
	BandwidthTestURL         string             `json:"bandwidthTestUrl"`
	RetainPreviousResults    bool               `json:"retainPreviousResults"`
	ScoreWeights             model.ScoreWeights `json:"scoreWeights"`
	AllowedPorts             []int              `json:"allowedPorts"`
	AllowedCountries         []string           `json:"allowedCountries"`
	BlockedCountries         []string           `json:"blockedCountries"`
	LegacyProbeCount         int                `json:"probeCount,omitempty"`
}

func DefaultSettings() Settings {
	return Settings{
		H3ConnectTimeoutMS: 3000, H3RequestTimeoutMS: 6000, H3BandwidthTestURL: DefaultH3BandwidthTestURL,
		ConnectionConcurrency: 64, HTTPConcurrency: 16, BandwidthConcurrency: 3,
		ConnectTimeoutMS: 1200, RequestTimeoutMS: 4000, BandwidthTimeoutMS: 12000,
		SourceTimeoutMS: 10000, SourceRetries: 2,
		ConnectionProbeCount: 3, HTTPProbeCount: 3,
		ConnectionMinSuccessRate: 2.0 / 3.0, HTTPMinSuccessRate: 2.0 / 3.0,
		HTTPCandidateCount: 150, BandwidthCandidates: 30,
		FinalResultCount: 15, MaxDownloadBytes: 20 * 1024 * 1024,
		BandwidthTestURL:      DefaultBandwidthTestURL,
		RetainPreviousResults: true,
		ScoreWeights:          DefaultScoreWeights(),
		AllowedPorts:          []int{443, 8443, 2053, 2083, 2087, 2096},
		AllowedCountries:      []string{}, BlockedCountries: []string{},
	}
}

func DefaultH3Settings() Settings {
	s := DefaultSettings()
	s.RequireH3 = true
	return s
}

func DefaultScoreWeights() model.ScoreWeights {
	return model.ScoreWeights{Bandwidth: 40, HTTP: 25, Connection: 15, Reliability: 15, Jitter: 5}
}

func LegacyScoreWeights() model.ScoreWeights {
	return model.ScoreWeights{TCP: 20, HTTPS: 20, Jitter: 10, Reliability: 30, Bandwidth: 20}
}

func (s *Settings) MigrateLegacy() {
	if s.LegacyProbeCount > 0 {
		s.ConnectionProbeCount = s.LegacyProbeCount
		s.HTTPProbeCount = s.LegacyProbeCount
		s.LegacyProbeCount = 0
	}
	if strings.TrimSpace(s.BandwidthTestURL) == "" {
		s.BandwidthTestURL = DefaultBandwidthTestURL
	}
	if s.ScoreWeights.Connection == 0 && s.ScoreWeights.HTTP == 0 {
		if s.RequireH3 && (s.ScoreWeights.QUIC != 0 || s.ScoreWeights.HTTP3 != 0) {
			s.ScoreWeights.Connection, s.ScoreWeights.HTTP = s.ScoreWeights.QUIC, s.ScoreWeights.HTTP3
		} else {
			s.ScoreWeights.Connection, s.ScoreWeights.HTTP = s.ScoreWeights.TCP, s.ScoreWeights.HTTPS
		}
	}
	s.ScoreWeights.TCP, s.ScoreWeights.HTTPS, s.ScoreWeights.QUIC, s.ScoreWeights.HTTP3 = 0, 0, 0, 0
	if strings.TrimSpace(s.H3BandwidthTestURL) == "" {
		s.H3BandwidthTestURL = DefaultH3BandwidthTestURL
	}
	if scoreWeightTotal(s.ScoreWeights) == 0 {
		s.ScoreWeights = DefaultScoreWeights()
	}
}

func (s *Settings) Normalize() {
	s.VerificationHost = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(s.VerificationHost), "."))
	s.H3BandwidthTestURL = strings.TrimSpace(s.H3BandwidthTestURL)
	s.BandwidthTestURL = strings.TrimSpace(s.BandwidthTestURL)
}

func (s Settings) Validate() error {
	if s.VerificationHost != "" && !ValidDomain(s.VerificationHost) {
		return fmt.Errorf("H3 验证域名必须为有效域名，不含协议、端口或路径")
	}
	if s.ScoreWeights.TCP != 0 || s.ScoreWeights.HTTPS != 0 || s.ScoreWeights.QUIC != 0 || s.ScoreWeights.HTTP3 != 0 {
		return fmt.Errorf("请使用共享的连接和请求评分权重")
	}
	if err := validateBandwidthTestURL(s.H3BandwidthTestURL); err != nil {
		return fmt.Errorf("H3：%w", err)
	}
	checks := []struct {
		name            string
		value, min, max int
	}{
		{"连接探测并发数", s.ConnectionConcurrency, 1, 256}, {"HTTP 请求并发数", s.HTTPConcurrency, 1, 64},
		{"带宽并发数", s.BandwidthConcurrency, 1, 10}, {"连接超时", s.ConnectTimeoutMS, 100, 30000},
		{"QUIC 握手超时", s.H3ConnectTimeoutMS, 100, 30000}, {"HTTP/3 请求超时", s.H3RequestTimeoutMS, 500, 60000},
		{"请求超时", s.RequestTimeoutMS, 500, 60000}, {"带宽超时", s.BandwidthTimeoutMS, 1000, 120000},
		{"数据源超时", s.SourceTimeoutMS, 500, 60000}, {"数据源重试次数", s.SourceRetries, 0, 3},
		{"连接探测次数", s.ConnectionProbeCount, 1, 10}, {"HTTP 请求次数", s.HTTPProbeCount, 1, 10},
		{"HTTP 候选数", s.HTTPCandidateCount, 1, 5000},
		{"带宽目标通过数", s.BandwidthCandidates, 1, 500}, {"最终结果数", s.FinalResultCount, 1, 100},
	}
	for _, check := range checks {
		if check.value < check.min || check.value > check.max {
			return fmt.Errorf("%s必须在 %d 到 %d 之间", check.name, check.min, check.max)
		}
	}
	if s.MaxDownloadBytes < 64*1024 || s.MaxDownloadBytes > 1024*1024*1024 {
		return fmt.Errorf("最大下载字节数必须在 64 KiB 到 1 GiB 之间")
	}
	if err := validateBandwidthTestURL(s.BandwidthTestURL); err != nil {
		return err
	}
	if s.ConnectionMinSuccessRate < 0.6 || s.ConnectionMinSuccessRate > 1 {
		return fmt.Errorf("连接最低成功率必须在 60%% 到 100%% 之间")
	}
	if s.HTTPMinSuccessRate < 0.6 || s.HTTPMinSuccessRate > 1 {
		return fmt.Errorf("HTTP 最低成功率必须在 60%% 到 100%% 之间")
	}
	if s.BandwidthCandidates > s.HTTPCandidateCount {
		return fmt.Errorf("带宽目标通过数不能大于 HTTP 候选数")
	}
	if s.FinalResultCount > s.BandwidthCandidates {
		return fmt.Errorf("最终结果数不能大于带宽目标通过数")
	}
	weights := []struct {
		name  string
		value int
	}{{"连接", s.ScoreWeights.Connection}, {"HTTP 请求", s.ScoreWeights.HTTP}, {"抖动", s.ScoreWeights.Jitter}, {"可用性", s.ScoreWeights.Reliability}, {"带宽", s.ScoreWeights.Bandwidth}}
	for _, weight := range weights {
		if weight.value < 0 || weight.value > 100 {
			return fmt.Errorf("%s 权重必须在 0 到 100 之间", weight.name)
		}
	}
	if scoreWeightTotal(s.ScoreWeights) != 100 {
		return fmt.Errorf("评分权重总和必须等于 100")
	}
	for _, port := range s.AllowedPorts {
		if port < 1 || port > 65535 {
			return fmt.Errorf("端口必须在 1 到 65535 之间")
		}
	}
	if err := validateCountries("允许国家", s.AllowedCountries); err != nil {
		return err
	}
	if err := validateCountries("排除国家", s.BlockedCountries); err != nil {
		return err
	}
	for _, allowed := range s.AllowedCountries {
		if slices.ContainsFunc(s.BlockedCountries, func(blocked string) bool { return strings.EqualFold(allowed, blocked) }) {
			return fmt.Errorf("国家 %s 不能同时出现在允许和排除列表", strings.ToUpper(allowed))
		}
	}
	return nil
}

func ValidDomain(host string) bool {
	if len(host) == 0 || len(host) > 253 || !strings.Contains(host, ".") || net.ParseIP(host) != nil {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-') {
				return false
			}
		}
	}
	return true
}

func scoreWeightTotal(weights model.ScoreWeights) int {
	return weights.Connection + weights.HTTP + weights.TCP + weights.HTTPS + weights.QUIC + weights.HTTP3 + weights.Jitter + weights.Reliability + weights.Bandwidth
}

func validateBandwidthTestURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("下载测速地址不能为空")
	}
	target, err := url.Parse(raw)
	if err != nil || target.Scheme != "https" || target.Host == "" || target.Hostname() == "" || target.Opaque != "" {
		return fmt.Errorf("下载测速地址必须是完整的 HTTPS URL")
	}
	if target.User != nil {
		return fmt.Errorf("下载测速地址不能包含用户名或密码")
	}
	if target.Fragment != "" || target.RawFragment != "" {
		return fmt.Errorf("下载测速地址不能包含片段")
	}
	return nil
}

func validateCountries(name string, countries []string) error {
	for _, country := range countries {
		country = strings.TrimSpace(country)
		if len(country) != 2 || country[0] < 'A' || country[0] > 'Z' || country[1] < 'A' || country[1] > 'Z' {
			return fmt.Errorf("%s必须使用两个大写字母的国家代码", name)
		}
	}
	return nil
}

func (s Settings) AllowsPort(port int) bool {
	return len(s.AllowedPorts) == 0 || slices.Contains(s.AllowedPorts, port)
}

func (s Settings) AllowsCountry(country string) bool {
	if slices.ContainsFunc(s.BlockedCountries, func(value string) bool {
		return strings.EqualFold(value, country)
	}) {
		return false
	}
	if len(s.AllowedCountries) == 0 {
		return true
	}
	return slices.ContainsFunc(s.AllowedCountries, func(value string) bool {
		return strings.EqualFold(value, country)
	})
}

func (s Settings) ConnectTimeout() time.Duration {
	if s.RequireH3 {
		return time.Duration(s.H3ConnectTimeoutMS) * time.Millisecond
	}
	return time.Duration(s.ConnectTimeoutMS) * time.Millisecond
}
func (s Settings) RequestTimeout() time.Duration {
	if s.RequireH3 {
		return time.Duration(s.H3RequestTimeoutMS) * time.Millisecond
	}
	return time.Duration(s.RequestTimeoutMS) * time.Millisecond
}
func (s Settings) BandwidthTimeout() time.Duration {
	return time.Duration(s.BandwidthTimeoutMS) * time.Millisecond
}
func (s Settings) SourceTimeout() time.Duration {
	return time.Duration(s.SourceTimeoutMS) * time.Millisecond
}

func (s Settings) ProbeMode() model.Mode {
	if s.RequireH3 {
		return model.ModeH3
	}
	return model.ModeTCPHTTPS
}
func (s Settings) EffectiveVerificationHost() string {
	if s.VerificationHost == "" {
		return "h3.speed.cloudflare.com"
	}
	return s.VerificationHost
}
func (s Settings) EffectiveBandwidthTestURL() string {
	if s.RequireH3 {
		return s.H3BandwidthTestURL
	}
	return s.BandwidthTestURL
}

// Decode previous settings/checkpoints without retaining two writable profiles.
func (s *Settings) UnmarshalJSON(content []byte) error {
	type plain Settings
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil {
		return err
	}
	next := plain(DefaultSettings())
	if _, ok := fields["scoreWeights"]; ok {
		next.ScoreWeights = model.ScoreWeights{}
	}
	if err := json.Unmarshal(content, &next); err != nil {
		return err
	}
	var legacyMode model.Mode
	if value, ok := fields["mode"]; ok {
		if err := json.Unmarshal(value, &legacyMode); err != nil {
			return err
		}
	}
	if _, ok := fields["requireH3"]; !ok {
		next.RequireH3 = legacyMode == model.ModeH3
	}
	mappings := [][3]string{
		{"connectionConcurrency", "tcpConcurrency", "quicConcurrency"}, {"httpConcurrency", "httpsConcurrency", "http3Concurrency"},
		{"connectionProbeCount", "tcpProbeCount", "quicProbeCount"}, {"httpProbeCount", "httpsProbeCount", "http3ProbeCount"},
		{"connectionMinSuccessRate", "tcpMinSuccessRate", "quicMinSuccessRate"}, {"httpMinSuccessRate", "httpsMinSuccessRate", "http3MinSuccessRate"},
		{"httpCandidateCount", "tcpCandidateCount", "h3CandidateCount"}, {"verificationHost", "workerHost", "workerHost"},
	}
	if legacyMode == model.ModeH3 {
		mappings = append(mappings, [3]string{"h3ConnectTimeoutMs", "connectTimeoutMs", "connectTimeoutMs"}, [3]string{"h3RequestTimeoutMs", "requestTimeoutMs", "requestTimeoutMs"}, [3]string{"h3BandwidthTestUrl", "bandwidthTestUrl", "bandwidthTestUrl"})
	}
	patch := map[string]json.RawMessage{}
	for _, mapping := range mappings {
		if _, exists := fields[mapping[0]]; exists {
			continue
		}
		key := mapping[1]
		if legacyMode == model.ModeH3 {
			key = mapping[2]
		}
		if value, ok := fields[key]; ok {
			patch[mapping[0]] = value
		}
	}
	data, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &next); err != nil {
		return err
	}
	*s = Settings(next)
	if _, current := fields["requireH3"]; !current {
		s.MigrateLegacy()
	}
	s.Normalize()
	return nil
}
