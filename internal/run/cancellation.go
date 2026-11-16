package run

import (
	"context"
	"errors"
	"slices"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
)

var errKeepCancelledResults = errors.New("保留取消时已完成的测速结果")

// recordCompletedResult is called only after the whole logical node has finished
// bandwidth measurement, or when reusing a complete bandwidth checkpoint.
func (p *progressTracker) recordCompletedResult(result model.ProbeResult, settings config.Settings) {
	if result.Bandwidth.Failure == model.FailureCancelled {
		return
	}
	connection, http := result.TCP, result.HTTPS
	if settings.RequireH3 {
		connection, http = result.QUIC, result.HTTP3
		if len(result.Endpoints) == 0 {
			return
		}
		for _, endpoint := range result.Endpoints {
			if endpoint.QUIC.Failures[model.FailureCancelled] > 0 || endpoint.HTTP3.Failures[model.FailureCancelled] > 0 || endpoint.Bandwidth.Failure == model.FailureCancelled {
				return
			}
		}
	}
	if connection.Failures[model.FailureCancelled] > 0 || http.Failures[model.FailureCancelled] > 0 {
		return
	}
	qualified := connection.SuccessRate >= settings.ConnectionMinSuccessRate && http.SuccessRate >= settings.HTTPMinSuccessRate && bandwidthPassed(result) && (result.Pinned || settings.AllowsPort(result.Candidate.Port) && settings.AllowsCountry(result.Candidate.Country))
	if !qualified && !result.Pinned {
		return
	}
	p.mu.Lock()
	if p.completedKeys == nil {
		p.completedKeys = map[string]bool{}
	}
	if p.completedKeys[result.Candidate.Key()] {
		p.mu.Unlock()
		return
	}
	p.completedKeys[result.Candidate.Key()] = true
	result.Endpoints = slices.Clone(result.Endpoints)
	p.completedResults = append(p.completedResults, result)
	if qualified {
		p.value.AvailableResults++
	}
	p.mu.Unlock()
	p.send()
}

func (p *progressTracker) cancelledResults(ctx context.Context, settings config.Settings) []model.ProbeResult {
	if !errors.Is(context.Cause(ctx), errKeepCancelledResults) {
		return nil
	}
	p.mu.Lock()
	results := slices.Clone(p.completedResults)
	available := p.value.AvailableResults
	p.mu.Unlock()
	if available == 0 {
		return nil
	}
	return rankPinnedResults(results, settings)
}
