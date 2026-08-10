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
	Probe(context.Context, model.Candidate, int, func()) model.ProbeStats
}
type HTTPSProbe interface {
	Probe(context.Context, model.Candidate, int, func()) model.ProbeStats
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

type stagePipeline struct {
	ctx          context.Context
	dependencies Dependencies
	settings     config.Settings
	progress     *progressTracker
}

type RunInput struct {
	Sources  []source.HTTPSource
	Retained []model.ProbeResult
	Plan     RunPlan
}

func (e Engine) Run(ctx context.Context, runID string, settings config.Settings, input RunInput, emit func(model.RunProgress)) model.RunSummary {
	started := time.Now()
	progress := newProgress(runID, started, emit)
	startStage := input.Plan.StartStage
	if !ValidStartStage(startStage) || startStage != StageSource && input.Plan.Base == nil {
		startStage = StageSource
	}
	base := input.Plan.Base
	parentRunID := ""
	if base != nil {
		parentRunID = base.RunID
	}
	checkpoint := checkpointForRun(runID, parentRunID, settings, input.Sources, input.Plan.Network, startStage, base)
	checkpointWarning := ""
	pipeline := stagePipeline{ctx: ctx, dependencies: e.Dependencies, settings: settings, progress: progress}
	persistCheckpoint := func() {
		checkpoint.Stages = progress.snapshot()
		if input.Plan.Save != nil {
			if err := input.Plan.Save(checkpoint); err != nil {
				checkpointWarning = "无法保存阶段检查点：" + err.Error()
				progress.setCheckpointWarning(checkpointWarning)
			}
		}
	}
	saveCheckpoint := func(stage string) {
		if checkpoint.CompletedAt == nil {
			checkpoint.CompletedAt = map[string]time.Time{}
		}
		if checkpoint.StageSettings == nil {
			checkpoint.StageSettings = map[string]config.Settings{}
		}
		if checkpoint.StageNetworks == nil {
			checkpoint.StageNetworks = map[string]NetworkSnapshot{}
		}
		checkpoint.CompletedAt[stage] = time.Now()
		checkpoint.StageSettings[stage] = settings
		checkpoint.StageNetworks[stage] = input.Plan.Network
		persistCheckpoint()
	}
	finish := func(results []model.ProbeResult, state string) model.RunSummary {
		summary := progress.summary(started, results, state)
		summary.ScoreWeights = settings.ScoreWeights
		summary.ParentRunID = parentRunID
		summary.StartStage = startStage
		summary.ReusedStages = reusedStages(startStage)
		summary.CheckpointWarning = checkpointWarning
		return summary
	}

	var candidates []model.Candidate
	var retainedResults []model.ProbeResult
	var tcpResults []model.ProbeResult
	var httpsResults []model.ProbeResult
	var results []model.ProbeResult

	if startStage == StageSource {
		var parseFailures map[model.FailureReason]int
		var cancelled bool
		candidates, parseFailures, cancelled = pipeline.source(input.Sources)
		if cancelled {
			return finish(nil, "cancelled")
		}
		var retainedKeys map[string]bool
		var retainedFailures map[model.FailureReason]int
		retainedResults, retainedKeys, retainedFailures = prepareRetained(input.Retained, candidates, settings)
		freshCandidates := filter(append([]model.Candidate(nil), candidates...), func(candidate model.Candidate) bool { return !retainedKeys[candidate.Key()] })
		checkpoint.Candidates = append([]model.Candidate(nil), candidates...)
		for _, retained := range retainedResults {
			checkpoint.Candidates = append(checkpoint.Candidates, retained.Candidate)
		}
		checkpoint.Candidates = unique(checkpoint.Candidates)
		candidates = freshCandidates
		saveCheckpoint("source")
		candidates = pipeline.filter(candidates, parseFailures)
		progress.addFailures(retainedFailures)
	} else if startStage == StageTCP {
		progress.reuse(checkpointStage(base, "source", len(checkpoint.Candidates)), completedTime(base, "source"))
		candidates = pipeline.filter(append([]model.Candidate(nil), checkpoint.Candidates...), nil)
	}

	if startStage == StageSource || startStage == StageTCP {
		persistCheckpoint()
		rawTCPResults := pipeline.tcp(candidates)
		if ctx.Err() != nil {
			return finish(nil, "cancelled")
		}
		checkpoint.TCPResults = append(append([]model.ProbeResult(nil), rawTCPResults...), retainedResults...)
		saveCheckpoint("tcp")
		tcpResults = filterTCPResults(rawTCPResults, settings)
	} else {
		progress.reuse(checkpointStage(base, "source", len(checkpoint.Candidates)), completedTime(base, "source"))
		progress.reuse(checkpointStage(base, "filter", len(checkpoint.Candidates)), completedTime(base, "filter"))
		progress.reuse(checkpointStage(base, "tcp", len(checkpoint.TCPResults)), completedTime(base, "tcp"))
		tcpResults = filterTCPResults(append([]model.ProbeResult(nil), checkpoint.TCPResults...), settings)
		persistCheckpoint()
	}

	if startStage == StageSource || startStage == StageTCP || startStage == StageHTTPS {
		rawHTTPSResults := pipeline.https(tcpResults)
		if ctx.Err() != nil {
			return finish(nil, "cancelled")
		}
		checkpoint.HTTPSResults = append([]model.ProbeResult(nil), rawHTTPSResults...)
		saveCheckpoint("https")
		httpsResults = filterHTTPSResults(rawHTTPSResults, settings)

		if startStage == StageSource {
			rawRetainedResults := pipeline.retained(retainedResults)
			if ctx.Err() != nil {
				return finish(nil, "cancelled")
			}
			checkpoint.HTTPSResults = append(checkpoint.HTTPSResults, rawRetainedResults...)
			retainedResults = filterHTTPSResults(rawRetainedResults, settings)
			saveCheckpoint("retained")
		} else {
			progress.skip("retained")
			retainedResults = nil
			saveCheckpoint("retained")
		}
	} else {
		progress.reuse(checkpointStage(base, "https", len(checkpoint.HTTPSResults)), completedTime(base, "https"))
		progress.reuse(checkpointStage(base, "retained", 0), completedTime(base, "retained"))
		httpsResults = filterHTTPSResults(append([]model.ProbeResult(nil), checkpoint.HTTPSResults...), settings)
		persistCheckpoint()
	}

	if startStage != StageRanking {
		results = pipeline.bandwidth(retainedResults, httpsResults)
		if ctx.Err() != nil {
			return finish(nil, "cancelled")
		}
		checkpoint.BandwidthResults = append([]model.ProbeResult(nil), results...)
		saveCheckpoint("bandwidth")
	} else {
		progress.reuse(checkpointStage(base, "bandwidth", len(checkpoint.BandwidthResults)), completedTime(base, "bandwidth"))
		results = append([]model.ProbeResult(nil), checkpoint.BandwidthResults...)
		persistCheckpoint()
	}

	results = pipeline.ranking(results)
	saveCheckpoint("ranking")
	return finish(results, "completed")
}

func (p stagePipeline) source(sources []source.HTTPSource) ([]model.Candidate, map[model.FailureReason]int, bool) {
	enabled := countIf(sources, func(item source.HTTPSource) bool { return item.Enabled })
	p.progress.start("source", enabled)
	candidates := []model.Candidate{}
	failures := map[model.FailureReason]int{}
	passed := 0
	for _, current := range sources {
		if !current.Enabled {
			continue
		}
		parsed, err := p.dependencies.Fetcher.Fetch(p.ctx, current)
		if err != nil {
			if p.ctx.Err() != nil {
				p.progress.advance("source", false, map[model.FailureReason]int{model.FailureCancelled: 1})
				return candidates, failures, true
			}
			p.progress.advance("source", false, map[model.FailureReason]int{model.FailureDownload: 1})
			continue
		}
		for _, failure := range parsed.Failures {
			failures[failure.Reason]++
		}
		candidates = append(candidates, parsed.Candidates...)
		passed++
		p.progress.advance("source", true, nil)
		if p.ctx.Err() != nil {
			return candidates, failures, true
		}
	}
	p.progress.finish("source", enabled, passed, enabled-passed)
	return unique(candidates), failures, false
}

func (p stagePipeline) filter(candidates []model.Candidate, initial map[model.FailureReason]int) []model.Candidate {
	started := time.Now()
	input := len(candidates) + failureCount(initial)
	p.progress.start("filter", input)
	failures := cloneFailures(initial)
	candidates = filterCandidates(candidates, p.settings, failures)
	p.progress.addFailures(failures)
	p.progress.finishAt("filter", input, len(candidates), failureCount(failures), started)
	return candidates
}

func (p stagePipeline) tcp(candidates []model.Candidate) []model.ProbeResult {
	p.progress.startProbe("tcp", len(candidates), p.settings.TCPProbeCount)
	started := time.Now()
	results := parallel(p.ctx, candidates, p.settings.TCPConcurrency, func(ctx context.Context, candidate model.Candidate) model.ProbeResult {
		return model.ProbeResult{Candidate: candidate, TCP: p.dependencies.TCP.Probe(ctx, candidate, p.settings.TCPProbeCount, func() { p.progress.attempt("tcp") })}
	}, func(result model.ProbeResult) {
		passed := result.TCP.SuccessRate >= p.settings.TCPMinSuccessRate
		p.progress.advance("tcp", passed, nodeFailure(passed, result.TCP.Failures, model.FailureTCP))
	})
	passed := countIf(results, func(result model.ProbeResult) bool { return result.TCP.SuccessRate >= p.settings.TCPMinSuccessRate })
	p.progress.finishAt("tcp", len(candidates), passed, len(candidates)-passed, started)
	return results
}

func (p stagePipeline) https(candidates []model.ProbeResult) []model.ProbeResult {
	initialCount := min(p.settings.TCPCandidateCount, len(candidates))
	target := min(p.settings.TCPCandidateCount, len(candidates))
	p.progress.startProbe("https", initialCount, p.settings.HTTPSProbeCount)
	started := time.Now()
	results := make([]model.ProbeResult, 0, initialCount)
	passed, next := 0, 0
	for passed < target && next < len(candidates) && p.ctx.Err() == nil {
		batchSize := initialCount
		if next > 0 {
			batchSize = min(target-passed, len(candidates)-next)
			p.progress.extendProbe("https", batchSize, p.settings.HTTPSProbeCount)
		}
		batch := candidates[next : next+batchSize]
		next += batchSize
		batchResults := parallel(p.ctx, batch, p.settings.HTTPSConcurrency, func(ctx context.Context, result model.ProbeResult) model.ProbeResult {
			result.HTTPS = p.dependencies.HTTPS.Probe(ctx, result.Candidate, p.settings.HTTPSProbeCount, func() { p.progress.attempt("https") })
			return result
		}, func(result model.ProbeResult) {
			qualified := result.HTTPS.SuccessRate >= p.settings.HTTPSMinSuccessRate
			p.progress.advance("https", qualified, nodeFailure(qualified, result.HTTPS.Failures, model.FailureTLS))
		})
		results = append(results, batchResults...)
		passed += countIf(batchResults, func(result model.ProbeResult) bool { return result.HTTPS.SuccessRate >= p.settings.HTTPSMinSuccessRate })
	}
	p.progress.finishAt("https", next, passed, next-passed, started)
	return results
}

func (p stagePipeline) retained(candidates []model.ProbeResult) []model.ProbeResult {
	p.progress.startProbe("retained", len(candidates), 1)
	started := time.Now()
	results := parallel(p.ctx, candidates, p.settings.HTTPSConcurrency, func(ctx context.Context, result model.ProbeResult) model.ProbeResult {
		result.HTTPS = p.dependencies.HTTPS.Probe(ctx, result.Candidate, 1, func() { p.progress.attempt("retained") })
		return result
	}, func(result model.ProbeResult) {
		passed := result.HTTPS.SuccessRate >= p.settings.HTTPSMinSuccessRate
		p.progress.advance("retained", passed, nodeFailure(passed, result.HTTPS.Failures, model.FailureTLS))
	})
	passed := countIf(results, func(result model.ProbeResult) bool { return result.HTTPS.SuccessRate >= p.settings.HTTPSMinSuccessRate })
	p.progress.finishAt("retained", len(candidates), passed, len(candidates)-passed, started)
	return results
}

func (p stagePipeline) bandwidth(retainedResults, freshResults []model.ProbeResult) []model.ProbeResult {
	target := min(p.settings.BandwidthCandidates, len(freshResults))
	p.progress.startProbe("bandwidth", len(retainedResults)+target, 1)
	started := time.Now()
	probe := func(batch []model.ProbeResult) []model.ProbeResult {
		return parallel(p.ctx, batch, p.settings.BandwidthConcurrency, func(ctx context.Context, result model.ProbeResult) model.ProbeResult {
			result.Bandwidth = p.dependencies.Bandwidth.Probe(ctx, result.Candidate)
			p.progress.attempt("bandwidth")
			return result
		}, func(result model.ProbeResult) {
			failures := map[model.FailureReason]int{}
			if result.Bandwidth.Failure != "" {
				failures[result.Bandwidth.Failure] = 1
			}
			p.progress.advance("bandwidth", bandwidthPassed(result), failures)
		})
	}
	results := probe(retainedResults)
	passed := countIf(results, bandwidthPassed)
	freshPassed, next := 0, 0
	for freshPassed < target && next < len(freshResults) && p.ctx.Err() == nil {
		batchSize := min(target-freshPassed, len(freshResults)-next)
		if next > 0 {
			p.progress.extendProbe("bandwidth", batchSize, 1)
		}
		batchResults := probe(freshResults[next : next+batchSize])
		next += batchSize
		results = append(results, batchResults...)
		freshPassed += countIf(batchResults, bandwidthPassed)
	}
	passed += freshPassed
	p.progress.finishAt("bandwidth", len(results), passed, len(results)-passed, started)
	return results
}

func (p stagePipeline) ranking(results []model.ProbeResult) []model.ProbeResult {
	input := len(results)
	p.progress.start("ranking", input)
	started := time.Now()
	results = ranking.Score(results, p.settings.TCPMinSuccessRate, p.settings.HTTPSMinSuccessRate, p.settings.ScoreWeights)
	if len(results) > p.settings.FinalResultCount {
		results = results[:p.settings.FinalResultCount]
	}
	p.progress.finishAt("ranking", input, len(results), input-len(results), started)
	return results
}

func checkpointForRun(runID, parentRunID string, settings config.Settings, sources []source.HTTPSource, network NetworkSnapshot, startStage string, base *Checkpoint) Checkpoint {
	checkpoint := Checkpoint{
		Version: CheckpointVersion, RunID: runID, ParentRunID: parentRunID, Network: network, Settings: settings,
		Sources: append([]source.HTTPSource(nil), sources...), CompletedAt: map[string]time.Time{}, StageSettings: map[string]config.Settings{}, StageNetworks: map[string]NetworkSnapshot{},
	}
	if base == nil || startStage == StageSource {
		return checkpoint
	}
	checkpoint.Sources = append([]source.HTTPSource(nil), base.Sources...)
	checkpoint.Candidates = append([]model.Candidate(nil), base.Candidates...)
	for name, completedAt := range base.CompletedAt {
		checkpoint.CompletedAt[name] = completedAt
	}
	for name, stageSettings := range base.StageSettings {
		checkpoint.StageSettings[name] = stageSettings
	}
	for name, stageNetwork := range base.StageNetworks {
		checkpoint.StageNetworks[name] = stageNetwork
	}
	if startStage == StageHTTPS || startStage == StageBandwidth || startStage == StageRanking {
		checkpoint.TCPResults = append([]model.ProbeResult(nil), base.TCPResults...)
	}
	if startStage == StageBandwidth || startStage == StageRanking {
		checkpoint.HTTPSResults = append([]model.ProbeResult(nil), base.HTTPSResults...)
	}
	if startStage == StageRanking {
		checkpoint.BandwidthResults = append([]model.ProbeResult(nil), base.BandwidthResults...)
	}
	return checkpoint
}

func checkpointStage(checkpoint *Checkpoint, name string, input int) model.StageProgress {
	if checkpoint != nil {
		if stage, ok := checkpoint.Stage(name); ok {
			return stage
		}
	}
	return model.StageProgress{Name: name, Input: input}
}

func completedTime(checkpoint *Checkpoint, name string) time.Time {
	if checkpoint == nil {
		return time.Time{}
	}
	return checkpoint.CompletedAt[name]
}

func reusedStages(startStage string) []string {
	switch startStage {
	case StageTCP:
		return []string{"source"}
	case StageHTTPS:
		return []string{"source", "filter", "tcp"}
	case StageBandwidth:
		return []string{"source", "filter", "tcp", "https", "retained"}
	case StageRanking:
		return []string{"source", "filter", "tcp", "https", "retained", "bandwidth"}
	default:
		return []string{}
	}
}

func filterCandidates(candidates []model.Candidate, settings config.Settings, failures map[model.FailureReason]int) []model.Candidate {
	return filter(candidates, func(candidate model.Candidate) bool {
		if !settings.AllowsPort(candidate.Port) {
			failures[model.FailurePortFiltered]++
			return false
		}
		if !settings.AllowsCountry(candidate.Country) {
			failures[model.FailureCountryFiltered]++
			return false
		}
		return true
	})
}

func filterTCPResults(results []model.ProbeResult, settings config.Settings) []model.ProbeResult {
	results = filter(results, func(result model.ProbeResult) bool {
		return settings.AllowsPort(result.Candidate.Port) && settings.AllowsCountry(result.Candidate.Country) && result.TCP.SuccessRate >= settings.TCPMinSuccessRate
	})
	for index := range results {
		results[index].HTTPS = model.ProbeStats{}
		results[index].Bandwidth = model.BandwidthStats{}
		results[index].Score = 0
		results[index].Parts = model.ScoreParts{}
		results[index].Status = ""
	}
	slices.SortStableFunc(results, compareTCP)
	return results
}

func filterHTTPSResults(results []model.ProbeResult, settings config.Settings) []model.ProbeResult {
	results = filter(results, func(result model.ProbeResult) bool {
		return settings.AllowsPort(result.Candidate.Port) && settings.AllowsCountry(result.Candidate.Country) && result.TCP.SuccessRate >= settings.TCPMinSuccessRate && result.HTTPS.SuccessRate >= settings.HTTPSMinSuccessRate
	})
	for index := range results {
		results[index].Bandwidth = model.BandwidthStats{}
		results[index].Score = 0
		results[index].Parts = model.ScoreParts{}
		results[index].Status = ""
	}
	slices.SortStableFunc(results, compareHTTPS)
	return results
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
func bandwidthPassed(result model.ProbeResult) bool {
	return result.Bandwidth.Mbps > 0 && result.Bandwidth.Failure == ""
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

func prepareRetained(items []model.ProbeResult, fresh []model.Candidate, settings config.Settings) ([]model.ProbeResult, map[string]bool, map[model.FailureReason]int) {
	freshByKey := make(map[string]model.Candidate, len(fresh))
	for _, candidate := range fresh {
		freshByKey[candidate.Key()] = candidate
	}
	seen := make(map[string]bool, len(items))
	retainedKeys := make(map[string]bool, len(items))
	prepared := make([]model.ProbeResult, 0, len(items))
	failures := map[model.FailureReason]int{}
	for _, result := range items {
		key := result.Candidate.Key()
		if seen[key] {
			continue
		}
		seen[key] = true
		retainedKeys[key] = true
		if candidate, ok := freshByKey[key]; ok {
			result.Candidate = candidate
		}
		if !settings.AllowsPort(result.Candidate.Port) {
			failures[model.FailurePortFiltered]++
			continue
		}
		if !settings.AllowsCountry(result.Candidate.Country) {
			failures[model.FailureCountryFiltered]++
			continue
		}
		if result.TCP.SuccessRate < settings.TCPMinSuccessRate {
			failures[model.FailureTCP]++
			continue
		}
		result.HTTPS = model.ProbeStats{}
		result.Bandwidth = model.BandwidthStats{}
		result.Score = 0
		result.Parts = model.ScoreParts{}
		result.Status = ""
		prepared = append(prepared, result)
	}
	return prepared, retainedKeys, failures
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
	emitMu       sync.Mutex
	value        model.RunProgress
	emit         func(model.RunProgress)
	stageStarts  map[string]time.Time
	stageUpdates map[string]int
	stop         chan struct{}
	done         chan struct{}
	stopOnce     sync.Once
}

func newProgress(id string, started time.Time, emit func(model.RunProgress)) *progressTracker {
	progress := &progressTracker{
		value:        model.RunProgress{RunID: id, State: "running", StartedAt: started, Failures: map[model.FailureReason]int{}},
		emit:         emit,
		stageStarts:  map[string]time.Time{},
		stageUpdates: map[string]int{},
	}
	if emit != nil {
		progress.stop = make(chan struct{})
		progress.done = make(chan struct{})
		go progress.heartbeat()
	}
	return progress
}
func (p *progressTracker) start(name string, input int) {
	p.startProbe(name, input, 0)
}
func (p *progressTracker) startProbe(name string, input, attemptsPerInput int) {
	p.mu.Lock()
	p.stageStarts[name] = time.Now()
	p.value.Stages = append(p.value.Stages, model.StageProgress{Name: name, Input: input, AttemptsTotal: input * attemptsPerInput, State: "running"})
	p.mu.Unlock()
	p.send()
}
func (p *progressTracker) reuse(stage model.StageProgress, measuredAt time.Time) {
	p.mu.Lock()
	stage.State = "reused"
	stage.MeasuredAt = measuredAt
	p.value.Stages = append(p.value.Stages, stage)
	p.mu.Unlock()
	p.send()
}
func (p *progressTracker) skip(name string) {
	p.mu.Lock()
	p.value.Stages = append(p.value.Stages, model.StageProgress{Name: name, State: "skipped"})
	p.mu.Unlock()
	p.send()
}
func (p *progressTracker) snapshot() []model.StageProgress {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]model.StageProgress(nil), p.value.Stages...)
}
func (p *progressTracker) setCheckpointWarning(message string) {
	p.mu.Lock()
	p.value.CheckpointWarning = message
	p.mu.Unlock()
	p.send()
}
func (p *progressTracker) extendProbe(name string, input, attemptsPerInput int) {
	p.mu.Lock()
	for i := range p.value.Stages {
		if p.value.Stages[i].Name == name {
			p.value.Stages[i].Input += input
			p.value.Stages[i].AttemptsTotal += input * attemptsPerInput
			break
		}
	}
	p.mu.Unlock()
	p.send()
}
func (p *progressTracker) finish(name string, input, passed, failed int) {
	p.finishAt(name, input, passed, failed, p.stageStarts[name])
}
func (p *progressTracker) finishAt(name string, input, passed, failed int, started time.Time) {
	p.mu.Lock()
	for i := range p.value.Stages {
		if p.value.Stages[i].Name == name {
			p.value.Stages[i].Input = input
			p.value.Stages[i].Passed = passed
			p.value.Stages[i].Failed = failed
			p.value.Stages[i].DurationMS = time.Since(started).Milliseconds()
			p.value.Stages[i].State = "completed"
		}
	}
	p.mu.Unlock()
	p.send()
}
func (p *progressTracker) advance(name string, passed bool, failures map[model.FailureReason]int) {
	p.mu.Lock()
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
	firstUpdate := p.stageUpdates[name] == 1
	p.mu.Unlock()
	if firstUpdate {
		p.send()
	}
}
func (p *progressTracker) attempt(name string) {
	p.mu.Lock()
	firstAttempt := false
	for i := range p.value.Stages {
		if p.value.Stages[i].Name == name {
			p.value.Stages[i].AttemptsCompleted++
			firstAttempt = p.value.Stages[i].AttemptsCompleted == 1
			break
		}
	}
	p.mu.Unlock()
	if firstAttempt {
		p.send()
	}
}
func (p *progressTracker) addFailures(failures map[model.FailureReason]int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for reason, count := range failures {
		p.value.Failures[reason] += count
	}
}
func (p *progressTracker) heartbeat() {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer func() {
		ticker.Stop()
		close(p.done)
	}()
	for {
		select {
		case <-ticker.C:
			p.send()
		case <-p.stop:
			return
		}
	}
}
func (p *progressTracker) stopHeartbeat() {
	if p.stop == nil {
		return
	}
	p.stopOnce.Do(func() {
		close(p.stop)
		<-p.done
	})
}
func (p *progressTracker) send() {
	if p.emit == nil {
		return
	}
	p.emitMu.Lock()
	defer p.emitMu.Unlock()
	p.mu.Lock()
	for i := range p.value.Stages {
		if p.value.Stages[i].State == "running" {
			p.value.Stages[i].DurationMS = time.Since(p.stageStarts[p.value.Stages[i].Name]).Milliseconds()
		}
	}
	copyValue := p.value
	copyValue.Stages = append([]model.StageProgress(nil), p.value.Stages...)
	copyValue.Failures = map[model.FailureReason]int{}
	for k, v := range p.value.Failures {
		copyValue.Failures[k] = v
	}
	p.mu.Unlock()
	p.emit(copyValue)
}
func (p *progressTracker) summary(started time.Time, results []model.ProbeResult, state string) model.RunSummary {
	p.stopHeartbeat()
	p.mu.Lock()
	if results == nil {
		results = []model.ProbeResult{}
	}
	p.value.State = state
	p.value.Message = fmt.Sprintf("%d 个结果", len(results))
	if state == "cancelled" {
		for index := range p.value.Stages {
			if p.value.Stages[index].State == "running" {
				p.value.Stages[index].State = "cancelled"
			}
		}
	}
	runID := p.value.RunID
	checkpointWarning := p.value.CheckpointWarning
	failures := map[model.FailureReason]int{}
	for k, v := range p.value.Failures {
		failures[k] = v
	}
	p.mu.Unlock()
	p.send()
	return model.RunSummary{RunID: runID, StartedAt: started, FinishedAt: time.Now(), State: state, Results: results, Failures: failures, CheckpointWarning: checkpointWarning}
}
