package probe

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/linvva/cf-node-bench/internal/model"
)

func TestSummarizeUsesPercentiles(t *testing.T) {
	stats := Summarize(5, []float64{50, 10, 30, 20}, map[model.FailureReason]int{model.FailureTCP: 1})
	if stats.SuccessRate != 0.8 || stats.P50MS != 25 || math.Abs(stats.P95MS-47) > 0.0001 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestHTTPSAndBandwidthWithVerifiedTLS(t *testing.T) {
	server, roots := speedServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != speedHost {
			t.Errorf("host = %q", r.Host)
		}
		if r.URL.Path == "/__down" {
			_, _ = w.Write(make([]byte, 256*1024))
			return
		}
		_, _ = w.Write([]byte("colo=TEST"))
	}))
	defer server.Close()
	candidate := candidateFor(t, server.Listener.Addr())
	attemptsCompleted := 0
	httpsStats := (HTTPSProber{ConnectTimeout: time.Second, RequestTimeout: time.Second, RootCAs: roots}).Probe(t.Context(), candidate, 3, func() { attemptsCompleted++ })
	if httpsStats.Successes != 3 {
		t.Fatalf("HTTPS stats: %+v", httpsStats)
	}
	if attemptsCompleted != 3 {
		t.Fatalf("attempt progress = %d", attemptsCompleted)
	}
	band := (BandwidthProber{ConnectTimeout: time.Second, TotalTimeout: time.Second, MaxBytes: 128 * 1024, RootCAs: roots, Path: "/__down"}).Probe(t.Context(), candidate)
	if band.Bytes != 128*1024 || band.Mbps <= 0 || band.Failure != "" {
		t.Fatalf("bandwidth: %+v", band)
	}
}

func TestBandwidthUsesConfiguredURLAcrossHTTPSRedirect(t *testing.T) {
	const firstHost = "downloads.example.test"
	const redirectedHost = "cdn.example.test"
	var requests []string
	server, roots := speedServerForHosts(t, []string{firstHost, redirectedHost}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Host+r.URL.RequestURI()+"#"+r.TLS.ServerName)
		if r.Host == firstHost {
			http.Redirect(w, r, "https://"+redirectedHost+"/asset.bin?source=redirect", http.StatusFound)
			return
		}
		if encoding := r.Header.Get("Accept-Encoding"); encoding != "identity" {
			t.Errorf("Accept-Encoding = %q", encoding)
		}
		_, _ = w.Write(make([]byte, 256*1024))
	}))
	defer server.Close()
	stats := (BandwidthProber{
		ConnectTimeout: time.Second,
		TotalTimeout:   time.Second,
		MaxBytes:       64 * 1024,
		RootCAs:        roots,
		URL:            "https://" + firstHost + "/start?token=one",
	}).Probe(t.Context(), candidateFor(t, server.Listener.Addr()))
	if stats.Failure != "" || stats.Bytes != 64*1024 {
		t.Fatalf("bandwidth stats: %+v", stats)
	}
	want := []string{
		firstHost + "/start?token=one#" + firstHost,
		redirectedHost + "/asset.bin?source=redirect#" + redirectedHost,
	}
	if len(requests) != len(want) {
		t.Fatalf("requests = %v", requests)
	}
	for index := range want {
		if requests[index] != want[index] {
			t.Fatalf("request %d = %q, want %q", index, requests[index], want[index])
		}
	}
}

func TestBandwidthRejectsUnsafeRedirect(t *testing.T) {
	tests := map[string]string{
		"HTTP downgrade": "http://speed.cloudflare.com/insecure",
		"user info":      "https://user:secret@speed.cloudflare.com/private",
	}
	for name, redirectURL := range tests {
		t.Run(name, func(t *testing.T) {
			server, roots := speedServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, redirectURL, http.StatusFound)
			}))
			defer server.Close()
			stats := (BandwidthProber{ConnectTimeout: time.Second, TotalTimeout: time.Second, MaxBytes: 64 * 1024, RootCAs: roots}).Probe(t.Context(), candidateFor(t, server.Listener.Addr()))
			if stats.Failure != model.FailureDownload {
				t.Fatalf("failure = %q", stats.Failure)
			}
		})
	}
}

