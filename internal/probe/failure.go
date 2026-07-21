package probe

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"

	"github.com/linvva/cf-node-bench/internal/model"
)

func classify(ctx context.Context, err error) model.FailureReason {
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return model.FailureCancelled
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return model.FailureTimeout
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return model.FailureDNS
	}
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "tls") || strings.Contains(text, "certificate") || strings.Contains(text, "x509") {
		return model.FailureTLS
	}
	return model.FailureTCP
}
