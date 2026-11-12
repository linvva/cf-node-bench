package probe

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/linvva/cf-node-bench/internal/model"
)

type TCPProber struct {
	Timeout        time.Duration
	MinSuccessRate float64
}

func (p TCPProber) Probe(ctx context.Context, candidate model.Candidate, attempts int, onAttempt func()) model.ProbeStats {
	samples := make([]float64, 0, attempts)
	failures := map[model.FailureReason]int{}
	dialer := net.Dialer{Timeout: p.Timeout}
	address := net.JoinHostPort(candidate.IP, strconv.Itoa(candidate.Port))
	completed, skipped := 0, 0
	for completed < attempts && ctx.Err() == nil {
		started := time.Now()
		conn, err := dialer.DialContext(ctx, "tcp", address)
		if err != nil {
			failures[classify(ctx, err)]++
		} else {
			samples = append(samples, float64(time.Since(started).Microseconds())/1000)
			_ = conn.Close()
		}
		completed++
		if onAttempt != nil {
			onAttempt()
		}
		if ctx.Err() == nil && cannotReachSuccessRate(attempts, completed, len(samples), p.MinSuccessRate) {
			skipped = attempts - completed
			break
		}
	}
	stats := Summarize(completed, samples, failures)
	stats.Skipped = skipped
	return stats
}
