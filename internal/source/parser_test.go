package source

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseTextAndFailures(t *testing.T) {
	result := Parse([]byte("1.1.1.1:443#US\n1.1.1.1:443 8.8.8.8:8443#jp bad:443 9.9.9.9:99999 2.2.2.2:443#USA"), "source")
	if len(result.Candidates) != 2 {
		t.Fatalf("got %d candidates", len(result.Candidates))
	}
	if result.Candidates[1].Country != "JP" {
		t.Fatalf("country not normalized: %q", result.Candidates[1].Country)
	}
	if len(result.Failures) != 3 {
		t.Fatalf("got %d failures", len(result.Failures))
	}
}

func TestParseBareIPv4UsesHTTPSPort(t *testing.T) {
	result := Parse([]byte("1.1.1.1 8.8.8.8"), "source")
	if len(result.Candidates) != 2 || result.Candidates[0].Port != 443 {
		t.Fatalf("unexpected candidates: %+v", result.Candidates)
	}
}

func TestParseNestedJSON(t *testing.T) {
	data := []byte(`{"payload":{"nodes":[{"ip":"1.0.0.1","port":443,"cc":"AU"},{"host":"8.8.4.4","port":"8443","country":"US"}]}}`)
	result := Parse(data, "json")
	if len(result.Candidates) != 2 {
		t.Fatalf("got %d candidates", len(result.Candidates))
	}
	if result.Candidates[0].AddressType != "ipv4" {
		t.Fatalf("unexpected address type")
	}
}

func TestFetcherRejectsUndeclaredContentEncoding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "br")
		_, _ = w.Write([]byte("1.1.1.1:443"))
	}))
	defer server.Close()
	_, err := (Fetcher{}).Fetch(t.Context(), HTTPSource{ID: "x", URL: server.URL})
	if err == nil {
		t.Fatal("expected unsupported encoding error")
	}
}
