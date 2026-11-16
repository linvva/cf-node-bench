package run

import (
	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
	"github.com/linvva/cf-node-bench/internal/ranking"
)

func pinnedKeys(candidates []model.Candidate) map[string]bool {
	keys := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		keys[candidate.Key()] = true
	}
	return keys
}

func mergePinnedCandidates(candidates, pinned []model.Candidate) []model.Candidate {
	// Keep current source metadata when the same node is present in both lists.
	return unique(append(append([]model.Candidate(nil), candidates...), pinned...))
}

func markPinnedResults(results []model.ProbeResult, keys map[string]bool) []model.ProbeResult {
	marked := append([]model.ProbeResult(nil), results...)
	for index := range marked {
		marked[index].Pinned = keys[marked[index].Candidate.Key()]
	}
	return marked
}

func rankPinnedResults(results []model.ProbeResult, settings config.Settings) []model.ProbeResult {
	eligible := filter(append([]model.ProbeResult(nil), results...), func(r model.ProbeResult) bool {
		return (r.Pinned || settings.AllowsPort(r.Candidate.Port) && settings.AllowsCountry(r.Candidate.Country)) && bandwidthPassed(r)
	})
	var ranked []model.ProbeResult
	if settings.RequireH3 {
		ranked = ranking.ScoreH3(eligible, settings.ConnectionMinSuccessRate, settings.HTTPMinSuccessRate, settings.ScoreWeights)
	} else {
		ranked = ranking.Score(eligible, settings.ConnectionMinSuccessRate, settings.HTTPMinSuccessRate, settings.ScoreWeights)
	}
	seen := map[string]bool{}
	fresh := 0
	ranked = filter(ranked, func(r model.ProbeResult) bool {
		if !r.Pinned {
			if fresh >= settings.FinalResultCount {
				return false
			}
			fresh++
		}
		seen[r.Candidate.Key()] = true
		return true
	})
	for _, result := range results {
		if result.Pinned && !seen[result.Candidate.Key()] {
			result.Score, result.Parts, result.Status = 0, model.ScoreParts{}, "failed_gate"
			ranked = append(ranked, result)
			seen[result.Candidate.Key()] = true
		}
	}
	return ranked
}

// ApplyPinnedResumeInfo prevents a newly pinned node from disappearing when an
// older checkpoint lacks the measurements that the selected start would reuse.
func ApplyPinnedResumeInfo(info ResumeInfo, checkpoint Checkpoint, pinned []model.Candidate) ResumeInfo {
	info.Options = append([]ResumeOption(nil), info.Options...)
	for index := range info.Options {
		option := &info.Options[index]
		var measured []model.Candidate
		var results []model.ProbeResult
		switch option.Stage {
		case StageTCP, StageQUIC:
			measured = checkpoint.Candidates
		case StageHTTPS:
			results = checkpoint.TCPResults
		case StageHTTP3:
			results = checkpoint.QUICResults
		case StageBandwidth:
			results = checkpoint.HTTPSResults
			if checkpoint.Settings.RequireH3 {
				results = checkpoint.HTTP3Results
			}
		case StageRanking:
			results = checkpoint.BandwidthResults
		}
		keys := pinnedKeys(measured)
		for _, result := range results {
			keys[result.Candidate.Key()] = true
		}
		for _, candidate := range pinned {
			if !keys[candidate.Key()] {
				option.Available = false
				option.Warnings = append(append([]string(nil), option.Warnings...), "检查点缺少固化节点的前置测量，请从更早阶段或全量测速开始")
				break
			}
		}
	}
	return info
}
