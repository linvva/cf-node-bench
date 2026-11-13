package run

import (
	"context"
	"math"
	"net"
	"net/netip"
	"slices"
	"sync"
	"time"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
	"github.com/linvva/cf-node-bench/internal/ranking"
	"github.com/linvva/cf-node-bench/internal/source"
)

type DNSResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type h3Pipeline struct {
	stagePipeline
	cacheMu  sync.Mutex
	cache    map[string]*h3StageCache
	retained map[string]bool
}

type h3StageCache struct {
	mu      sync.Mutex
	results map[string]*h3Measurement
	slots   chan struct{}
}

type h3Measurement struct {
	done     chan struct{}
	endpoint model.EndpointResult
}

func (e Engine) runH3(ctx context.Context, id string, settings config.Settings, input RunInput, emit func(model.RunProgress)) model.RunSummary {
	started := time.Now()
	progress := newProgress(id, started, emit)
	progress.mu.Lock()
	progress.value.Mode = model.ModeH3
	progress.mu.Unlock()
	p := h3Pipeline{stagePipeline: stagePipeline{ctx: ctx, dependencies: e.Dependencies, settings: settings, progress: progress}, cache: map[string]*h3StageCache{}, retained: map[string]bool{}}
	stage := input.Plan.StartStage
	if stage == "" || input.Plan.Base == nil {
		stage = StageSource
	}
	checkpoint := Checkpoint{Version: CheckpointVersion, RunID: id, Settings: settings, Network: input.Plan.Network, Sources: input.Sources, CompletedAt: map[string]time.Time{}, StageSettings: map[string]config.Settings{}, StageNetworks: map[string]NetworkSnapshot{}}
	checkpoint.LocalFiles = input.LocalFiles != nil
	order := []string{"source", "filter", "dns", StageQUIC, StageHTTP3, StageBandwidth, StageRanking}
	reuseUntil := slices.Index(order, stage)
	if stage == StageQUIC {
		reuseUntil = 1
	} // DNS must be refreshed for a new QUIC measurement.
	base := input.Plan.Base
	if base != nil && stage != StageSource {
		checkpoint.LocalFiles = base.LocalFiles
	}
	progress.mu.Lock()
	progress.value.LocalFiles = checkpoint.LocalFiles
	progress.mu.Unlock()
	if base != nil && stage != StageSource {
		checkpoint.ParentRunID = base.RunID
		checkpoint.Sources = append([]source.HTTPSource(nil), base.Sources...)
		checkpoint.Candidates = append([]model.Candidate(nil), base.Candidates...)
		checkpoint.RetainedKeys = append([]string(nil), base.RetainedKeys...)
		for _, name := range order[:max(0, reuseUntil)] {
			if saved, ok := base.Stage(name); ok {
				progress.reuse(saved, base.CompletedAt[name])
				checkpoint.CompletedAt[name] = base.CompletedAt[name]
				checkpoint.StageSettings[name] = stageSettings(*base, name)
				checkpoint.StageNetworks[name] = base.StageNetworks[name]
			}
		}
		if reuseUntil > slices.Index(order, StageQUIC) {
			checkpoint.QUICResults = cloneH3Results(base.QUICResults)
		}
		if reuseUntil > slices.Index(order, StageHTTP3) {
			checkpoint.HTTP3Results = cloneH3Results(base.HTTP3Results)
			checkpoint.HTTPSelectionComplete = base.HTTPSelectionComplete
		}
		if stage == StageRanking {
			checkpoint.BandwidthResults = cloneH3Results(base.BandwidthResults)
		}
	}
	persist := func() {
		checkpoint.Stages = progress.snapshot()
		checkpoint.UpdatedAt = time.Now()
		if input.Plan.Save != nil {
			if err := input.Plan.Save(checkpoint); err != nil {
				progress.setCheckpointWarning("无法保存阶段检查点：" + err.Error())
			}
		}
	}
	save := func(name string) {
		checkpoint.CompletedAt[name] = time.Now()
		checkpoint.StageSettings[name] = settings
		checkpoint.StageNetworks[name] = input.Plan.Network
		persist()
	}
	if base != nil && stage != StageSource {
		// Bind reused complete stages to this run before a resumed stage can be cancelled.
		persist()
	}
	finish := func(results []model.ProbeResult, state string) model.RunSummary {
		summary := progress.summary(started, results, state)
		summary.Mode, summary.WorkerHost = model.ModeH3, settings.EffectiveVerificationHost()
		summary.ScoreWeights = settings.ScoreWeights
		summary.StartStage, summary.ParentRunID = stage, checkpoint.ParentRunID
		for _, s := range progress.snapshot() {
			if s.State == "reused" {
				summary.ReusedStages = append(summary.ReusedStages, s.Name)
			}
		}
		progress.mu.Lock()
		summary.LogicalCandidates, summary.PhysicalCandidates, summary.DownloadedBytes = progress.value.LogicalCandidates, progress.value.PhysicalCandidates, progress.value.DownloadedBytes
		progress.mu.Unlock()
		return summary
	}
	if stage == StageSource {
		candidates, failures, cancelled := p.source(input.Sources, input.LocalFiles)
		if cancelled {
			return finish(nil, "cancelled")
		}
		fresh := map[string]model.Candidate{}
		for _, c := range candidates {
			fresh[c.Key()] = c
		}
		for _, r := range input.Retained {
			c := r.Candidate
			if updated, ok := fresh[c.Key()]; ok {
				c = updated
			}
			candidates = append(candidates, c)
			checkpoint.RetainedKeys = append(checkpoint.RetainedKeys, c.Key())
		}
		checkpoint.Candidates = unique(candidates)
		save("source")
		p.progress.addFailures(failures)
	}
	for _, key := range checkpoint.RetainedKeys {
		p.retained[key] = true
	}
	var groups []model.ProbeResult
	if stage == StageSource || stage == StageQUIC {
		candidates := p.filter(append([]model.Candidate(nil), checkpoint.Candidates...), nil)
		save("filter")
		groups = p.resolve(candidates)
		if ctx.Err() != nil {
			return finish(nil, "cancelled")
		}
		save("dns")
		progress.start(StageQUIC, 0)
		groups = p.probeBatch(StageQUIC, groups)
		if ctx.Err() != nil {
			return finish(nil, "cancelled")
		}
		p.finishStage(StageQUIC, groups)
		checkpoint.QUICResults = cloneH3Results(groups)
		save(StageQUIC)
	} else {
		groups = cloneH3Results(checkpoint.QUICResults)
		p.setCounts(groups)
	}
	if stage == StageSource || stage == StageQUIC || stage == StageHTTP3 {
		groups = p.eligible(groups, StageQUIC)
		slices.SortStableFunc(groups, func(a, b model.ProbeResult) int { return compareNumber(a.QUIC.P95MS, b.QUIC.P95MS) })
		groups = p.selectStage(StageHTTP3, groups, settings.HTTPCandidateCount)
		if ctx.Err() != nil {
			return finish(nil, "cancelled")
		}
		checkpoint.HTTP3Results = cloneH3Results(groups)
		checkpoint.HTTPSelectionComplete = true
		save(StageHTTP3)
	} else {
		groups = cloneH3Results(checkpoint.HTTP3Results)
	}
	if stage != StageRanking {
		groups = p.httpCandidates(groups)
		groups = p.selectStage(StageBandwidth, groups, settings.BandwidthCandidates)
		if ctx.Err() != nil {
			return finish(nil, "cancelled")
		}
		checkpoint.BandwidthResults = cloneH3Results(groups)
		save(StageBandwidth)
	} else {
		groups = cloneH3Results(checkpoint.BandwidthResults)
		p.setCounts(groups)
	}
	progress.start(StageRanking, len(groups))
	if ctx.Err() != nil {
		return finish(nil, "cancelled")
	}
	groups = p.eligible(groups, StageBandwidth)
	results := ranking.ScoreH3(groups, settings.ConnectionMinSuccessRate, settings.HTTPMinSuccessRate, settings.ScoreWeights)
	results = results[:min(len(results), settings.FinalResultCount)]
	if ctx.Err() != nil {
		return finish(nil, "cancelled")
	}
	progress.finish(StageRanking, len(groups), len(results), len(groups)-len(results))
	if ctx.Err() != nil {
		return finish(nil, "cancelled")
	}
	save(StageRanking)
	return finish(results, "completed")
}

