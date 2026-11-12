package probe

import (
	"context"
	"crypto/x509"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/linvva/cf-node-bench/internal/model"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

const h3TestHost = "worker.example.test"

func h3TestServer(t *testing.T, hosts []string, handler http.Handler) (model.Candidate, *x509.CertPool, *atomic.Int32) {
	t.Helper()
	certServer, roots := speedServerForHosts(t, hosts, handler)
	tlsConfig := certServer.TLS.Clone()
	certServer.Close()
	socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	active := &atomic.Int32{}
	server := &http3.Server{TLSConfig: tlsConfig, Handler: handler, ConnContext: func(ctx context.Context, conn *quic.Conn) context.Context {
		active.Add(1)
		context.AfterFunc(conn.Context(), func() { active.Add(-1) })
		return ctx
	}}
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(socket) }()
	t.Cleanup(func() { _ = server.Close(); _ = socket.Close(); <-done })
	return candidateFor(t, socket.LocalAddr()), roots, active
}

func TestH3ProbesUseFixedIPAndVerifiedWorkerHost(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("ALL_PROXY", "socks5://127.0.0.1:1")
	var requests atomic.Int32
	candidate, roots, active := h3TestServer(t, []string{h3TestHost, "download.example.test"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.ProtoMajor != 3 || r.TLS == nil || r.TLS.NegotiatedProtocol != "h3" || r.Host != r.TLS.ServerName {
			t.Errorf("unexpected request: proto=%s host=%s tls=%+v", r.Proto, r.Host, r.TLS)
		}
		if r.Host == h3TestHost {
			if r.URL.Path != "/cdn-cgi/trace" {
				t.Errorf("probe path=%s", r.URL.Path)
			}
			_, _ = io.WriteString(w, "colo=TEST\n")
			return
		}
		if r.URL.RequestURI() != "/file.bin?size=large" || r.Header.Get("Accept-Encoding") != "identity" {
			t.Errorf("download request=%s", r.URL)
		}
		_, _ = w.Write(make([]byte, 256*1024))
	}))
	completed := 0
	quicStats := (QUICProber{Timeout: time.Second, WorkerHost: h3TestHost, RootCAs: roots, MinSuccessRate: 2.0 / 3}).Probe(t.Context(), candidate, 3, func() { completed++ })
	if quicStats.Successes != 3 || completed != 3 || quicStats.Attempts != 3 || quicStats.Skipped != 0 {
		t.Fatalf("QUIC=%+v attempts=%d", quicStats, completed)
	}
	httpStats := (HTTP3Prober{ConnectTimeout: time.Second, RequestTimeout: time.Second, WorkerHost: h3TestHost, RootCAs: roots}).Probe(t.Context(), candidate, 3, nil)
	if httpStats.Successes != 3 {
		t.Fatalf("HTTP/3=%+v", httpStats)
	}
	band := (H3BandwidthProber{ConnectTimeout: time.Second, TotalTimeout: time.Second, MaxBytes: 128 * 1024, RootCAs: roots, URL: "https://download.example.test/file.bin?size=large"}).Probe(t.Context(), candidate)
	if band.Failure != "" || band.Bytes != 128*1024 || band.Mbps <= 0 || band.TTFBMS <= 0 {
		t.Fatalf("download=%+v", band)
	}
	if requests.Load() != 4 {
		t.Fatalf("requests=%d", requests.Load())
	}
	deadline := time.Now().Add(time.Second)
	for active.Load() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if active.Load() != 0 {
		t.Fatalf("connections still active after probes: %d", active.Load())
	}
}

func TestH3RejectsUntrustedAndMismatchedCertificates(t *testing.T) {
	candidate, roots, _ := h3TestServer(t, []string{h3TestHost}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	for _, tc := range []struct {
		name, host string
		roots      *x509.CertPool
	}{{"untrusted", h3TestHost, nil}, {"wrong host", "other.example.test", roots}} {
		t.Run(tc.name, func(t *testing.T) {
			stats := (HTTP3Prober{ConnectTimeout: time.Second, RequestTimeout: time.Second, WorkerHost: tc.host, RootCAs: tc.roots}).Probe(t.Context(), candidate, 1, nil)
			if stats.Failures[model.FailureTLS] != 1 {
				t.Fatalf("failures=%+v", stats.Failures)
			}
		})
	}
}

func TestH3BandwidthDefaultUsesH3Endpoint(t *testing.T) {
	candidate, roots, _ := h3TestServer(t, []string{"h3.speed.cloudflare.com"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "h3.speed.cloudflare.com" || r.URL.RequestURI() != "/__down?bytes=131072" {
			t.Errorf("unexpected default download target: %s%s", r.Host, r.URL.RequestURI())
		}
		_, _ = w.Write(make([]byte, 131072))
	}))
	stats := (H3BandwidthProber{ConnectTimeout: time.Second, TotalTimeout: time.Second, MaxBytes: 131072, RootCAs: roots}).Probe(t.Context(), candidate)
	if stats.Failure != "" || stats.Bytes != 131072 {
		t.Fatalf("default download: %+v", stats)
	}
}

