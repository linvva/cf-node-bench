package probe

import (
	"context"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/linvva/cf-node-bench/internal/model"
)

func TestHTTPProbesStopOnlyWhenSuccessRateIsUnreachable(t *testing.T) {
	for _, protocol := range []string{"https", "http3"} {
		for _, tc := range []struct {
			name      string
			minimum   float64
			statuses  []int
			completed int
			successes int
		}{
			{"100 percent first failure", 1, []int{403, 200, 200}, 1, 0},
			{"two thirds recovery", 2.0 / 3, []int{403, 200, 200}, 3, 2},
			{"two thirds second failure", 2.0 / 3, []int{403, 403, 200}, 2, 0},
			{"survivor keeps all samples", 2.0 / 3, []int{200, 200, 200}, 3, 3},
			{"actual attempts denominator", 0.8, []int{200, 200, 403, 403, 200}, 4, 2},
			{"zero preserves full sampling", 0, []int{403, 200, 200}, 3, 2},
		} {
			t.Run(protocol+"/"+tc.name, func(t *testing.T) {
				var requests atomic.Int32
				handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					index := int(requests.Add(1)) - 1
					if index >= len(tc.statuses) {
						t.Error("unexpected extra request")
						w.WriteHeader(500)
						return
					}
					w.WriteHeader(tc.statuses[index])
				})
				completed := 0
				var stats model.ProbeStats
				if protocol == "https" {
					server, roots := speedServer(t, handler)
					defer server.Close()
					stats = (HTTPSProber{ConnectTimeout: time.Second, RequestTimeout: time.Second, RootCAs: roots, MinSuccessRate: tc.minimum}).Probe(t.Context(), candidateFor(t, server.Listener.Addr()), len(tc.statuses), func() { completed++ })
				} else {
					candidate, roots, _ := h3TestServer(t, []string{h3TestHost}, handler)
					stats = (HTTP3Prober{ConnectTimeout: time.Second, RequestTimeout: time.Second, RootCAs: roots, WorkerHost: h3TestHost, MinSuccessRate: tc.minimum}).Probe(t.Context(), candidate, len(tc.statuses), func() { completed++ })
				}
				if stats.Attempts != tc.completed || stats.Successes != tc.successes || stats.Skipped != len(tc.statuses)-tc.completed || completed != tc.completed || int(requests.Load()) != tc.completed {
					t.Fatalf("stats=%+v, callbacks=%d, requests=%d", stats, completed, requests.Load())
				}
				if stats.SuccessRate != float64(tc.successes)/float64(tc.completed) || stats.Failures[model.FailureHTTPStatus] != tc.completed-tc.successes {
					t.Fatalf("unmeasured attempts affected statistics: %+v", stats)
				}
			})
		}
	}
}

func TestHTTPSRejectsStatusBeforeReadingSlowBody(t *testing.T) {
	closed := make(chan struct{})
	server, roots := speedServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(closed)
	}))
	defer server.Close()
	started := time.Now()
	stats := (HTTPSProber{ConnectTimeout: time.Second, RequestTimeout: 2 * time.Second, RootCAs: roots, MinSuccessRate: 1}).Probe(t.Context(), candidateFor(t, server.Listener.Addr()), 3, nil)
	if stats.Attempts != 1 || stats.Skipped != 2 || stats.Failures[model.FailureHTTPStatus] != 1 || time.Since(started) > time.Second {
		t.Fatalf("slow error body was read: %+v, duration=%s", stats, time.Since(started))
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("rejected response connection was not closed")
	}
}

func TestTCPStopsAfterFailureAndKeepsSurvivorSamples(t *testing.T) {
	for _, failAt := range []int{0, 1, 3} {
		t.Run(string(rune('0'+failAt)), func(t *testing.T) {
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			candidate := candidateFor(t, listener.Addr())
			if failAt == 0 {
				_ = listener.Close()
			}
			completed := 0
			stats := (TCPProber{Timeout: time.Second, MinSuccessRate: 1}).Probe(t.Context(), candidate, 3, func() {
				completed++
				if completed == failAt {
					_ = listener.Close()
				}
			})
			want := min(failAt+1, 3)
			if completed != want || stats.Attempts != want || stats.Successes != failAt || stats.Skipped != 3-want {
				t.Fatalf("stats=%+v callbacks=%d", stats, completed)
			}
		})
	}
}

func TestCancelledProbeDoesNotMarkUnmeasuredAttemptsSkipped(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	candidate := candidateFor(t, listener.Addr())
	_ = listener.Close()
	// Hold the UDP port open without replying, keeping QUIC failures deterministic.
	socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP(candidate.IP), Port: candidate.Port})
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	for name, probe := range map[string]interface {
		Probe(context.Context, model.Candidate, int, func()) model.ProbeStats
	}{
		"tcp":   TCPProber{Timeout: time.Second, MinSuccessRate: 1},
		"https": HTTPSProber{ConnectTimeout: time.Second, RequestTimeout: time.Second, MinSuccessRate: 1},
		"quic":  QUICProber{Timeout: 30 * time.Millisecond, WorkerHost: h3TestHost, MinSuccessRate: 1},
		"http3": HTTP3Prober{ConnectTimeout: 30 * time.Millisecond, RequestTimeout: time.Second, WorkerHost: h3TestHost, MinSuccessRate: 1},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			callbacks := 0
			stats := probe.Probe(ctx, candidate, 3, func() { callbacks++; cancel() })
			if callbacks != 1 || stats.Attempts != 1 || stats.Skipped != 0 {
				t.Fatalf("cancelled probe=%+v callbacks=%d", stats, callbacks)
			}
			stats = probe.Probe(ctx, candidate, 3, func() { t.Error("pre-cancelled context attempted a probe") })
			if stats.Attempts != 0 || stats.Skipped != 0 || len(stats.Failures) != 0 {
				t.Fatalf("pre-cancelled probe=%+v", stats)
			}
		})
	}
}
