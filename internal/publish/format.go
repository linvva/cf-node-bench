package publish

import (
	"math"
	"net"
	"strconv"
	"strings"

	"github.com/linvva/cf-node-bench/internal/model"
)

func FormatResult(result model.ProbeResult, fields OutputFields) string {
	base := result.Candidate.Key()
	if fields.Country && result.Candidate.Country != "" {
		base += "#" + result.Candidate.Country
	}
	parts := []string{base}
	if fields.TCPP95 {
		parts = append(parts, "TCP"+compact(result.TCP.P95MS)+"ms")
	}
	if fields.HTTPLatency {
		parts = append(parts, "HTTP"+compact(result.HTTPS.AverageMS)+"ms")
	}
	if fields.Bandwidth {
		parts = append(parts, compact(result.Bandwidth.Mbps)+"Mbps")
	}
	return strings.Join(parts, "|")
}

func FormatResults(results []model.ProbeResult, fields OutputFields) string {
	lines := make([]string, len(results))
	for index, result := range results {
		lines[index] = FormatResult(result, fields)
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func CloudflareContents(results []model.ProbeResult, settings Settings) (contents []string, eligible, skipped int) {
	seen := map[string]bool{}
	for _, result := range results {
		content := ""
		if settings.Cloudflare.RecordType == "A" {
			if result.Candidate.Port != 443 || result.Candidate.AddressType != model.AddressIPv4 || net.ParseIP(result.Candidate.IP).To4() == nil {
				skipped++
				continue
			}
			content = result.Candidate.IP
		} else {
			content = FormatResult(result, settings.Output)
		}
		eligible++
		if !seen[content] {
			seen[content] = true
			contents = append(contents, content)
		}
	}
	return contents, eligible, skipped
}

func compact(value float64) string {
	rounded := math.Round((value+1e-9)*10) / 10
	return strings.TrimSuffix(strconv.FormatFloat(rounded, 'f', 1, 64), ".0")
}
