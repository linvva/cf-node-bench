package probe

import (
	"math"
	"slices"

	"github.com/linvva/cf-node-bench/internal/model"
)

func Summarize(attempts int, samples []float64, failures map[model.FailureReason]int) model.ProbeStats {
	ordered := slices.Clone(samples)
	slices.Sort(ordered)
	stats := model.ProbeStats{Attempts: attempts, Successes: len(samples), Failures: failures, SamplesMS: ordered}
	if attempts > 0 {
		stats.SuccessRate = float64(len(samples)) / float64(attempts)
	}
	if len(ordered) == 0 {
		return stats
	}
	for _, value := range ordered {
		stats.AverageMS += value
	}
	stats.AverageMS /= float64(len(ordered))
	stats.P50MS = percentile(ordered, 0.50)
	stats.P95MS = percentile(ordered, 0.95)
	if len(ordered) > 1 {
		var total float64
		for index := 1; index < len(ordered); index++ {
			total += math.Abs(ordered[index] - ordered[index-1])
		}
		stats.JitterMS = total / float64(len(ordered)-1)
	}
	return stats
}

func percentile(values []float64, quantile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	position := quantile * float64(len(values)-1)
	lower := int(math.Floor(position))
	upper := int(math.Ceil(position))
	if lower == upper {
		return values[lower]
	}
	fraction := position - float64(lower)
	return values[lower] + (values[upper]-values[lower])*fraction
}