func (p *h3Pipeline) resolve(candidates []model.Candidate) []model.ProbeResult {
	p.progress.start("dns", len(candidates))
	resolver := p.dependencies.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	type resolved struct {
		addresses []netip.Addr
		err       error
		at        time.Time
	}
	cache := map[string]resolved{}
	groups := []model.ProbeResult{}
	for _, c := range candidates {
		if p.ctx.Err() != nil {
			break
		}
		var resolution resolved
		if c.AddressType == model.AddressDomain {
			var ok bool
			resolution, ok = cache[c.Host]
			if !ok {
				dnsCtx, cancel := context.WithTimeout(p.ctx, p.settings.RequestTimeout())
				resolution.addresses, resolution.err = resolver.LookupNetIP(dnsCtx, "ip4", c.Host)
				cancel()
				resolution.at = time.Now()
				cache[c.Host] = resolution
			}
		} else if ip, err := netip.ParseAddr(c.IP); err == nil && ip.Is4() {
			resolution.addresses = []netip.Addr{ip}
			resolution.at = time.Now()
		}
		group := model.ProbeResult{Mode: model.ModeH3, Candidate: c, ResolvedAt: resolution.at}
		seen := map[string]bool{}
		for _, address := range resolution.addresses {
			address = address.Unmap()
			if address.Is4() && !seen[address.String()] {
				seen[address.String()] = true
				group.Endpoints = append(group.Endpoints, model.EndpointResult{IP: address.String()})
			}
		}
		passed := resolution.err == nil && len(group.Endpoints) > 0
		failure := map[model.FailureReason]int{}
		if passed {
			groups = append(groups, group)
		} else {
			failure[model.FailureDNS] = 1
		}
		p.progress.advance("dns", passed, failure)
	}
	if p.ctx.Err() == nil {
		p.progress.finish("dns", len(candidates), len(groups), len(candidates)-len(groups))
	}
	p.setCounts(groups)
	return groups
}

