package run

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
	"github.com/linvva/cf-node-bench/internal/ranking"
	"github.com/linvva/cf-node-bench/internal/source"
)

type SourceFetcher interface {
	Fetch(context.Context, source.HTTPSource) (source.ParseResult, error)
}
type TCPProbe interface {
	Probe(context.Context, model.Candidate, int) model.ProbeStats
}
type HTTPSProbe interface {
	Probe(context.Context, model.Candidate, int) model.ProbeStats
}
type BandwidthProbe interface {
	Probe(context.Context, model.Candidate) model.BandwidthStats
}

type Dependencies struct {
	Fetcher   SourceFetcher
	TCP       TCPProbe
	HTTPS     HTTPSProbe
	Bandwidth BandwidthProbe
}

type Engine struct{ Dependencies Dependencies }

func (e Engine) Run(ctx context.Context, runID string, settings config.Settings, sources []source.HTTPSource, emit func(model.RunProgress)) model.RunSummary {
	started := time.Now()
	progress := newProgress(runID, started, emit)
	results := make([]model.ProbeResult, 0)

	enabledSources := countIf(sources, func(item source.HTTPSource) bool { return item.Enabled })
	progress.start("source", enabledSources)
	candidates := make([]model.Candidate, 0)
	parseFailures := map[model.FailureReason]int{}
	sourcesPassed := 0
	for _, current := range sources {
		if !current.Enabled {
			continue
		}
		parsed, err := e.Dependencies.Fetcher.Fetch(ctx, current)
		if err != nil {
			if ctx.Err() != nil {
				progress.advance("source", false, map[model.FailureReason]int{model.FailureCancelled: 1})
				return progress.summary(started, nil, "cancelled")
			}
			progress.advance("source", false, map[model.FailureReason]int{model.FailureDownload: 1})
			continue
		}
		for _, failure := range parsed.Failures {
			parseFailures[failure.Reason]++
		}
		candidates = append(candidates, parsed.Candidates...)
		sourcesPassed++
		progress.advance("source", true, nil)
		if ctx.Err() != nil {
			return progress.summary(started, nil, "cancelled")
		}
	}
	candidates = unique(candidates)
	progress.finish("source", enabledSources, sourcesPassed, enabledSources-sourcesPassed)

	filterStarted := time.Now()
	filterInput := len(candidates) + failureCount(parseFailures)
	progress.start("filter", filterInput)
	filterFailures := cloneFailures(parseFailures)
	filteredCandidates := make([]model.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if !settings.AllowsPort(candidate.Port) {
			filterFailures[model.FailurePortFiltered]++
			continue
		}
		if !settings.AllowsCountry(candidate.Country) {
			filterFailures[model.FailureCountryFiltered]++
			continue
		}
		filteredCandidates = append(filteredCandidates, candidate)
	}
	candidates = filteredCandidates
	filterFailed := failureCount(filterFailures)
	progress.addFailures(filterFailures)
	progress.finishAt("filter", filterInput, len(candidates), filterFailed, filterStarted)

	progress.start("tcp", len(candidates))
	tcpStarted := time.Now()
	tcpResults := parallel(ctx, candidates, settings.TCPConcurrency, func(ctx context.Context, candidate model.Candidate) model.ProbeResult {
		return model.ProbeResult{Candidate: candidate, TCP: e.Dependencies.TCP.Probe(ctx, candidate, settings.TCPProbeCount)}
	}, func(result model.ProbeResult) {
		passed := result.TCP.SuccessRate >= settings.TCPMinSuccessRate
		progress.advance("tcp", passed, nodeFailure(passed, result.TCP.Failures, model.FailureTCP))
	})
	slices.SortStableFunc(tcpResults, compareTCP)
	passedTCP := countIf(tcpResults, func(r model.ProbeResult) bool { return r.TCP.SuccessRate >= settings.TCPMinSuccessRate })
	tcpResults = filter(tcpResults, func(r model.ProbeResult) bool { return r.TCP.SuccessRate >= settings.TCPMinSuccessRate })
	if len(tcpResults) > settings.TCPCandidateCount {
		tcpResults = tcpResults[:settings.TCPCandidateCount]
	}
	progress.finishAt("tcp", len(candidates), passedTCP, len(candidates)-passedTCP, tcpStarted)
	if ctx.Err() != nil {
		return progress.summary(started, nil, "cancelled")
	}

	progress.start("https", len(tcpResults))
	httpsStarted := time.Now()
	httpsResults := parallel(ctx, tcpResults, settings.HTTPSConcurrency, func(ctx context.Context, result model.ProbeResult) model.ProbeResult {
		result.HTTPS = e.Dependencies.HTTPS.Probe(ctx, result.Candidate, settings.HTTPSProbeCount)
		return result
	}, func(result model.ProbeResult) {
		passed := result.HTTPS.SuccessRate >= settings.HTTPSMinSuccessRate
		progress.advance("https", passed, nodeFailure(passed, result.HTTPS.Failures, model.FailureTCP))
	})
	slices.SortStableFunc(httpsResults, compareHTTPS)
	passedHTTPS := countIf(httpsResults, func(r model.ProbeResult) bool { return r.HTTPS.SuccessRate >= settings.HTTPSMinSuccessRate })
	httpsResults = filter(httpsResults, func(r model.ProbeResult) bool { return r.HTTPS.SuccessRate >= settings.HTTPSMinSuccessRate })
	progress.finishAt("https", len(tcpResults), passedHTTPS, len(tcpResults)-passedHTTPS, httpsStarted)
	if ctx.Err() != nil {
		return progress.summary(started, nil, "cancelled")
	}

	if len(httpsResults) > settings.BandwidthCandidates {
		httpsResults = httpsResults[:settings.BandwidthCandidates]
	}
	progress.start("bandwidth", len(httpsResults))
	bandStarted := time.Now()
	results = parallel(ctx, httpsResults, settings.BandwidthConcurrency, func(ctx context.Context, result model.ProbeResult) model.ProbeResult {
		result.Bandwidth = e.Dependencies.Bandwidth.Probe(ctx, result.Candidate)
		return result
	}, func(result model.ProbeResult) {
		failures := map[model.FailureReason]int{}
		if result.Bandwidth.Failure != "" {
			failures[result.Bandwidth.Failure] = 1
		}
		progress.advance("bandwidth", result.Bandwidth.Mbps > 0 && result.Bandwidth.Failure == "", failures)
	})
	passedBand := countIf(results, func(r model.ProbeResult) bool { return r.Bandwidth.Mbps > 0 && r.Bandwidth.Failure == "" })
	progress.finishAt("bandwidth", len(results), passedBand, len(results)-passedBand, bandStarted)
	if ctx.Err() != nil {
		return progress.summary(started, nil, "cancelled")
	}

	rankingInput := len(results)
	progress.start("ranking", rankingInput)
	rankStarted := time.Now()
	results = ranking.Score(results, settings.TCPMinSuccessRate, settings.HTTPSMinSuccessRate)
	if len(results) > settings.FinalResultCount {
		results = results[:settings.FinalResultCount]
	}
	progress.finishAt("ranking", rankingInput, len(results), rankingInput-len(results), rankStarted)
	return progress.summary(started, results, "completed")
}

