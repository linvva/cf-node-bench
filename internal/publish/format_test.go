package publish

import (
	"testing"

	"github.com/linvva/cf-node-bench/internal/model"
)

func sampleResult(ip string, port int, country string) model.ProbeResult {
	return model.ProbeResult{
		Candidate: model.Candidate{AddressType: model.AddressIPv4, IP: ip, Port: port, Country: country},
		TCP:       model.ProbeStats{P95MS: 22}, HTTPS: model.ProbeStats{AverageMS: 44.25},
		Bandwidth: model.BandwidthStats{Mbps: 186},
	}
}

func TestFormatResultFields(t *testing.T) {
	result := sampleResult("104.18.1.20", 8443, "CN")
	for _, test := range []struct {
		name   string
		fields OutputFields
		want   string
	}{
		{"detailed", OutputFields{Format: OutputFormatDetailed, Country: true, TCPP95: true, HTTPLatency: true, Bandwidth: true}, "104.18.1.20:8443#CN|TCP22ms|HTTP44.3ms|186Mbps"},
		{"compact default", DefaultSettings().Output, "104.18.1.20:8443#CN|44ms|186M"},
		{"compact all fields", OutputFields{Country: true, TCPP95: true, HTTPLatency: true, Bandwidth: true}, "104.18.1.20:8443#CN|TCP22ms|44ms|186M"},
		{"minimal", OutputFields{Country: true}, "104.18.1.20:8443#CN"},
		{"without country", OutputFields{HTTPLatency: true, Bandwidth: true}, "104.18.1.20:8443#44ms|186M"},
		{"no fields", OutputFields{}, "104.18.1.20:8443"},
		{"detailed without country", OutputFields{Format: OutputFormatDetailed, HTTPLatency: true}, "104.18.1.20:8443|HTTP44.3ms"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := FormatResult(result, test.fields); got != test.want {
				t.Fatalf("format=%q, want %q", got, test.want)
			}
		})
	}
	result.Candidate.Country = ""
	result.TCP.P95MS, result.HTTPS.AverageMS, result.Bandwidth.Mbps = 22.5, 44.5, 186.5
	if got := FormatResult(result, OutputFields{Country: true, TCPP95: true, HTTPLatency: true, Bandwidth: true}); got != "104.18.1.20:8443#TCP23ms|45ms|187M" {
		t.Fatalf("compact rounding or unknown country=%q", got)
	}
}

func TestCloudflareContentsAAndTXT(t *testing.T) {
	results := []model.ProbeResult{
		sampleResult("1.1.1.1", 443, "US"), sampleResult("1.1.1.1", 443, "US"), sampleResult("1.1.1.2", 8443, "CN"), sampleResult("not-an-ip", 443, "US"),
	}
	settings := DefaultSettings()
	contents, eligible, skipped := CloudflareContents(results, settings)
	if len(contents) != 1 || contents[0] != "1.1.1.1" || eligible != 2 || skipped != 2 {
		t.Fatalf("A contents=%v eligible=%d skipped=%d", contents, eligible, skipped)
	}
	settings.Cloudflare.RecordType = "TXT"
	contents, eligible, skipped = CloudflareContents(results, settings)
	if len(contents) != 3 || eligible != 4 || skipped != 0 || contents[1] != "1.1.1.2:8443#CN|44ms|186M" {
		t.Fatalf("TXT contents=%v eligible=%d skipped=%d", contents, eligible, skipped)
	}
}

func TestH3FormatPreservesDomainAndUsesOnlyH3Metrics(t *testing.T) {
	result := sampleResult("1.1.1.1", 8443, "HK")
	result.Mode = model.ModeH3
	result.Candidate.AddressType = model.AddressDomain
	result.Candidate.Host = "nodes.example.com"
	result.QUIC = model.ProbeStats{P95MS: 17.5}
	result.HTTP3 = model.ProbeStats{AverageMS: 31.2}
	fields := OutputFields{Format: OutputFormatDetailed, Country: true, TCPP95: true, HTTPLatency: true, Bandwidth: true}
	if got := FormatResult(result, fields); got != "nodes.example.com:8443#HK|H3|QUIC17.5ms|HTTP331.2ms|186Mbps" {
		t.Fatalf("H3 format = %q", got)
	}
	if got := FormatResult(result, OutputFields{}); got != "nodes.example.com:8443#H3" {
		t.Fatalf("H3 base format = %q", got)
	}
	fields.Format = OutputFormatCompact
	if got := FormatResult(result, fields); got != "nodes.example.com:8443#HK|H3|QUIC18ms|31ms|186M" {
		t.Fatalf("compact H3 format = %q", got)
	}
	if got := FormatResult(result, OutputFields{Country: true}); got != "nodes.example.com:8443#HK|H3" {
		t.Fatalf("minimal H3 format = %q", got)
	}
	settings := DefaultSettings()
	settings.Cloudflare.RecordType = "TXT"
	contents, eligible, skipped := CloudflareContents([]model.ProbeResult{result}, settings)
	if len(contents) != 1 || contents[0] != "nodes.example.com:8443#HK|H3|31ms|186M" || eligible != 1 || skipped != 0 {
		t.Fatalf("H3 TXT = %v eligible=%d skipped=%d", contents, eligible, skipped)
	}
	result.Candidate.Port = 443
	settings.Cloudflare.RecordType = "A"
	contents, eligible, skipped = CloudflareContents([]model.ProbeResult{result}, settings)
	if len(contents) != 0 || eligible != 0 || skipped != 1 {
		t.Fatalf("domain was expanded into A records: %v eligible=%d skipped=%d", contents, eligible, skipped)
	}
}