func (p *h3Pipeline) setCounts(groups []model.ProbeResult) {
	physical := map[string]bool{}
	for _, r := range groups {
		for _, ep := range r.Endpoints {
			physical[(model.Candidate{IP: ep.IP, Port: r.Candidate.Port}).Key()] = true
		}
	}
	p.progress.mu.Lock()
	p.progress.value.LogicalCandidates, p.progress.value.PhysicalCandidates = len(groups), len(physical)
	p.progress.mu.Unlock()
	p.progress.send()
}

func (p *h3Pipeline) probeBatch(stage string, groups []model.ProbeResult) []model.ProbeResult {
	todo := []model.Candidate{}
	queued := map[string]bool{}
	for _, r := range groups {
		for _, ep := range r.Endpoints {
			c := model.Candidate{AddressType: model.AddressIPv4, IP: ep.IP, Port: r.Candidate.Port}
			if !queued[c.Key()] {
				queued[c.Key()] = true
				todo = append(todo, c)
			}
		}
	}
	attempts, concurrency := p.settings.ConnectionProbeCount, p.settings.ConnectionConcurrency
	if stage == StageHTTP3 {
		attempts, concurrency = p.settings.HTTPProbeCount, p.settings.HTTPConcurrency
	}
	if stage == StageBandwidth {
		attempts, concurrency = 1, p.settings.BandwidthConcurrency
	}
	p.cacheMu.Lock()
	if p.cache[stage] == nil {
		p.cache[stage] = &h3StageCache{results: map[string]*h3Measurement{}, slots: make(chan struct{}, concurrency)}
	}
	cache := p.cache[stage]
	p.cacheMu.Unlock()
	p.progress.extendProbe(stage, len(groups), 0)
	// Connection and HTTP stages scan their complete pools once.
	if stage == StageQUIC || stage == StageHTTP3 {
		p.progress.planAttempts(stage, len(todo)*attempts)
	}
	type measurement struct {
		key      string
		endpoint model.EndpointResult
	}
	type endpointLocation struct{ group, endpoint int }
	output := cloneH3Results(groups)
	locations := map[string][]endpointLocation{}
	pending := make([]int, len(output))
	complete := func(r *model.ProbeResult) {
		aggregateH3(r)
		passed := p.passed(*r, stage)
		failures := map[model.FailureReason]int{}
		if !passed {
			switch stage {
			case StageQUIC:
				failures = nodeFailure(false, r.QUIC.Failures, model.FailureQUIC)
			case StageHTTP3:
				failures = nodeFailure(false, r.HTTP3.Failures, model.FailureProtocol)
			default:
				reason := r.Bandwidth.Failure
				if reason == "" {
					reason = model.FailureDownload
				}
				failures[reason] = 1
			}
		}
		p.progress.advance(stage, passed, failures)
	}
	for i := range output {
		r := &output[i]
		pending[i] = len(r.Endpoints)
		if pending[i] == 0 {
			complete(r)
		}
		for j, ep := range r.Endpoints {
			key := (model.Candidate{IP: ep.IP, Port: r.Candidate.Port}).Key()
			locations[key] = append(locations[key], endpointLocation{i, j})
		}
	}
	var aggregationMu sync.Mutex
	parallel(p.ctx, todo, concurrency, func(ctx context.Context, c model.Candidate) measurement {
		cache.mu.Lock()
		if cached, ok := cache.results[c.Key()]; ok {
			cache.mu.Unlock()
			<-cached.done
			return measurement{c.Key(), cached.endpoint}
		}
		cached := &h3Measurement{done: make(chan struct{})}
		cache.results[c.Key()] = cached
		cache.mu.Unlock()
		defer close(cached.done)
		if stage == StageBandwidth {
			p.progress.planAttempts(stage, attempts)
		}
		ep := model.EndpointResult{IP: c.IP}
		select {
		case cache.slots <- struct{}{}:
			defer func() { <-cache.slots }()
		case <-ctx.Done():
			cached.endpoint = ep
			return measurement{c.Key(), ep}
		}
		onAttempt := func() { p.progress.attempt(stage) }
		switch stage {
		case StageQUIC:
			ep.QUIC = p.dependencies.QUIC.Probe(ctx, c, attempts, onAttempt)
			p.progress.skipAttempts(stage, ep.QUIC.Skipped)
		case StageHTTP3:
			ep.HTTP3 = p.dependencies.HTTP3.Probe(ctx, c, attempts, onAttempt)
			p.progress.skipAttempts(stage, ep.HTTP3.Skipped)
		case StageBandwidth:
			ep.Bandwidth = p.dependencies.H3Bandwidth.Probe(ctx, c)
			onAttempt()
			p.progress.mu.Lock()
			p.progress.value.DownloadedBytes += ep.Bandwidth.Bytes
			p.progress.mu.Unlock()
		}
		cached.endpoint = ep
		return measurement{c.Key(), ep}
	}, func(value measurement) {
		aggregationMu.Lock()
		defer aggregationMu.Unlock()
		for _, location := range locations[value.key] {
			r := &output[location.group]
			ep := &r.Endpoints[location.endpoint]
			switch stage {
			case StageQUIC:
				ep.QUIC = value.endpoint.QUIC
			case StageHTTP3:
				ep.HTTP3 = value.endpoint.HTTP3
			case StageBandwidth:
				ep.Bandwidth = value.endpoint.Bandwidth
			}
			pending[location.group]--
			if pending[location.group] == 0 {
				complete(r)
			}
		}
	})
	return output
}

