package run

import "context"

// parallelUntil admits candidates in input order and reserves a place for every
// in-flight result. Failures free a place immediately; successes cannot exceed
// the target even when the remaining probes all pass.
func parallelUntil[I any, O any](ctx context.Context, input []I, concurrency, target int, work func(context.Context, I) O, completed func(O) bool) []O {
	type measurement struct {
		index int
		value O
	}
	results := make(chan measurement, min(concurrency, len(input)))
	output := make([]O, 0)
	next, active, passed := 0, 0, 0
	for {
		for ctx.Err() == nil && next < len(input) && active < concurrency && passed+active < target {
			index := next
			next++
			active++
			var empty O
			output = append(output, empty)
			go func() { results <- measurement{index, work(ctx, input[index])} }()
		}
		if active == 0 {
			return output
		}
		result := <-results
		active--
		output[result.index] = result.value
		if completed(result.value) {
			passed++
		}
	}
}