func TestH3CannotFallbackToWorkingTCP(t *testing.T) {
	var tcpRequests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { tcpRequests.Add(1); w.WriteHeader(200) }))
	defer server.Close()
	candidate := candidateFor(t, server.Listener.Addr())
	socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP(candidate.IP), Port: candidate.Port})
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	stats := (HTTP3Prober{ConnectTimeout: 80 * time.Millisecond, RequestTimeout: time.Second, WorkerHost: h3TestHost, MinSuccessRate: 1}).Probe(t.Context(), candidate, 3, nil)
	if stats.Failures[model.FailureQUICHandshakeTimeout] != 1 || stats.Attempts != 1 || stats.Skipped != 2 || tcpRequests.Load() != 0 {
		t.Fatalf("stats=%+v TCP=%d", stats, tcpRequests.Load())
	}
	quicStats := (QUICProber{Timeout: 80 * time.Millisecond, WorkerHost: h3TestHost, MinSuccessRate: 1}).Probe(t.Context(), candidate, 3, nil)
	if quicStats.Failures[model.FailureQUICHandshakeTimeout] != 1 || quicStats.Attempts != 1 || quicStats.Skipped != 2 {
		t.Fatalf("QUIC=%+v", quicStats)
	}
}

func TestH3RejectsALPNMismatch(t *testing.T) {
	certServer, roots := speedServerForHosts(t, []string{h3TestHost}, http.NotFoundHandler())
	tlsConfig := certServer.TLS.Clone()
	certServer.Close()
	tlsConfig.NextProtos = []string{"other"}
	listener, err := quic.ListenAddr("127.0.0.1:0", tlsConfig, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	stats := (QUICProber{Timeout: time.Second, WorkerHost: h3TestHost, RootCAs: roots}).Probe(t.Context(), candidateFor(t, listener.Addr()), 1, nil)
	if stats.Failures[model.FailureProtocol] != 1 {
		t.Fatalf("failures=%+v", stats.Failures)
	}
}

func TestH3RejectsHTTPStatusRedirectAndShortDownloads(t *testing.T) {
	for _, tc := range []struct {
		name         string
		status, size int
		want         model.FailureReason
	}{
		{"forbidden", 403, 100000, model.FailureHTTPStatus},
		{"redirect", 302, 100000, model.FailureHTTPStatus},
		{"short", 200, 1024, model.FailureDownload},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			candidate, roots, _ := h3TestServer(t, []string{h3TestHost}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.Header().Set("Location", "https://other.example.test/redirected")
				w.WriteHeader(tc.status)
				_, _ = w.Write(make([]byte, tc.size))
			}))
			stats := (H3BandwidthProber{ConnectTimeout: time.Second, TotalTimeout: time.Second, MaxBytes: 128 * 1024, RootCAs: roots, URL: "https://" + h3TestHost + "/file"}).Probe(t.Context(), candidate)
			if stats.Failure != tc.want || stats.Mbps != 0 || requests.Load() != 1 {
				t.Fatalf("stats=%+v requests=%d", stats, requests.Load())
			}
		})
	}
}

func TestH3CancellationClosesHandshakeAndDownload(t *testing.T) {
	t.Run("handshake", func(t *testing.T) {
		socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			t.Fatal(err)
		}
		defer socket.Close()
		ctx, cancel := context.WithCancel(t.Context())
		go func() { time.Sleep(30 * time.Millisecond); cancel() }()
		started := time.Now()
		stats := (QUICProber{Timeout: time.Second, WorkerHost: h3TestHost, MinSuccessRate: 1}).Probe(ctx, candidateFor(t, socket.LocalAddr()), 3, nil)
		if stats.Failures[model.FailureCancelled] != 1 || stats.Attempts != 1 || stats.Skipped != 0 || time.Since(started) > 500*time.Millisecond {
			t.Fatalf("stats=%+v duration=%s", stats, time.Since(started))
		}
	})
	t.Run("download", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		candidate, roots, _ := h3TestServer(t, []string{h3TestHost}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(make([]byte, 65536))
			w.(http.Flusher).Flush()
			cancel()
			<-r.Context().Done()
		}))
		started := time.Now()
		stats := (H3BandwidthProber{ConnectTimeout: time.Second, TotalTimeout: time.Second, MaxBytes: 1024 * 1024, RootCAs: roots, URL: "https://" + h3TestHost + "/file"}).Probe(ctx, candidate)
		if stats.Failure != model.FailureCancelled || time.Since(started) > 500*time.Millisecond {
			t.Fatalf("stats=%+v duration=%s", stats, time.Since(started))
		}
	})
}