func TestHTTPSClientDoesNotUseProxy(t *testing.T) {
	client := (HTTPSProber{ConnectTimeout: time.Second}).client(model.Candidate{IP: "127.0.0.1", Port: 443})
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatal("HTTPS probe transport must always connect directly")
	}
}

func TestHTTPSClassifiesTimeout(t *testing.T) {
	server, roots := speedServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(150 * time.Millisecond)
		_, _ = w.Write([]byte("late"))
	}))
	defer server.Close()
	stats := (HTTPSProber{ConnectTimeout: time.Second, RequestTimeout: 20 * time.Millisecond, RootCAs: roots}).Probe(t.Context(), candidateFor(t, server.Listener.Addr()), 1, nil)
	if stats.Failures[model.FailureTimeout] != 1 {
		t.Fatalf("failures: %+v", stats.Failures)
	}
}

func TestBandwidthCancellationStopsRead(t *testing.T) {
	server, roots := speedServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher := w.(http.Flusher)
		for i := 0; i < 100; i++ {
			_, _ = w.Write(make([]byte, 1024))
			flusher.Flush()
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	go func() { time.Sleep(30 * time.Millisecond); cancel() }()
	started := time.Now()
	stats := (BandwidthProber{ConnectTimeout: time.Second, TotalTimeout: time.Second, MaxBytes: 1024 * 1024, RootCAs: roots, Path: "/__down"}).Probe(ctx, candidateFor(t, server.Listener.Addr()))
	if stats.Failure != model.FailureCancelled {
		t.Fatalf("failure: %q", stats.Failure)
	}
	if time.Since(started) > 300*time.Millisecond {
		t.Fatal("cancellation was not prompt")
	}
}

func TestBandwidthThresholdOptimisticBound(t *testing.T) {
	threshold := BandwidthThreshold{MinMbps: 8, LineMbps: 16}
	for _, tc := range []struct {
		name               string
		received, maxBytes int64
		remaining          time.Duration
		wantStop           bool
	}{
		{"exactly reachable", 0, 2_000_000, time.Second, false},
		{"byte limit", 0, 1_999_999, time.Second, true},
		{"time limit", 0, 2_000_000, time.Second - time.Microsecond, true},
		{"on target already", 1_000_000, 2_000_000, time.Millisecond, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := threshold.cannotReach(tc.received, tc.maxBytes, time.Second, tc.remaining); got != tc.wantStop {
				t.Fatalf("cannotReach = %v, want %v", got, tc.wantStop)
			}
		})
	}
}

