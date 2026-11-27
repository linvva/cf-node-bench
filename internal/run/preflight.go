package run

import (
	"context"
	"net"
	"net/netip"
	"net/url"
	"time"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
)

const DownloadCheckBytes = 64 * 1024

func TestDownload(ctx context.Context, settings config.Settings, candidates []model.Candidate, resolver DNSResolver, prober BandwidthProbe) model.DownloadCheck {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	target, _ := url.Parse(settings.EffectiveBandwidthTestURL())
	report := model.DownloadCheck{Failures: map[model.FailureReason]int{}}
	if target != nil {
		report.Host = target.Hostname()
	}
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	physical := make([]model.Candidate, 0, 5)
	seen := map[string]bool{}
	var domains []model.Candidate
	add := func(ip string, port int) {
		candidate := model.Candidate{AddressType: model.AddressIPv4, IP: ip, Port: port}
		if !seen[candidate.Key()] && len(physical) < 5 {
			physical = append(physical, candidate)
			seen[candidate.Key()] = true
		}
	}
	for _, candidate := range candidates {
		if len(physical) == 5 {
			break
		}
		if candidate.AddressType == model.AddressDomain {
			if settings.RequireH3 && len(domains) < 5 && !seen[candidate.Key()] {
				domains = append(domains, candidate)
				seen[candidate.Key()] = true
			}
		} else if ip, err := netip.ParseAddr(candidate.IP); err == nil && ip.Is4() {
			add(ip.String(), candidate.Port)
		}
	}
	resolved := parallel(ctx, domains[:min(len(domains), 5-len(physical))], 3, func(ctx context.Context, candidate model.Candidate) []model.Candidate {
		addresses, err := resolver.LookupNetIP(ctx, "ip4", candidate.Host)
		if err != nil {
			return nil
		}
		var endpoints []model.Candidate
		for _, address := range addresses {
			if ip := address.Unmap(); ip.Is4() {
				endpoints = append(endpoints, model.Candidate{IP: ip.String(), Port: candidate.Port})
			}
		}
		return endpoints
	}, nil)
	for _, endpoints := range resolved {
		if len(physical)+report.Failures[model.FailureDNS] == 5 {
			break
		}
		if len(endpoints) == 0 {
			report.Failures[model.FailureDNS]++
		}
		for _, endpoint := range endpoints {
			if len(physical)+report.Failures[model.FailureDNS] >= 5 {
				break
			}
			add(endpoint.IP, endpoint.Port)
		}
	}
	report.Sampled = report.Failures[model.FailureDNS]
	for _, stats := range parallel(ctx, physical, 5, prober.Probe, nil) {
		report.Sampled++
		if stats.Failure == "" && stats.Bytes >= DownloadCheckBytes {
			report.Passed++
			continue
		}
		reason := stats.Failure
		if reason == "" {
			reason = model.FailureDownload
		}
		report.Failures[reason]++
	}
	return report
}

func (p stagePipeline) checkDownloads(candidates []model.Candidate) {
	target := p.settings.EffectiveBandwidthTestURL()
	defaultURL := config.DefaultBandwidthTestURL
	if p.settings.RequireH3 {
		defaultURL = config.DefaultH3BandwidthTestURL
	}
	if p.dependencies.DownloadCheck == nil || target == defaultURL || p.ctx.Err() != nil {
		return
	}
	report := TestDownload(p.ctx, p.settings, candidates, p.dependencies.Resolver, p.dependencies.DownloadCheck)
	p.progress.mu.Lock()
	p.progress.value.DownloadCheck = &report
	p.progress.mu.Unlock()
	p.progress.send()
}

func (p stagePipeline) checkResultDownloads(results []model.ProbeResult) {
	candidates := make([]model.Candidate, 0, len(results))
	for _, result := range results {
		if p.settings.RequireH3 && result.Candidate.AddressType == model.AddressDomain {
			for _, endpoint := range result.Endpoints {
				candidates = append(candidates, model.Candidate{IP: endpoint.IP, Port: result.Candidate.Port})
			}
		} else {
			candidates = append(candidates, result.Candidate)
		}
	}
	p.checkDownloads(candidates)
}
