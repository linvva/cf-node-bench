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
	fields := OutputFields{Country: true, TCPP95: true, HTTPLatency: true, Bandwidth: true}
	if got := FormatResult(result, fields); got != "104.18.1.20:8443#CN|TCP22ms|HTTP44.3ms|186Mbps" {
		t.Fatalf("format=%q", got)
	}
	result.Candidate.Country = ""
	if got := FormatResult(result, OutputFields{}); got != "104.18.1.20:8443" {
		t.Fatalf("base format=%q", got)
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
	if len(contents) != 3 || eligible != 4 || skipped != 0 || contents[1] != "1.1.1.2:8443#CN|HTTP44.3ms|186Mbps" {
		t.Fatalf("TXT contents=%v eligible=%d skipped=%d", contents, eligible, skipped)
	}
}
