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
	httpsStats := (HTTPSProber{ConnectTimeout: time.Second, RequestTimeout: time.Second, RootCAs: roots}).Probe(t.Context(), candidate, 3)
	if httpsStats.Successes != 3 {
		t.Fatalf("HTTPS stats: %+v", httpsStats)
	}
	band := (BandwidthProber{ConnectTimeout: time.Second, TotalTimeout: time.Second, MaxBytes: 128 * 1024, RootCAs: roots, Path: "/__down"}).Probe(t.Context(), candidate)
	if band.Bytes != 128*1024 || band.Mbps <= 0 || band.Failure != "" {
		t.Fatalf("bandwidth: %+v", band)
	}
}

func TestHTTPSClassifiesTimeout(t *testing.T) {
	server, roots := speedServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(150 * time.Millisecond)
		_, _ = w.Write([]byte("late"))
	}))
	defer server.Close()
	stats := (HTTPSProber{ConnectTimeout: time.Second, RequestTimeout: 20 * time.Millisecond, RootCAs: roots}).Probe(t.Context(), candidateFor(t, server.Listener.Addr()), 1)
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

func speedServer(t *testing.T, handler http.Handler) (*httptest.Server, *x509.CertPool) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: speedHost}, DNSNames: []string{speedHost}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
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