func TestBandwidthThresholdStopsDownload(t *testing.T) {
	for _, h3 := range []bool{false, true} {
		name := "https"
		if h3 {
			name = "h3"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cancelRequest := make(chan struct{}, 1)
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/pass":
					_, _ = w.Write(make([]byte, 256*1024))
					return
				case "/below":
					time.Sleep(100 * time.Millisecond)
					_, _ = w.Write(make([]byte, 64*1024))
					return
				case "/line":
					time.Sleep(700 * time.Millisecond)
					_, _ = w.Write(make([]byte, 1024))
					w.(http.Flusher).Flush()
				case "/stall-body":
					_, _ = w.Write(make([]byte, 64*1024))
					w.(http.Flusher).Flush()
				case "/cancel":
					cancelRequest <- struct{}{}
				}
				<-r.Context().Done()
			})
			var candidate model.Candidate
			var roots *x509.CertPool
			const host = "download.example.test"
			if h3 {
				candidate, roots, _ = h3TestServer(t, []string{host}, handler)
			} else {
				server, pool := speedServerForHosts(t, []string{host}, handler)
				t.Cleanup(server.Close)
				candidate, roots = candidateFor(t, server.Listener.Addr()), pool
			}
			for _, tc := range []struct {
				path         string
				threshold    BandwidthThreshold
				maxBytes     int64
				want         model.FailureReason
				wantBytes    int64
				maxElapsedMS float64
			}{
				{"/pass", BandwidthThreshold{MinMbps: 1}, 20 << 20, "", 256 * 1024, 1000},
				{"/below", BandwidthThreshold{MinMbps: 80}, 20 << 20, model.FailureBandwidthBelowMin, 64 * 1024, 1000},
				{"/stall-headers", BandwidthThreshold{MinMbps: 80}, 4 << 20, model.FailureBandwidthBelowMin, 0, 1000},
				{"/original-timeout", BandwidthThreshold{MinMbps: 80}, 20 << 20, model.FailureTimeout, 0, 1500},
				{"/stall-body", BandwidthThreshold{MinMbps: 80}, 20 << 20, model.FailureBandwidthBelowMin, 64 * 1024, 2500},
				{"/line", BandwidthThreshold{MinMbps: 80, LineMbps: 100}, 20 << 20, model.FailureBandwidthBelowMin, 1024, 1700},
				{"/cancel", BandwidthThreshold{MinMbps: 80}, 20 << 20, model.FailureCancelled, 0, 1500},
			} {
				t.Run(tc.path[1:], func(t *testing.T) {
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					if tc.path == "/cancel" {
						go func() {
							select {
							case <-cancelRequest:
								cancel()
							case <-ctx.Done():
							}
						}()
					}
					url := "https://" + host + tc.path
					totalTimeout := 12 * time.Second
					if tc.path == "/original-timeout" {
						totalTimeout = 250 * time.Millisecond
					}
					started := time.Now()
					var stats model.BandwidthStats
					if h3 {
						stats = (H3BandwidthProber{ConnectTimeout: 3 * time.Second, TotalTimeout: totalTimeout, MaxBytes: tc.maxBytes, RootCAs: roots, URL: url, Threshold: tc.threshold}).Probe(ctx, candidate)
					} else {
						stats = (BandwidthProber{ConnectTimeout: 3 * time.Second, TotalTimeout: totalTimeout, MaxBytes: tc.maxBytes, RootCAs: roots, URL: url, Threshold: tc.threshold}).Probe(ctx, candidate)
					}
					if stats.Failure != tc.want || stats.Bytes != tc.wantBytes || milliseconds(time.Since(started)) > tc.maxElapsedMS {
						t.Fatalf("unexpected bandwidth result: %+v (%s)", stats, time.Since(started))
					}
					if tc.path == "/below" && (stats.Mbps <= 0 || stats.Mbps >= tc.threshold.MinMbps) {
						t.Fatalf("completed low sample lost its measured bandwidth: %+v", stats)
					}
					if tc.path == "/stall-body" && (stats.DurationMS < 2090 || stats.Mbps != 0) {
						t.Fatalf("expected cutoff at approximately 2.10 seconds: %+v", stats)
					}
					if tc.path == "/line" && stats.Mbps != 0 {
						t.Fatalf("projected failure was reported as a complete sample: %+v", stats)
					}
				})
			}
		})
	}
}

func speedServer(t *testing.T, handler http.Handler) (*httptest.Server, *x509.CertPool) {
	return speedServerForHosts(t, []string{speedHost}, handler)
}

func speedServerForHosts(t *testing.T, hosts []string, handler http.Handler) (*httptest.Server, *x509.CertPool) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: hosts[0]}, DNSNames: hosts, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	server.StartTLS()
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	return server, roots
}

func candidateFor(t *testing.T, address net.Addr) model.Candidate {
	t.Helper()
	host, portText, err := net.SplitHostPort(address.String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	return model.Candidate{AddressType: model.AddressIPv4, IP: host, Port: port}
}
