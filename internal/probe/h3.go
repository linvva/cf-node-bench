package probe

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/linvva/cf-node-bench/internal/model"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

type QUICProber struct {
	Timeout    time.Duration
	WorkerHost string
	RootCAs    *x509.CertPool
}

func (p QUICProber) Probe(ctx context.Context, candidate model.Candidate, attempts int, onAttempt func()) model.ProbeStats {
	samples := make([]float64, 0, attempts)
	failures := map[model.FailureReason]int{}
	for index := 0; index < attempts && ctx.Err() == nil; index++ {
		attemptCtx, cancel := context.WithTimeout(ctx, p.Timeout)
		started := time.Now()
		var duration time.Duration
		dialer, err := newQUICDialer(candidate, p.Timeout)
		if err == nil {
			var conn *quic.Conn
			conn, err = dialer.dial(attemptCtx, h3TLS(p.WorkerHost, p.RootCAs), nil)
			duration = time.Since(started)
			if err == nil {
				_ = conn.CloseWithError(0, "")
			}
			dialer.close()
		}
		if err != nil {
			failures[classifyH3(attemptCtx, err)]++
		} else {
			samples = append(samples, milliseconds(duration))
		}
		cancel()
		if onAttempt != nil {
			onAttempt()
		}
	}
	return Summarize(attempts, samples, failures)
}

type HTTP3Prober struct {
	ConnectTimeout time.Duration
	RequestTimeout time.Duration
	WorkerHost     string
	RootCAs        *x509.CertPool
	Path           string
}

func (p HTTP3Prober) Probe(ctx context.Context, candidate model.Candidate, attempts int, onAttempt func()) model.ProbeStats {
	samples := make([]float64, 0, attempts)
	failures := map[model.FailureReason]int{}
	path := p.Path
	if path == "" {
		path = "/cdn-cgi/trace"
	}
	for index := 0; index < attempts && ctx.Err() == nil; index++ {
		attemptCtx, cancel := context.WithTimeout(ctx, p.RequestTimeout)
		started := time.Now()
		failure := p.attempt(attemptCtx, candidate, "https://"+p.WorkerHost+path)
		if failure != "" {
			failures[failure]++
		} else {
			samples = append(samples, milliseconds(time.Since(started)))
		}
		cancel()
		if onAttempt != nil {
			onAttempt()
		}
	}
	return Summarize(attempts, samples, failures)
}

func (p HTTP3Prober) attempt(ctx context.Context, candidate model.Candidate, target string) model.FailureReason {
	client, closeClient, err := newH3Client(candidate, p.ConnectTimeout, p.RootCAs)
	if err != nil {
		return classifyH3(ctx, err)
	}
	defer closeClient()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return model.FailureProtocol
	}
	resp, err := client.Do(req)
	if err != nil {
		return classifyH3(ctx, err)
	}
	defer resp.Body.Close()
	if failure := h3ResponseFailure(resp); failure != "" {
		return failure
	}
	_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return classifyH3(ctx, err)
	}
	return ""
}

type H3BandwidthProber struct {
	ConnectTimeout time.Duration
	TotalTimeout   time.Duration
	MaxBytes       int64
	RootCAs        *x509.CertPool
	URL            string
}

func (p H3BandwidthProber) Probe(ctx context.Context, candidate model.Candidate) model.BandwidthStats {
	testCtx, cancel := context.WithTimeout(ctx, p.TotalTimeout)
	defer cancel()
	target := p.URL
	if target == "" {
		target = fmt.Sprintf("https://h3.speed.cloudflare.com/__down?bytes=%d", p.MaxBytes)
	}
	req, err := http.NewRequestWithContext(testCtx, http.MethodGet, target, nil)
	if err != nil || req.URL.Scheme != "https" || req.URL.Hostname() == "" || req.URL.User != nil || req.URL.Fragment != "" {
		return model.BandwidthStats{Failure: model.FailureDownload}
	}
	client, closeClient, err := newH3Client(candidate, p.ConnectTimeout, p.RootCAs)
	if err != nil {
		return model.BandwidthStats{Failure: classifyH3(testCtx, err)}
	}
	defer closeClient()
	started := time.Now()
	var firstByte atomic.Int64
	req.Header.Set("Accept-Encoding", "identity")
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
		GotFirstResponseByte: func() { firstByte.CompareAndSwap(0, time.Since(started).Nanoseconds()) },
	}))
	resp, err := client.Do(req)
	if err != nil {
		return model.BandwidthStats{Failure: classifyH3(testCtx, err)}
	}
	defer resp.Body.Close()
	if failure := h3ResponseFailure(resp); failure != "" {
		return model.BandwidthStats{Failure: failure}
	}
	bytesRead, readErr := io.Copy(io.Discard, io.LimitReader(resp.Body, p.MaxBytes))
	duration := time.Since(started)
	stats := model.BandwidthStats{Bytes: bytesRead, DurationMS: milliseconds(duration), TTFBMS: milliseconds(time.Duration(firstByte.Load()))}
	// The local deadline bounds the measurement, just like MaxBytes. A useful
	// sample remains valid when that budget expires; cancellation and stream
	// failures are still failures, even after receiving enough data.
	budgetReached := errors.Is(testCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil
	if ctx.Err() != nil {
		stats.Failure = classifyH3(ctx, ctx.Err())
	} else if readErr != nil && !(budgetReached && bytesRead >= 64*1024) {
		stats.Failure = classifyH3(testCtx, readErr)
	} else if bytesRead < 64*1024 {
		stats.Failure = model.FailureDownload
	} else if duration > 0 {
		stats.Mbps = float64(bytesRead*8) / duration.Seconds() / 1_000_000
	}
	return stats
}

