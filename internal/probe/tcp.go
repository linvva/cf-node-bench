package probe

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/linvva/cf-node-bench/internal/model"
)

type TCPProber struct{ Timeout time.Duration }

func (p TCPProber) Probe(ctx context.Context, candidate model.Candidate, attempts int) model.ProbeStats {
	samples := make([]float64, 0, attempts)
	failures := map[model.FailureReason]int{}
	dialer := net.Dialer{Timeout: p.Timeout}
	address := net.JoinHostPort(candidate.IP, strconv.Itoa(candidate.Port))
	for index := 0; index < attempts; index++ {
		started := time.Now()
		conn, err := dialer.DialContext(ctx, "tcp", address)
		if err != nil {
			failures[classify(ctx, err)]++
			if ctx.Err() != nil {
				break
			}
			continue
		}
		samples = append(samples, float64(time.Since(started).Microseconds())/1000)
		_ = conn.Close()
	}
	return Summarize(attempts, samples, failures)
}
