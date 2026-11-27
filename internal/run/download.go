package run

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
	"github.com/linvva/cf-node-bench/internal/source"
)

type DownloadCheckpoint struct {
	Measurements map[string]model.BandwidthStats `json:"measurements"`
	Finished     map[string]bool                 `json:"finished"`
	Pins         []string                        `json:"pins,omitempty"`
}

type downloadJournal struct {
	mu       sync.Mutex
	data     *DownloadCheckpoint
	save     func()
	lastSave time.Time
}

func (p *stagePipeline) openDownload(c *Checkpoint, base *Checkpoint, resume bool, save func()) {
	var data DownloadCheckpoint
	if resume {
		data.Measurements = maps.Clone(base.Download.Measurements)
		data.Finished = maps.Clone(base.Download.Finished)
	}
	if data.Measurements == nil {
		data.Measurements = map[string]model.BandwidthStats{}
	}
	if data.Finished == nil {
		data.Finished = map[string]bool{}
	}
	data.Pins = slices.Sorted(maps.Keys(p.pinned))
	c.Download = &data
	p.download = &downloadJournal{data: &data, save: save}
}

func (j *downloadJournal) measured(key string) (model.BandwidthStats, bool) {
	if j == nil {
		return model.BandwidthStats{}, false
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	value, ok := j.data.Measurements[key]
	return value, ok
}

func (j *downloadJournal) finished(key string) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.data.Finished[key]
}

func (j *downloadJournal) record(key string, stats model.BandwidthStats, logical bool) {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.data.Measurements[key] = stats
	if logical {
		j.data.Finished[key] = true
	}
	j.savePeriodically()
}

func (j *downloadJournal) finish(key string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.data.Finished[key] = true
	j.savePeriodically()
}

func (j *downloadJournal) savePeriodically() {
	if time.Since(j.lastSave) >= 2*time.Second {
		j.save()
		j.lastSave = time.Now()
	}
}

func endDownload(ctx context.Context, c *Checkpoint, save func()) {
	if c.Download == nil {
		return
	}
	if ctx.Err() == nil {
		c.Download = nil
		return
	}
	if errors.Is(context.Cause(ctx), errDiscardRun) {
		c.Download = nil
	}
	save()
}

func DownloadResumeInfo(c Checkpoint, settings config.Settings, network NetworkSnapshot, sources []source.HTTPSource, pins []model.Candidate, now time.Time) *ResumeOption {
	if c.RunID == "" || c.Download == nil {
		return nil
	}
	option := &ResumeOption{Stage: StageBandwidth, Available: true, Input: len(c.Download.Measurements), MeasuredAt: c.UpdatedAt, Warnings: []string{}}
	// Ranking settings can be changed independently of the saved measurements.
	previous := c.Settings
	for _, pair := range [][2]*[]string{{&previous.AllowedCountries, &settings.AllowedCountries}, {&previous.BlockedCountries, &settings.BlockedCountries}} {
		if len(*pair[0]) == 0 && len(*pair[1]) == 0 {
			*pair[0], *pair[1] = nil, nil
		}
	}
	if len(previous.AllowedPorts) == 0 && len(settings.AllowedPorts) == 0 {
		previous.AllowedPorts, settings.AllowedPorts = nil, nil
	}
	previous.ScoreWeights, previous.FinalResultCount = settings.ScoreWeights, settings.FinalResultCount
	if !reflect.DeepEqual(previous, settings) {
		option.Available = false
		option.Warnings = append(option.Warnings, "测速设置已变化，请重新测量")
	}
	if c.Network != network {
		option.Available = false
		option.Warnings = append(option.Warnings, "网络接口或本机 IPv4 已变化，请重新测量")
	}
	if !c.LocalFiles && !sameEnabledSources(c.Sources, sources) || !slices.Equal(c.Download.Pins, slices.Sorted(maps.Keys(pinnedKeys(pins)))) {
		option.Available = false
		option.Warnings = append(option.Warnings, "数据源或固定节点已变化，请重新测量")
	}
	if option.Available && !c.UpdatedAt.IsZero() && now.Sub(c.UpdatedAt) > 30*time.Minute {
		option.Warnings = append(option.Warnings, "已保存的测量超过 30 分钟，继续测速会复用这些数据")
	}
	return option
}

func (p stagePipeline) downloadFresh(fresh []model.ProbeResult, previous model.StageQueue, measure func(context.Context, model.ProbeResult) model.ProbeResult, passed func(model.ProbeResult) bool) []model.ProbeResult {
	// Count completed logical nodes before scheduling work. A shared H3 IP alone
	// does not mean the other logical nodes using it were already scheduled.
	var reused, pending []model.ProbeResult
	for _, result := range fresh {
		if p.download != nil && p.download.finished(result.Candidate.Key()) {
			reused = append(reused, result)
		} else {
			pending = append(pending, result)
		}
	}
	results := parallel(p.ctx, reused, p.settings.BandwidthConcurrency, measure, nil)
	previous.Passed += countIf(results, passed)
	previous.Planned += len(results)
	previous.Pool += len(results)
	return append(results, parallelUntil(p.ctx, pending, p.settings.BandwidthConcurrency,
		p.settings.BandwidthCandidates-previous.Passed, p.settings.HTTPCandidateCount, measure, passed, func(queue model.StageQueue) {
			queue.Target = p.settings.BandwidthCandidates
			queue.Passed += previous.Passed
			queue.Planned += previous.Planned
			queue.Pool += previous.Pool
			p.progress.downloadQueue(StageBandwidth, queue)
		})...)
}
