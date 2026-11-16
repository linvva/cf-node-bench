package run

import (
	"context"
	"math"

	"github.com/linvva/cf-node-bench/internal/model"
)

// parallelUntil queues candidates in ranked order and estimates replenishment from
// the observed pass rate, drawing more candidates in batches as needed. Keep
// workers busy until the success target is reached, then drain in-flight work.
func parallelUntil[I any, O any](ctx context.Context, input []I, concurrency, target, batchSize int, work func(context.Context, I) O, completed func(O) bool, update func(model.StageQueue)) []O {
	type measurement struct {
		index int
		value O
	}
	concurrency = min(max(1, concurrency), len(input))
	target = max(0, target)
	batchSize = max(1, batchSize)
	pool := min(batchSize, len(input))
	results := make(chan measurement, concurrency)
	output := make([]O, 0)
	next, active, passed, finished := 0, 0, 0, 0
	planned := min(target, pool)
	notify := func() {
		pending := planned - next
		if ctx.Err() != nil || passed >= target {
			pending = 0
		}
		if update != nil {
			update(model.StageQueue{Pool: pool, Target: target, Planned: planned, Active: active, Pending: pending, Passed: passed})
		}
	}
	notify()
	for {
		desired := planned
		if ctx.Err() == nil && passed < target {
			rate := 1.0
			if finished > 0 {
				if passed == 0 {
					// One hypothetical success keeps an all-failure sample finite.
					rate = 1 / float64(finished+1)
				} else {
					rate = float64(passed) / float64(finished)
				}
			}
			outstanding := max(concurrency, int(math.Ceil(float64(target-passed)/rate)))
			desired = min(len(input), max(planned, next+outstanding-active))
			planned = min(pool, desired)
		}
		for ctx.Err() == nil && passed < target && next < desired && active < concurrency {
			if next == pool {
				pool = min(len(input), pool+batchSize)
				planned = min(pool, desired)
			}
			index := next
			next++
			active++
			var empty O
			output = append(output, empty)
			go func() { results <- measurement{index, work(ctx, input[index])} }()
			notify()
		}
		notify()
		if active == 0 {
			return output
		}
		result := <-results
		active--
		finished++
		output[result.index] = result.value
		if completed(result.value) {
			passed++
		}
	}
}
