package run

import (
	"slices"
	"strings"

	"github.com/linvva/cf-node-bench/internal/model"
)

func (p *h3Pipeline) quickH3(groups []model.ProbeResult, c, base *Checkpoint, resume bool, save func()) []model.ProbeResult {
	var extra, fresh []model.ProbeResult
	for _, result := range p.eligible(groups, StageQUIC) {
		if result.Pinned || p.retained[result.Candidate.Key()] {
			extra = append(extra, result)
		} else {
			fresh = append(fresh, result)
		}
	}
	slices.SortFunc(fresh, func(a, b model.ProbeResult) int {
		if order := compareNumber(b.QUIC.SuccessRate, a.QUIC.SuccessRate); order != 0 {
			return order
		}
		if order := compareNumber(a.QUIC.P95MS, b.QUIC.P95MS); order != 0 {
			return order
		}
		return strings.Compare(a.Candidate.Key(), b.Candidate.Key())
	})
	var previous []model.ProbeResult
	if resume {
		previous = base.HTTP3Results
		c.HTTP3Results = slices.Clone(previous)
		cache := &h3StageCache{results: map[string]*h3Measurement{}, slots: make(chan struct{}, p.settings.HTTPConcurrency)}
		done := make(chan struct{})
		close(done)
		for _, result := range previous {
			for _, endpoint := range result.Endpoints {
				key := (model.Candidate{IP: endpoint.IP, Port: result.Candidate.Port}).Key()
				cache.results[key] = &h3Measurement{done: done, endpoint: endpoint}
			}
		}
		p.cache[StageHTTP3] = cache
	}
	known := quickCache(previous)
	measure := func(batch []model.ProbeResult) []model.ProbeResult {
		http := p.quickHTTP(StageHTTP3, batch, known, func(items []model.ProbeResult) []model.ProbeResult {
			return p.selectStage(StageHTTP3, items, p.settings.HTTPCandidateCount)
		}, func(result model.ProbeResult) bool { return p.passed(result, StageHTTP3) })
		if p.ctx.Err() != nil {
			return nil
		}
		c.HTTP3Results = appendQuickResults(c.HTTP3Results, known, cloneH3Results(http))
		save()
		return p.selectStage(StageBandwidth, p.httpCandidates(http), p.settings.BandwidthCandidates)
	}
	var output []model.ProbeResult
	if len(extra) > 0 {
		output = measure(extra)
	}
	next, complete := p.quickBatches(fresh, measure)
	c.HTTPSelectionComplete = complete
	return append(output, next...)
}

// Complete the extra nodes first. Each fresh batch then has its own HTTP pass
// before entering the existing, target-limited download queue.
func (p stagePipeline) quickBatches(fresh []model.ProbeResult, run func([]model.ProbeResult) []model.ProbeResult) ([]model.ProbeResult, bool) {
	var output []model.ProbeResult
	for batch := range slices.Chunk(fresh, max(1, p.settings.HTTPCandidateCount)) {
		if p.ctx.Err() != nil || p.progress.currentQueue().Passed >= p.settings.BandwidthCandidates {
			return output, false
		}
		output = append(output, run(batch)...)
	}
	return output, p.ctx.Err() == nil
}

// HTTP measurements from completed batches are already in the same checkpoint
// as the downloads. Replay their eligibility without reopening connections.
func (p stagePipeline) quickHTTP(stage string, results []model.ProbeResult, cached map[string]model.ProbeResult, measure func([]model.ProbeResult) []model.ProbeResult, passed func(model.ProbeResult) bool) []model.ProbeResult {
	var known, pending []model.ProbeResult
	for _, result := range results {
		if old, ok := cached[result.Candidate.Key()]; ok {
			known = append(known, old)
		} else {
			pending = append(pending, result)
		}
	}
	if len(known) > 0 {
		p.progress.start(stage, len(known))
		count := countIf(known, passed)
		p.progress.finish(stage, len(known), count, len(known)-count)
	}
	if len(pending) > 0 {
		known = append(known, measure(pending)...)
	}
	return known
}

func quickCache(base []model.ProbeResult) map[string]model.ProbeResult {
	known := make(map[string]model.ProbeResult, len(base))
	for _, result := range base {
		known[result.Candidate.Key()] = result
	}
	return known
}

func (p stagePipeline) quickTCP(connections, retained []model.ProbeResult, c, base *Checkpoint, resume bool, save func()) []model.ProbeResult {
	var pinned, fresh []model.ProbeResult
	retainedKeys := map[string]bool{}
	for _, result := range retained {
		retainedKeys[result.Candidate.Key()] = true
	}
	for _, result := range connections {
		if result.Pinned {
			pinned = append(pinned, result)
		} else if !retainedKeys[result.Candidate.Key()] {
			fresh = append(fresh, result)
		}
	}
	slices.SortFunc(fresh, func(a, b model.ProbeResult) int {
		if order := compareTCP(a, b); order != 0 {
			return order
		}
		return strings.Compare(a.Candidate.Key(), b.Candidate.Key())
	})
	var previous []model.ProbeResult
	if resume {
		previous = base.HTTPSResults
		c.HTTPSResults = slices.Clone(previous)
	}
	known := quickCache(previous)
	passed := func(result model.ProbeResult) bool { return result.HTTPS.SuccessRate >= p.settings.HTTPMinSuccessRate }
	var output []model.ProbeResult
	if len(pinned)+len(retained) > 0 {
		pinHTTP := p.quickHTTP(StageHTTPS, pinned, known, p.https, passed)
		retainedHTTP := p.quickHTTP("retained", retained, known, p.retained, passed)
		if p.ctx.Err() != nil {
			return nil
		}
		extra := append(pinHTTP, retainedHTTP...)
		c.HTTPSResults = appendQuickResults(c.HTTPSResults, known, extra)
		save()
		output = p.bandwidth(filterHTTPSResults(slices.Clone(extra), p.settings), nil)
	} else {
		p.progress.skip("retained")
	}
	next, complete := p.quickBatches(fresh, func(batch []model.ProbeResult) []model.ProbeResult {
		http := p.quickHTTP(StageHTTPS, batch, known, p.https, passed)
		if p.ctx.Err() != nil {
			return nil
		}
		c.HTTPSResults = appendQuickResults(c.HTTPSResults, known, http)
		save()
		return p.bandwidth(nil, filterHTTPSResults(http, p.settings))
	})
	c.HTTPSelectionComplete = complete
	return append(output, next...)
}

func appendQuickResults(existing []model.ProbeResult, known map[string]model.ProbeResult, results []model.ProbeResult) []model.ProbeResult {
	for _, result := range results {
		key := result.Candidate.Key()
		if _, ok := known[key]; !ok {
			existing = append(existing, result)
			known[key] = result
		}
	}
	return existing
}
