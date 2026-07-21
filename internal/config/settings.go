package config

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

type Settings struct {
	TCPConcurrency       int      `json:"tcpConcurrency"`
	HTTPSConcurrency     int      `json:"httpsConcurrency"`
	BandwidthConcurrency int      `json:"bandwidthConcurrency"`
	ConnectTimeoutMS     int      `json:"connectTimeoutMs"`
	RequestTimeoutMS     int      `json:"requestTimeoutMs"`
	BandwidthTimeoutMS   int      `json:"bandwidthTimeoutMs"`
	SourceTimeoutMS      int      `json:"sourceTimeoutMs"`
	SourceRetries        int      `json:"sourceRetries"`
	TCPProbeCount        int      `json:"tcpProbeCount"`
	HTTPSProbeCount      int      `json:"httpsProbeCount"`
	TCPMinSuccessRate    float64  `json:"tcpMinSuccessRate"`
	HTTPSMinSuccessRate  float64  `json:"httpsMinSuccessRate"`
	TCPCandidateCount    int      `json:"tcpCandidateCount"`
	BandwidthCandidates  int      `json:"bandwidthCandidates"`
	FinalResultCount     int      `json:"finalResultCount"`
	MaxDownloadBytes     int64    `json:"maxDownloadBytes"`
	AllowedPorts         []int    `json:"allowedPorts"`
	AllowedCountries     []string `json:"allowedCountries"`
	BlockedCountries     []string `json:"blockedCountries"`
	LegacyProbeCount     int      `json:"probeCount,omitempty"`
}

func DefaultSettings() Settings {
	return Settings{
		TCPConcurrency: 64, HTTPSConcurrency: 16, BandwidthConcurrency: 3,
		ConnectTimeoutMS: 1200, RequestTimeoutMS: 4000, BandwidthTimeoutMS: 12000,
		SourceTimeoutMS: 10000, SourceRetries: 2,
		TCPProbeCount: 3, HTTPSProbeCount: 3,
		TCPMinSuccessRate: 2.0 / 3.0, HTTPSMinSuccessRate: 2.0 / 3.0,
		TCPCandidateCount: 150, BandwidthCandidates: 30,
		FinalResultCount: 15, MaxDownloadBytes: 20 * 1024 * 1024,
		AllowedPorts:     []int{443, 8443, 2053, 2083, 2087, 2096},
		AllowedCountries: []string{}, BlockedCountries: []string{},
	}
}

func (s *Settings) MigrateLegacy() {
	if s.LegacyProbeCount > 0 {
		s.TCPProbeCount = s.LegacyProbeCount
		s.HTTPSProbeCount = s.LegacyProbeCount
		s.LegacyProbeCount = 0
	}
}

func (s Settings) Validate() error {
	checks := []struct {
		name            string
		value, min, max int
	}{
		{"TCP 并发数", s.TCPConcurrency, 1, 256}, {"HTTPS 并发数", s.HTTPSConcurrency, 1, 64},
		{"带宽并发数", s.BandwidthConcurrency, 1, 10}, {"连接超时", s.ConnectTimeoutMS, 100, 30000},
		{"请求超时", s.RequestTimeoutMS, 500, 60000}, {"带宽超时", s.BandwidthTimeoutMS, 1000, 120000},
		{"数据源超时", s.SourceTimeoutMS, 500, 60000}, {"数据源重试次数", s.SourceRetries, 0, 3},
		{"TCP 探测次数", s.TCPProbeCount, 1, 10}, {"HTTPS 探测次数", s.HTTPSProbeCount, 1, 10},
		{"TCP 候选数", s.TCPCandidateCount, 1, 5000},
		{"带宽候选数", s.BandwidthCandidates, 1, 500}, {"最终结果数", s.FinalResultCount, 1, 100},
	}
	for _, check := range checks {
		if check.value < check.min || check.value > check.max {
			return fmt.Errorf("%s必须在 %d 到 %d 之间", check.name, check.min, check.max)
		}
	}
	if s.MaxDownloadBytes < 64*1024 || s.MaxDownloadBytes > 1024*1024*1024 {
		return fmt.Errorf("最大下载字节数必须在 64 KiB 到 1 GiB 之间")
	}
	if s.TCPMinSuccessRate < 0.6 || s.TCPMinSuccessRate > 1 {
		return fmt.Errorf("TCP 最低成功率必须在 60%% 到 100%% 之间")
	}
	if s.HTTPSMinSuccessRate < 0.6 || s.HTTPSMinSuccessRate > 1 {
		return fmt.Errorf("HTTPS 最低成功率必须在 60%% 到 100%% 之间")
	}
	if s.BandwidthCandidates > s.TCPCandidateCount {
		return fmt.Errorf("带宽候选数不能大于 TCP 候选数")
	}
	if s.FinalResultCount > s.BandwidthCandidates {
		return fmt.Errorf("最终结果数不能大于带宽候选数")
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
	return time.Duration(s.ConnectTimeoutMS) * time.Millisecond
}
func (s Settings) RequestTimeout() time.Duration {
	return time.Duration(s.RequestTimeoutMS) * time.Millisecond
}
func (s Settings) BandwidthTimeout() time.Duration {
	return time.Duration(s.BandwidthTimeoutMS) * time.Millisecond
}
func (s Settings) SourceTimeout() time.Duration {
	return time.Duration(s.SourceTimeoutMS) * time.Millisecond
}