func TestH3HTTPAndDownloadTimeout(t *testing.T) {
	candidate, roots, _ := h3TestServer(t, []string{h3TestHost}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/file" {
			_, _ = w.Write(make([]byte, 1024))
			w.(http.Flusher).Flush()
		}
		<-r.Context().Done()
	}))
	stats := (HTTP3Prober{ConnectTimeout: time.Second, RequestTimeout: 100 * time.Millisecond, WorkerHost: h3TestHost, RootCAs: roots}).Probe(t.Context(), candidate, 1, nil)
	if stats.Failures[model.FailureTimeout] != 1 {
		t.Fatalf("HTTP/3=%+v", stats)
	}
	band := (H3BandwidthProber{ConnectTimeout: time.Second, TotalTimeout: 100 * time.Millisecond, MaxBytes: 1024 * 1024, RootCAs: roots, URL: "https://" + h3TestHost + "/file"}).Probe(t.Context(), candidate)
	if band.Failure != model.FailureTimeout || band.Mbps != 0 {
		t.Fatalf("download=%+v", band)
	}
}

func TestH3DownloadTimeBudgetMeasuresPartialTransfer(t *testing.T) {
	for _, size := range []int{64*1024 - 1, 64 * 1024, 256 * 1024} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			closed := make(chan struct{})
			candidate, roots, _ := h3TestServer(t, []string{h3TestHost}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write(make([]byte, size))
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				close(closed)
			}))
			stats := (H3BandwidthProber{ConnectTimeout: time.Second, TotalTimeout: 500 * time.Millisecond, MaxBytes: 20 * 1024 * 1024, RootCAs: roots, URL: "https://" + h3TestHost + "/file"}).Probe(t.Context(), candidate)
			if stats.Bytes != int64(size) {
				t.Fatalf("did not measure received bytes: %+v", stats)
			}
			if size >= 64*1024 {
				want := float64(size*8) / (stats.DurationMS / 1000) / 1_000_000
				if stats.Failure != "" || stats.Mbps <= 0 || math.Abs(stats.Mbps-want) > 0.000001 {
					t.Fatalf("valid timed sample discarded: %+v, expected Mbps=%f", stats, want)
				}
			} else if stats.Failure != model.FailureTimeout || stats.Mbps != 0 {
				t.Fatalf("short timed sample accepted: %+v", stats)
			}
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("download request still active after measurement")
			}
		})
	}
}

func TestH3DownloadRejectsInterruptedPartialTransfers(t *testing.T) {
	for _, parentDeadline := range []bool{false, true} {
		t.Run(fmt.Sprint(parentDeadline), func(t *testing.T) {
			candidate, roots, _ := h3TestServer(t, []string{h3TestHost}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", "20971520")
				_, _ = w.Write(make([]byte, 256*1024))
				w.(http.Flusher).Flush()
				if parentDeadline {
					<-r.Context().Done()
				}
			}))
			ctx := t.Context()
			if parentDeadline {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 500*time.Millisecond)
				defer cancel()
			}
			stats := (H3BandwidthProber{ConnectTimeout: time.Second, TotalTimeout: 2 * time.Second, MaxBytes: 20 * 1024 * 1024, RootCAs: roots, URL: "https://" + h3TestHost + "/file"}).Probe(ctx, candidate)
			if stats.Bytes < 64*1024 || stats.Failure == "" || stats.Mbps != 0 {
				t.Fatalf("interrupted transfer accepted: %+v", stats)
			}
		})
	}
}

func TestQUICDialerClosesOwnedSocket(t *testing.T) {
	dialer, err := newQUICDialer(model.Candidate{IP: "127.0.0.1", Port: 443}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	address := dialer.socket.LocalAddr().(*net.UDPAddr)
	dialer.close()
	socket, err := net.ListenUDP("udp4", address)
	if err != nil {
		t.Fatalf("probe still owns UDP port: %v", err)
	}
	_ = socket.Close()
}