func (p *h3Pipeline) selectStage(stage string, groups []model.ProbeResult, target int) []model.ProbeResult {
	p.progress.start(stage, 0)
	if stage == StageHTTP3 {
		results := p.probeBatch(stage, groups)
		p.finishStage(stage, results)
		selected := countIf(results, func(r model.ProbeResult) bool {
			return !p.retained[r.Candidate.Key()] && p.passed(r, stage)
		})
		p.progress.selectCandidates(stage, min(target, selected))
		return results
	}
	retained, fresh := []model.ProbeResult{}, []model.ProbeResult{}
	for _, r := range groups {
		if p.retained[r.Candidate.Key()] {
			retained = append(retained, r)
		} else {
			fresh = append(fresh, r)
		}
	}
	planned := min(target, len(fresh))
	p.progress.downloadQueue(stage, model.StageQueue{Pool: len(fresh), Target: target, Planned: planned, Pending: planned})
	results := p.probeBatch(stage, retained)
	results = append(results, parallelUntil(p.ctx, fresh, p.settings.BandwidthConcurrency, target, func(_ context.Context, group model.ProbeResult) model.ProbeResult {
		return p.probeBatch(stage, []model.ProbeResult{group})[0]
	}, func(group model.ProbeResult) bool { return p.passed(group, stage) }, func(queue model.StageQueue) {
		p.progress.downloadQueue(stage, queue)
	})...)
	p.finishStage(stage, results)
	return results
}