func parallel[I any, O any](ctx context.Context, input []I, concurrency int, work func(context.Context, I) O, completed func(O)) []O {
	jobs := make(chan I)
	results := make(chan O, len(input))
	var workers sync.WaitGroup
	for index := 0; index < min(concurrency, len(input)); index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for item := range jobs {
				result := work(ctx, item)
				if completed != nil {
					completed(result)
				}
				results <- result
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, item := range input {
			select {
			case jobs <- item:
			case <-ctx.Done():
				return
			}
		}
	}()
	workers.Wait()
	close(results)
	output := make([]O, 0, len(input))
	for result := range results {
		output = append(output, result)
	}
	return output
}

func compareTCP(a, b model.ProbeResult) int {
	if a.TCP.SuccessRate != b.TCP.SuccessRate {
		if a.TCP.SuccessRate > b.TCP.SuccessRate {
			return -1
		}
		return 1
	}
	if a.TCP.P95MS < b.TCP.P95MS {
		return -1
	}
	if a.TCP.P95MS > b.TCP.P95MS {
		return 1
	}
	return 0
}
func compareHTTPS(a, b model.ProbeResult) int {
	if a.HTTPS.SuccessRate != b.HTTPS.SuccessRate {
		if a.HTTPS.SuccessRate > b.HTTPS.SuccessRate {
			return -1
		}
		return 1
	}
	if a.HTTPS.P95MS < b.HTTPS.P95MS {
		return -1
	}
	if a.HTTPS.P95MS > b.HTTPS.P95MS {
		return 1
	}
	return 0
}
func filter[T any](items []T, keep func(T) bool) []T {
	result := items[:0]
	for _, item := range items {
		if keep(item) {
			result = append(result, item)
		}
	}
	return result
}
func countIf[T any](items []T, condition func(T) bool) int {
	count := 0
	for _, item := range items {
		if condition(item) {
			count++
		}
	}
	return count
}
func unique(items []model.Candidate) []model.Candidate {
	seen := map[string]bool{}
	result := items[:0]
	for _, item := range items {
		if !seen[item.Key()] {
			seen[item.Key()] = true
			result = append(result, item)
		}
	}
	return result
}

