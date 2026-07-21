package ranking

import (
	"math"
	"slices"

	"github.com/linvva/cf-node-bench/internal/model"
)

var weights = model.ScoreParts{TCP: 0.20, HTTPS: 0.20, Jitter: 0.10, Reliability: 0.30, Bandwidth: 0.20}

func Score(results []model.ProbeResult, tcpMinimumSuccessRate, httpsMinimumSuccessRate float64) []model.ProbeResult {
	eligible := make([]model.ProbeResult, 0, len(results))
	for _, result := range results {
		if result.TCP.SuccessRate < tcpMinimumSuccessRate || result.HTTPS.SuccessRate < httpsMinimumSuccessRate || result.Bandwidth.Mbps <= 0 {
			result.Status = "failed_gate"
			continue
		}
		eligible = append(eligible, result)
	}
	if len(eligible) == 0 {
		return eligible
	}
	tcpMin, tcpMax := extrema(eligible, func(r model.ProbeResult) float64 { return r.TCP.P95MS })
	httpsMin, httpsMax := extrema(eligible, func(r model.ProbeResult) float64 { return r.HTTPS.P95MS })
	jitterMin, jitterMax := extrema(eligible, func(r model.ProbeResult) float64 { return r.HTTPS.JitterMS })
	bandMin, bandMax := extrema(eligible, func(r model.ProbeResult) float64 { return r.Bandwidth.Mbps })
	for index := range eligible {
		result := &eligible[index]
		result.Parts = model.ScoreParts{
			TCP:         lowerBetter(result.TCP.P95MS, tcpMin, tcpMax) * 100,
			HTTPS:       lowerBetter(result.HTTPS.P95MS, httpsMin, httpsMax) * 100,
			Jitter:      lowerBetter(result.HTTPS.JitterMS, jitterMin, jitterMax) * 100,
			Reliability: ((result.TCP.SuccessRate + result.HTTPS.SuccessRate) / 2) * 100,
			Bandwidth:   higherBetter(result.Bandwidth.Mbps, bandMin, bandMax) * 100,
		}
		result.Score = round2(result.Parts.TCP*weights.TCP + result.Parts.HTTPS*weights.HTTPS + result.Parts.Jitter*weights.Jitter + result.Parts.Reliability*weights.Reliability + result.Parts.Bandwidth*weights.Bandwidth)
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

func extrema(results []model.ProbeResult, value func(model.ProbeResult) float64) (float64, float64) {
	minValue, maxValue := value(results[0]), value(results[0])
	for _, result := range results[1:] {
		current := value(result)
		minValue = math.Min(minValue, current)
		maxValue = math.Max(maxValue, current)
	}
	return minValue, maxValue
}

func lowerBetter(value, minValue, maxValue float64) float64 {
	if maxValue == minValue {
		return 1
	}
	return 1 - (value-minValue)/(maxValue-minValue)
}

func higherBetter(value, minValue, maxValue float64) float64 {
	if maxValue == minValue {
		return 1
	}
	return (value - minValue) / (maxValue - minValue)
}

func round2(value float64) float64 { return math.Round(value*100) / 100 }
