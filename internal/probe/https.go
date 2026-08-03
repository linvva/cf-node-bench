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
	"time"

	"github.com/linvva/cf-node-bench/internal/model"
)

const speedHost = "speed.cloudflare.com"

var errRejectedRedirect = errors.New("下载测速重定向被拒绝")

type HTTPSProber struct {
	ConnectTimeout time.Duration
	RequestTimeout time.Duration
	RootCAs        *x509.CertPool
	Path           string
}

func (p HTTPSProber) Probe(ctx context.Context, candidate model.Candidate, attempts int, onAttempt func()) model.ProbeStats {
	samples := make([]float64, 0, attempts)
	failures := map[model.FailureReason]int{}
	client := p.client(candidate)
	path := p.Path
	if path == "" {
		path = "/cdn-cgi/trace"
	}
	for index := 0; index < attempts; index++ {
		attemptCtx, cancel := context.WithTimeout(ctx, p.RequestTimeout)
		started := time.Now()
		req, _ := http.NewRequestWithContext(attemptCtx, http.MethodGet, "https://"+speedHost+path, nil)
		req.Host = speedHost
		resp, err := client.Do(req)
		if err != nil {
			failures[classify(attemptCtx, err)]++
			cancel()
			if onAttempt != nil {
				onAttempt()
			}
			if ctx.Err() != nil {
				break
			}
			continue
		}
		_, readErr := io.Copy(io.Discard, io.LimitReader(resp.Body, 256*1024))
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			failures[model.FailureHTTPStatus]++
		} else if readErr != nil {
			failures[classify(attemptCtx, readErr)]++
		} else {
			samples = append(samples, float64(time.Since(started).Microseconds())/1000)
		}
		cancel()
		if onAttempt != nil {
			onAttempt()
		}
	}
	return Summarize(attempts, samples, failures)
}

func (p HTTPSProber) client(candidate model.Candidate) *http.Client {
	address := net.JoinHostPort(candidate.IP, strconv.Itoa(candidate.Port))
	dialer := net.Dialer{Timeout: p.ConnectTimeout}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, address)
		},
		TLSClientConfig:     &tls.Config{RootCAs: p.RootCAs, MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: p.ConnectTimeout,
		DisableKeepAlives:   true,
		DisableCompression:  true,
	}
	return &http.Client{Transport: transport}
}

type BandwidthProber struct {
	ConnectTimeout time.Duration
	TotalTimeout   time.Duration
	MaxBytes       int64
	RootCAs        *x509.CertPool
	Path           string
	URL            string
}

func (p BandwidthProber) Probe(ctx context.Context, candidate model.Candidate) model.BandwidthStats {
	testCtx, cancel := context.WithTimeout(ctx, p.TotalTimeout)
	defer cancel()
	targetURL := p.URL
	if targetURL == "" {
		path := p.Path
		if path == "" {
			path = fmt.Sprintf("/__down?bytes=%d", p.MaxBytes)
		}
		targetURL = "https://" + speedHost + path
	}
	started := time.Now()
	var firstByte time.Time
	req, err := http.NewRequestWithContext(testCtx, http.MethodGet, targetURL, nil)
	if err != nil {
		return model.BandwidthStats{Failure: model.FailureDownload}
	}
	req.Header.Set("Accept-Encoding", "identity")
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
		GotFirstResponseByte: func() { firstByte = time.Now() },
	}))
	client := (HTTPSProber{ConnectTimeout: p.ConnectTimeout, RootCAs: p.RootCAs}).client(candidate)
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 ||
			req.URL.Scheme != "https" ||
			req.URL.Hostname() == "" ||
			req.URL.User != nil ||
			req.URL.Fragment != "" ||
			req.URL.RawFragment != "" {
			return errRejectedRedirect
		}
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, errRejectedRedirect) {
			return model.BandwidthStats{Failure: model.FailureDownload}
		}
		return model.BandwidthStats{Failure: classify(testCtx, err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return model.BandwidthStats{Failure: model.FailureHTTPStatus}
	}
	bytesRead, err := io.Copy(io.Discard, io.LimitReader(resp.Body, p.MaxBytes))
	duration := time.Since(started)
	stats := model.BandwidthStats{Bytes: bytesRead, DurationMS: float64(duration.Microseconds()) / 1000}
	if !firstByte.IsZero() {
		stats.TTFBMS = float64(firstByte.Sub(started).Microseconds()) / 1000
	}
	if duration > 0 {
		stats.Mbps = float64(bytesRead*8) / duration.Seconds() / 1_000_000
	}
	if err != nil {
		stats.Failure = classify(testCtx, err)
	}
	return stats
}
