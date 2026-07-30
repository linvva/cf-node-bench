package publish

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/linvva/cf-node-bench/internal/model"
)

var (
	ErrAlreadyPublishing = errors.New("该测速结果正在发布")
	ErrNoTargets         = errors.New("没有已启用的发布目标")
)

type Update struct {
	RunID  string                  `json:"runId"`
	Result model.PublicationResult `json:"result"`
}

type publishJob struct {
	ctx      context.Context
	summary  model.RunSummary
	settings Settings
	target   string
}

type Coordinator struct {
	service *Service
	update  func(Update)

	mu      sync.Mutex
	queue   []publishJob
	working bool
	pending map[string]bool
}

func NewCoordinator(service *Service, update func(Update)) *Coordinator {
	return &Coordinator{service: service, update: update, pending: map[string]bool{}}
}

func (c *Coordinator) Enqueue(ctx context.Context, summary model.RunSummary, settings Settings, target string) error {
	if target == "all" && !settings.Cloudflare.Enabled && !settings.GitHub.Enabled && !settings.Telegram.Enabled {
		return ErrNoTargets
	}
	if target != "all" && !targetEnabled(settings, target) {
		return ErrNoTargets
	}
	c.mu.Lock()
	if c.pending[summary.RunID] {
		c.mu.Unlock()
		return ErrAlreadyPublishing
	}
	c.pending[summary.RunID] = true
	c.queue = append(c.queue, publishJob{ctx: ctx, summary: summary, settings: settings, target: target})
	startWorker := !c.working
	c.working = true
	c.mu.Unlock()

	for _, current := range jobTargets(settings, target) {
		c.emit(summary.RunID, model.PublicationResult{Target: current, State: "queued"})
	}
	if startWorker {
		go c.work()
	}
	return nil
}

func (c *Coordinator) work() {
	for {
		c.mu.Lock()
		if len(c.queue) == 0 {
			c.working = false
			c.mu.Unlock()
			return
		}
		job := c.queue[0]
		c.queue = c.queue[1:]
		c.mu.Unlock()

		c.run(job)
		c.mu.Lock()
		delete(c.pending, job.summary.RunID)
		c.mu.Unlock()
	}
}

func (c *Coordinator) run(job publishJob) {
	if job.target != "all" {
		outcomes := publicationMap(job.summary.Publications)
		result := c.runTarget(job, job.target, outcomes)
		c.emit(job.summary.RunID, result)
		return
	}

	outcomes := publicationMap(job.summary.Publications)
	var mu sync.Mutex
	var wait sync.WaitGroup
	for _, target := range []string{"cloudflare", "github"} {
		if !targetEnabled(job.settings, target) {
			continue
		}
		wait.Add(1)
		go func(current string) {
			defer wait.Done()
			result := c.runTarget(job, current, nil)
			mu.Lock()
			outcomes[current] = result
			mu.Unlock()
			c.emit(job.summary.RunID, result)
		}(target)
	}
	wait.Wait()
	if job.settings.Telegram.Enabled {
		result := c.runTarget(job, "telegram", outcomes)
		c.emit(job.summary.RunID, result)
	}
}

func (c *Coordinator) runTarget(job publishJob, target string, outcomes map[string]model.PublicationResult) model.PublicationResult {
	c.emit(job.summary.RunID, model.PublicationResult{Target: target, State: "running", StartedAt: time.Now()})
	switch target {
	case "cloudflare":
		return c.service.PublishCloudflare(job.ctx, job.summary, job.settings)
	case "github":
		return c.service.PublishGitHub(job.ctx, job.summary, job.settings)
	case "telegram":
		return c.service.PublishTelegram(job.ctx, job.summary, job.settings, outcomes)
	default:
		return model.PublicationResult{Target: target, State: "failed", Message: "未知发布目标", FinishedAt: time.Now()}
	}
}

func (c *Coordinator) emit(runID string, result model.PublicationResult) {
	if c.update != nil {
		c.update(Update{RunID: runID, Result: result})
	}
}

func targetEnabled(settings Settings, target string) bool {
	switch target {
	case "cloudflare":
		return settings.Cloudflare.Enabled
	case "github":
		return settings.GitHub.Enabled
	case "telegram":
		return settings.Telegram.Enabled
	default:
		return false
	}
}

func jobTargets(settings Settings, target string) []string {
	if target != "all" {
		return []string{target}
	}
	targets := []string{}
	for _, current := range []string{"cloudflare", "github", "telegram"} {
		if targetEnabled(settings, current) {
			targets = append(targets, current)
		}
	}
	return targets
}

func publicationMap(results []model.PublicationResult) map[string]model.PublicationResult {
	outcomes := make(map[string]model.PublicationResult, len(results))
	for _, result := range results {
		outcomes[result.Target] = result
	}
	return outcomes
}
