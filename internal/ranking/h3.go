package ranking

import (
	"slices"

	"github.com/linvva/cf-node-bench/internal/model"
)

func ScoreH3(results []model.ProbeResult, quicMinimumSuccessRate, http3MinimumSuccessRate float64, weights model.ScoreWeights) []model.ProbeResult {
	weights = weights.ForMode(model.ModeH3)
	eligible := make([]model.ProbeResult, 0, len(results))
	for _, result := range results {
		if result.QUIC.SuccessRate < quicMinimumSuccessRate || result.HTTP3.SuccessRate < http3MinimumSuccessRate || result.Bandwidth.Mbps <= 0 || result.Bandwidth.Failure != "" {
			continue
		}
		eligible = append(eligible, result)
	}
	if len(eligible) == 0 {
		return eligible
	}
	quicMin, quicMax := extrema(eligible, func(r model.ProbeResult) float64 { return r.QUIC.P95MS })
	httpMin, httpMax := extrema(eligible, func(r model.ProbeResult) float64 { return r.HTTP3.P95MS })
	jitterMin, jitterMax := extrema(eligible, func(r model.ProbeResult) float64 { return r.HTTP3.JitterMS })
	bandMin, bandMax := extrema(eligible, func(r model.ProbeResult) float64 { return r.Bandwidth.Mbps })
	for index := range eligible {
		result := &eligible[index]
		result.Mode = model.ModeH3
		result.Parts = model.ScoreParts{
			QUIC:        lowerBetter(result.QUIC.P95MS, quicMin, quicMax) * 100,
			HTTP3:       lowerBetter(result.HTTP3.P95MS, httpMin, httpMax) * 100,
			Jitter:      lowerBetter(result.HTTP3.JitterMS, jitterMin, jitterMax) * 100,
			Reliability: ((result.QUIC.SuccessRate + result.HTTP3.SuccessRate) / 2) * 100,
			Bandwidth:   higherBetter(result.Bandwidth.Mbps, bandMin, bandMax) * 100,
		}
		result.Score = round2((result.Parts.QUIC*float64(weights.QUIC) + result.Parts.HTTP3*float64(weights.HTTP3) + result.Parts.Jitter*float64(weights.Jitter) + result.Parts.Reliability*float64(weights.Reliability) + result.Parts.Bandwidth*float64(weights.Bandwidth)) / 100)
		result.Status = "qualified"
	}
	slices.SortStableFunc(eligible, func(a, b model.ProbeResult) int {
		if a.Score > b.Score {
			return -1
		}
		if a.Score < b.Score {
			return 1
		}
		return 0
	})
	return eligible
}