func cloneFailures(failures map[model.FailureReason]int) map[model.FailureReason]int {
	result := make(map[model.FailureReason]int, len(failures))
	for reason, count := range failures {
		result[reason] = count
	}
	return result
}

func failureCount(failures map[model.FailureReason]int) int {
	total := 0
	for _, count := range failures {
		total += count
	}
	return total
}

func nodeFailure(passed bool, failures map[model.FailureReason]int, fallback model.FailureReason) map[model.FailureReason]int {
	if passed {
		return nil
	}
	selected := fallback
	selectedCount := 0
	for reason, count := range failures {
		if count > selectedCount || count == selectedCount && reason < selected {
			selected = reason
			selectedCount = count
		}
	}
	return map[model.FailureReason]int{selected: 1}
}

type progressTracker struct {
	mu           sync.Mutex
	value        model.RunProgress
	emit         func(model.RunProgress)
	stageStarts  map[string]time.Time
	stageUpdates map[string]int
	lastEmit     time.Time
}

func newProgress(id string, started time.Time, emit func(model.RunProgress)) *progressTracker {
	return &progressTracker{
		value:        model.RunProgress{RunID: id, State: "running", StartedAt: started, Failures: map[model.FailureReason]int{}},
		emit:         emit,
		stageStarts:  map[string]time.Time{},
		stageUpdates: map[string]int{},
	}
}
func (p *progressTracker) start(name string, input int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stageStarts[name] = time.Now()
	p.value.Stages = append(p.value.Stages, model.StageProgress{Name: name, Input: input, State: "running"})
	p.sendLocked()
}
func (p *progressTracker) finish(name string, input, passed, failed int) {
	p.finishAt(name, input, passed, failed, p.stageStarts[name])
}
func (p *progressTracker) finishAt(name string, input, passed, failed int, started time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.value.Stages {
		if p.value.Stages[i].Name == name {
			p.value.Stages[i] = model.StageProgress{Name: name, Input: input, Passed: passed, Failed: failed, DurationMS: time.Since(started).Milliseconds(), State: "completed"}
		}
	}
	p.sendLocked()
}
func (p *progressTracker) advance(name string, passed bool, failures map[model.FailureReason]int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.value.Stages {
		if p.value.Stages[i].Name != name {
			continue
		}
		if passed {
			p.value.Stages[i].Passed++
		} else {
			p.value.Stages[i].Failed++
		}
		p.value.Stages[i].DurationMS = time.Since(p.stageStarts[name]).Milliseconds()
		break
	}
	for reason, count := range failures {
		p.value.Failures[reason] += count
	}
	p.stageUpdates[name]++
	if p.stageUpdates[name] == 1 || time.Since(p.lastEmit) >= 100*time.Millisecond {
		p.sendLocked()
	}
}
func (p *progressTracker) addFailures(failures map[model.FailureReason]int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for reason, count := range failures {
		p.value.Failures[reason] += count
	}
}
func (p *progressTracker) sendLocked() {
	if p.emit != nil {
		copyValue := p.value
		copyValue.Stages = append([]model.StageProgress(nil), p.value.Stages...)
		copyValue.Failures = map[model.FailureReason]int{}
		for k, v := range p.value.Failures {
			copyValue.Failures[k] = v
		}
		p.emit(copyValue)
		p.lastEmit = time.Now()
	}
}
func (p *progressTracker) summary(started time.Time, results []model.ProbeResult, state string) model.RunSummary {
	p.mu.Lock()
	defer p.mu.Unlock()
	if results == nil {
		results = []model.ProbeResult{}
	}
	p.value.State = state
	p.value.Message = fmt.Sprintf("%d 个结果", len(results))
	p.sendLocked()
	failures := map[model.FailureReason]int{}
	for k, v := range p.value.Failures {
		failures[k] = v
	}
	return model.RunSummary{RunID: p.value.RunID, StartedAt: started, FinishedAt: time.Now(), State: state, Results: results, Failures: failures}
}
