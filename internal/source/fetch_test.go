package source

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestFetcherRetriesTransientFailure(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("1.1.1.1:443#US"))
	}))
	defer server.Close()

	result, err := (Fetcher{Timeout: time.Second, MaxRetries: 1, RetryDelay: time.Millisecond}).Fetch(t.Context(), HTTPSource{ID: "retry", URL: server.URL})
	if err != nil || len(result.Candidates) != 1 || requests.Load() != 2 {
		t.Fatalf("result=%+v requests=%d err=%v", result, requests.Load(), err)
	}
}

func TestFetcherIgnoresProxyEnvironment(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://proxy.invalid:3128")
	t.Setenv("HTTPS_PROXY", "http://proxy.invalid:3128")
	t.Setenv("http_proxy", "http://proxy.invalid:3128")
	t.Setenv("https_proxy", "http://proxy.invalid:3128")
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("1.1.1.1:443#US"))
	}))
	defer server.Close()

	client := newDirectHTTPClient(time.Second)
	transport := client.Transport.(*http.Transport)
	dialed := make(chan string, 1)
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		dialed <- address
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}

	result, err := (Fetcher{Client: client}).Fetch(t.Context(), HTTPSource{ID: "direct", URL: "http://source.example/nodes"})
	if err != nil || len(result.Candidates) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if address := <-dialed; address != "source.example:80" {
		t.Fatalf("request was not direct, dialed %q", address)
	}
}

func TestFetcherTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte("1.1.1.1:443"))
	}))
	defer server.Close()

	started := time.Now()
	_, err := (Fetcher{Timeout: 20 * time.Millisecond}).Fetch(t.Context(), HTTPSource{ID: "timeout", URL: server.URL})
	if err == nil || time.Since(started) > 200*time.Millisecond {
		t.Fatalf("timeout was not enforced promptly: %v", err)
	}
}

func TestFetcherCancelsDuringRetryDelay(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporary", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	started := time.Now()
	_, err := (Fetcher{Timeout: time.Second, MaxRetries: 3, RetryDelay: time.Second}).Fetch(ctx, HTTPSource{ID: "cancel", URL: server.URL})
	if !errors.Is(err, context.Canceled) || time.Since(started) > 200*time.Millisecond {
		t.Fatalf("retry delay ignored cancellation: %v", err)
	}
}