// Each probe owns its UDP socket, so cancellation and failed handshakes cannot
// retain a shared transport or reuse a different candidate's connection.
type quicDialer struct {
	socket    *net.UDPConn
	transport *quic.Transport
	remote    *net.UDPAddr
	timeout   time.Duration
}

func newQUICDialer(candidate model.Candidate, timeout time.Duration) (*quicDialer, error) {
	ip := net.ParseIP(candidate.IP)
	if ip == nil || ip.To4() == nil || candidate.Port < 1 || candidate.Port > 65535 {
		return nil, fmt.Errorf("invalid IPv4 endpoint %s", net.JoinHostPort(candidate.IP, strconv.Itoa(candidate.Port)))
	}
	socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
	if err != nil {
		return nil, err
	}
	return &quicDialer{socket: socket, transport: &quic.Transport{Conn: socket}, remote: &net.UDPAddr{IP: ip.To4(), Port: candidate.Port}, timeout: timeout}, nil
}

func (d *quicDialer) dial(ctx context.Context, tlsConfig *tls.Config, quicConfig *quic.Config) (*quic.Conn, error) {
	connectCtx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	if quicConfig == nil {
		quicConfig = &quic.Config{}
	} else {
		quicConfig = quicConfig.Clone()
	}
	quicConfig.HandshakeIdleTimeout = d.timeout
	conn, err := d.transport.Dial(connectCtx, d.remote, tlsConfig, quicConfig)
	if err != nil {
		return nil, &quicHandshakeError{err}
	}
	if conn.ConnectionState().TLS.NegotiatedProtocol != http3.NextProtoH3 {
		_ = conn.CloseWithError(0, "")
		return nil, errH3Protocol
	}
	return conn, nil
}

func (d *quicDialer) close() {
	_ = d.transport.Close()
	_ = d.socket.Close()
}

func h3TLS(host string, roots *x509.CertPool) *tls.Config {
	return &tls.Config{ServerName: host, RootCAs: roots, MinVersion: tls.VersionTLS13, NextProtos: []string{http3.NextProtoH3}}
}

func newH3Client(candidate model.Candidate, timeout time.Duration, roots *x509.CertPool) (*http.Client, func(), error) {
	dialer, err := newQUICDialer(candidate, timeout)
	if err != nil {
		return nil, nil, err
	}
	transport := &http3.Transport{
		TLSClientConfig:    h3TLS("", roots),
		DisableCompression: true,
		Dial: func(ctx context.Context, _ string, tlsConfig *tls.Config, cfg *quic.Config) (*quic.Conn, error) {
			return dialer.dial(ctx, tlsConfig, cfg)
		},
	}
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return client, func() { _ = transport.Close(); dialer.close() }, nil
}

var errH3Protocol = errors.New("HTTP/3 was not negotiated")

type quicHandshakeError struct{ error }

func (e *quicHandshakeError) Unwrap() error { return e.error }

func h3ResponseFailure(resp *http.Response) model.FailureReason {
	if resp.ProtoMajor != 3 || resp.TLS == nil || resp.TLS.NegotiatedProtocol != http3.NextProtoH3 {
		return model.FailureProtocol
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return model.FailureHTTPStatus
	}
	return ""
}

func classifyH3(ctx context.Context, err error) model.FailureReason {
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return model.FailureCancelled
	}
	if errors.Is(err, errH3Protocol) {
		return model.FailureProtocol
	}
	var transportErr *quic.TransportError
	if errors.As(err, &transportErr) && transportErr.ErrorCode >= 0x100 && transportErr.ErrorCode <= 0x1ff {
		if transportErr.ErrorCode == 0x178 { // TLS no_application_protocol alert
			return model.FailureProtocol
		}
		return model.FailureTLS
	}
	var handshakeErr *quicHandshakeError
	var timeoutErr net.Error
	if errors.As(err, &handshakeErr) && (errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeoutErr) && timeoutErr.Timeout()) {
		return model.FailureQUICHandshakeTimeout
	}
	if failure := classify(ctx, err); failure != model.FailureTCP {
		return failure
	}
	if errors.As(err, &timeoutErr) && timeoutErr.Timeout() {
		return model.FailureTimeout
	}
	return model.FailureQUIC
}

func milliseconds(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}
