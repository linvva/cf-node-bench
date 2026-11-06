package source

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/linvva/cf-node-bench/internal/model"
)

func TestParseH3KeepsDomainIdentityAndDefaults(t *testing.T) {
	parsed := ParseH3([]byte("EDGE.Example.test. edge.example.test:443 edge.example.test:8443#jp 1.1.1.1 999.1.1.1 invalid@host.test https://host.test bad:443 [::1]:443"), "source")
	if len(parsed.Candidates) != 3 || len(parsed.Failures) != 5 {
		t.Fatalf("result=%+v", parsed)
	}
	first := parsed.Candidates[0]
	if first.Host != "edge.example.test" || first.IP != "" || first.AddressType != model.AddressDomain || first.Port != 443 || first.SourceID != "source" {
		t.Fatalf("domain=%+v", first)
	}
	if parsed.Candidates[1].Country != "JP" || parsed.Candidates[1].Port != 8443 {
		t.Fatalf("tagged domain=%+v", parsed.Candidates[1])
	}
	if parsed.Candidates[2].AddressType != model.AddressIPv4 || parsed.Candidates[2].Host != "" {
		t.Fatalf("IP=%+v", parsed.Candidates[2])
	}
	legacy := Parse([]byte("edge.example.test edge.example.test:443 1.1.1.1"), "source")
	if len(legacy.Candidates) != 1 || legacy.Candidates[0].IP != "1.1.1.1" {
		t.Fatalf("legacy behavior changed: %+v", legacy)
	}
}

func TestParseH3JSONAndIDNA(t *testing.T) {
	parsed := ParseH3([]byte(`{"nodes":[{"host":"EDGE.example.test"},{"host":"other.example.test","port":8443,"cc":"US"},{"ip":"1.1.1.1"},{"host":"invalid.example.test","port":"no"}]}`), "json")
	if len(parsed.Candidates) != 3 || len(parsed.Failures) != 1 {
		t.Fatalf("result=%+v", parsed)
	}
	if parsed.Candidates[0].Port != 443 || parsed.Candidates[1].Country != "US" || parsed.Candidates[2].Port != 443 {
		t.Fatalf("candidates=%+v", parsed.Candidates)
	}
	idna := ParseH3([]byte("例子.测试"), "unicode")
	if len(idna.Candidates) != 1 || idna.Candidates[0].Host != "xn--fsqu00a.xn--0zwm56d" {
		t.Fatalf("IDNA=%+v", idna)
	}
}

func TestFetcherEnablesDomainsOnlyForH3(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("edge.example.test:443")) }))
	defer server.Close()
	for _, allow := range []bool{false, true} {
		result, err := (Fetcher{AllowDomains: allow}).Fetch(t.Context(), HTTPSource{ID: "test", URL: server.URL})
		if err != nil {
			t.Fatal(err)
		}
		if (len(result.Candidates) == 1) != allow {
			t.Fatalf("allow=%t result=%+v", allow, result)
		}
	}
}
