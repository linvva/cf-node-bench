package probe

import (
	"context"
	"errors"
	"io"
	"math"
	"time"
)

var errBandwidthBelowMinimum = errors.New("带宽未达门槛")

type BandwidthThreshold struct {
	MinMbps  float64
	LineMbps float64
}

func (t BandwidthThreshold) withDeadline(ctx context.Context, started time.Time, maxBytes int64) (context.Context, context.CancelFunc) {
	if deadline, ok := ctx.Deadline(); ok && t.MinMbps > 0 {
		nanos := math.Ceil(float64(maxBytes) * 8 / 1e6 / t.MinMbps * float64(time.Second))
		if nanos < float64(deadline.Sub(started)) {
			return context.WithDeadlineCause(ctx, started.Add(time.Duration(max(1, nanos))), errBandwidthBelowMinimum)
		}
	}
	return ctx, func() {}
}

func (t BandwidthThreshold) read(ctx context.Context, body io.Reader, started time.Time, maxBytes int64) (int64, error) {
	if t.MinMbps == 0 || t.LineMbps == 0 {
		return io.Copy(io.Discard, io.LimitReader(body, maxBytes))
	}
	deadline, _ := ctx.Deadline()
	buffer := make([]byte, 32*1024)
	var received int64
	for received < maxBytes {
		n, err := body.Read(buffer[:min(int64(len(buffer)), maxBytes-received)])
		received += int64(n)
		if errors.Is(err, io.EOF) {
			return received, nil
		}
		if err != nil {
			return received, err
		}
		now := time.Now()
		if received < maxBytes && n > 0 && ctx.Err() == nil &&
			t.cannotReach(received, maxBytes, now.Sub(started), deadline.Sub(now)) {
			return received, errBandwidthBelowMinimum
		}
	}
	return received, nil
}

func (t BandwidthThreshold) cannotReach(received, maxBytes int64, elapsed, remaining time.Duration) bool {
	downloaded := float64(received) * 8 / 1e6
	seconds := elapsed.Seconds()
	if downloaded >= t.MinMbps*seconds {
		return false
	}
	// With LineMbps >= MinMbps, recovering samples benefit from a full-speed
	// transfer for as long as both the time and byte budgets permit.
	bytesLeft := float64(maxBytes-received) * 8 / 1e6
	futureSeconds := min(max(0, remaining.Seconds()), bytesLeft/t.LineMbps)
	return downloaded+t.LineMbps*futureSeconds < t.MinMbps*(seconds+futureSeconds)
}

func bandwidthCutShort(parent, request context.Context, err error) bool {
	return err != nil && parent.Err() == nil &&
		(errors.Is(err, errBandwidthBelowMinimum) || errors.Is(context.Cause(request), errBandwidthBelowMinimum))
}