func (p *h3Pipeline) httpCandidates(groups []model.ProbeResult) []model.ProbeResult {
	groups = p.eligible(groups, StageHTTP3)
	slices.SortStableFunc(groups, func(a, b model.ProbeResult) int {
		if rate := compareNumber(b.HTTP3.SuccessRate, a.HTTP3.SuccessRate); rate != 0 {
			return rate
		}
		return compareNumber(a.HTTP3.P95MS, b.HTTP3.P95MS)
	})
	selected := 0
	groups = filter(groups, func(r model.ProbeResult) bool {
		if p.retained[r.Candidate.Key()] {
			return true
		}
		if selected == p.settings.HTTPCandidateCount {
			return false
		}
		selected++
		return true
	})
	p.progress.selectCandidates(StageHTTP3, selected)
	return groups
}

func (p *h3Pipeline) finishStage(stage string, groups []model.ProbeResult) {
	if p.ctx.Err() != nil {
		return
	}
	passed := countIf(groups, func(r model.ProbeResult) bool { return p.passed(r, stage) })
	p.progress.finish(stage, len(groups), passed, len(groups)-passed)
}
func (p *h3Pipeline) passed(r model.ProbeResult, stage string) bool {
	if len(r.Endpoints) == 0 || r.QUIC.SuccessRate < p.settings.ConnectionMinSuccessRate {
		return false
	}
	if stage == StageQUIC {
		return true
	}
	if r.HTTP3.SuccessRate < p.settings.HTTPMinSuccessRate {
		return false
	}
	return stage != StageBandwidth || bandwidthPassed(r)
}
func (p *h3Pipeline) eligible(groups []model.ProbeResult, stage string) []model.ProbeResult {
	return filter(groups, func(r model.ProbeResult) bool {
		return p.settings.AllowsPort(r.Candidate.Port) && p.settings.AllowsCountry(r.Candidate.Country) && p.passed(r, stage)
	})
}
func cloneH3Results(groups []model.ProbeResult) []model.ProbeResult {
	result := append([]model.ProbeResult(nil), groups...)
	for i := range result {
		result[i].Endpoints = append([]model.EndpointResult(nil), result[i].Endpoints...)
	}
	return result
}
func compareNumber(a, b float64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func aggregateH3(r *model.ProbeResult) {
	if len(r.Endpoints) == 0 {
		return
	}
	r.QUIC, r.HTTP3 = model.ProbeStats{}, model.ProbeStats{}
	r.Bandwidth = model.BandwidthStats{}
	for i, ep := range r.Endpoints {
		r.QUIC = worstStats(r.QUIC, ep.QUIC, i == 0)
		r.HTTP3 = worstStats(r.HTTP3, ep.HTTP3, i == 0)
		if i == 0 {
			r.Bandwidth = ep.Bandwidth
		} else {
			r.Bandwidth.Bytes += ep.Bandwidth.Bytes
			r.Bandwidth.Mbps = math.Min(r.Bandwidth.Mbps, ep.Bandwidth.Mbps)
			r.Bandwidth.TTFBMS = math.Max(r.Bandwidth.TTFBMS, ep.Bandwidth.TTFBMS)
			r.Bandwidth.DurationMS = math.Max(r.Bandwidth.DurationMS, ep.Bandwidth.DurationMS)
			if ep.Bandwidth.Failure != "" {
				r.Bandwidth.Failure = ep.Bandwidth.Failure
			}
		}
	}
}
func worstStats(a, b model.ProbeStats, first bool) model.ProbeStats {
	if first {
		a = b
		a.SamplesMS = nil
		a.Failures = map[model.FailureReason]int{}
		for reason, n := range b.Failures {
			a.Failures[reason] = n
		}
		return a
	}
	a.Attempts = max(a.Attempts, b.Attempts)
	a.Skipped = max(a.Skipped, b.Skipped)
	a.Successes = min(a.Successes, b.Successes)
	a.SuccessRate = math.Min(a.SuccessRate, b.SuccessRate)
	a.AverageMS = math.Max(a.AverageMS, b.AverageMS)
	a.P50MS = math.Max(a.P50MS, b.P50MS)
	a.P95MS = math.Max(a.P95MS, b.P95MS)
	a.JitterMS = math.Max(a.JitterMS, b.JitterMS)
	for reason, n := range b.Failures {
		a.Failures[reason] += n
	}
	return a
}
