package publish

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGistUpdatesOnlyConfiguredFileAndReturnsRawURL(t *testing.T) {
	summary := testSummary()
	expected := FormatResults(summary.Results, DefaultSettings().Output)
	patches := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gists/gist-id" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"id":"gist-id","files":{"ip.txt":{"filename":"ip.txt","content":"old","raw_url":"https://raw.test/old"},"notes.md":{"filename":"notes.md","content":"keep"}}}`))
		case http.MethodPatch:
			patches++
			var payload struct {
				Files map[string]struct {
					Content string `json:"content"`
				} `json:"files"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Files) != 1 || payload.Files["ip.txt"].Content != expected {
				t.Fatalf("payload=%+v", payload)
			}
			_, _ = w.Write([]byte(`{"id":"gist-id","files":{"ip.txt":{"filename":"ip.txt","content":"updated","raw_url":"https://raw.test/current"},"notes.md":{"filename":"notes.md","content":"keep"}}}`))
		default:
			t.Fatalf("method=%s", r.Method)
		}
	}))
	defer server.Close()

	settings := DefaultSettings()
	settings.Request.MaxRetries = 0
	settings.Gist = GistSettings{Enabled: true, Token: "secret", GistID: "gist-id", Filename: "ip.txt"}
	result := testService(server).PublishGist(t.Context(), summary, settings)
	if result.State != "succeeded" || result.Items != len(summary.Results) || result.URL != "https://raw.test/current" || patches != 1 {
		t.Fatalf("result=%+v patches=%d", result, patches)
	}
}

func TestGistAddsMissingFileAndSkipsUnchangedContent(t *testing.T) {
	summary := testSummary()
	content := FormatResults(summary.Results, DefaultSettings().Output)
	t.Run("missing file", func(t *testing.T) {
		patched := false
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`{"id":"gist-id","files":{"other.txt":{"filename":"other.txt","content":"keep"}}}`))
				return
			}
			patched = true
			_, _ = w.Write([]byte(`{"id":"gist-id","files":{"ip.txt":{"filename":"ip.txt","content":"new","raw_url":"https://raw.test/new"},"other.txt":{"filename":"other.txt","content":"keep"}}}`))
		}))
		defer server.Close()
		settings := DefaultSettings()
		settings.Gist = GistSettings{Enabled: true, Token: "secret", GistID: "gist-id", Filename: "ip.txt"}
		result := testService(server).PublishGist(t.Context(), summary, settings)
		if result.State != "succeeded" || !patched || result.URL != "https://raw.test/new" {
			t.Fatalf("result=%+v patched=%t", result, patched)
		}
	})

	t.Run("unchanged content", func(t *testing.T) {
		requests := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++
			_, _ = fmt.Fprintf(w, `{"id":"gist-id","files":{"ip.txt":{"filename":"ip.txt","content":%q,"raw_url":"https://raw.test/same"}}}`, content)
		}))
		defer server.Close()
		settings := DefaultSettings()
		settings.Gist = GistSettings{Enabled: true, Token: "secret", GistID: "gist-id", Filename: "ip.txt"}
		result := testService(server).PublishGist(t.Context(), summary, settings)
		if result.State != "succeeded" || requests != 1 || result.URL != "https://raw.test/same" || !strings.Contains(result.Message, "无需更新") {
			t.Fatalf("result=%+v requests=%d", result, requests)
		}
	})
}

func TestGistRetriesAndRedactsToken(t *testing.T) {
	t.Run("retry", func(t *testing.T) {
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			if attempts == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"message":"retry"}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"gist-id","files":{"ip.txt":{"filename":"ip.txt","content":"old"}}}`))
		}))
		defer server.Close()
		settings := DefaultSettings()
		settings.Request.MaxRetries, settings.Request.RetryDelayMS = 1, 0
		settings.Gist = GistSettings{Enabled: true, Token: "secret", GistID: "gist-id", Filename: "ip.txt"}
		if err := testService(server).TestGist(t.Context(), settings); err != nil || attempts != 2 {
			t.Fatalf("err=%v attempts=%d", err, attempts)
		}
	})

	t.Run("authentication error", func(t *testing.T) {
		token := "gist-super-secret"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = fmt.Fprintf(w, `{"message":"bad token %s"}`, token)
		}))
		defer server.Close()
		settings := DefaultSettings()
		settings.Request.MaxRetries = 0
		settings.Gist = GistSettings{Enabled: true, Token: token, GistID: "gist-id", Filename: "ip.txt"}
		result := testService(server).PublishGist(t.Context(), testSummary(), settings)
		if result.State != "failed" || strings.Contains(result.Message, token) || !strings.Contains(result.Message, "[REDACTED]") {
			t.Fatalf("result=%+v", result)
		}
	})
}

func TestGistNotFoundAndEmptyResultsDoNotPatch(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"not found"}`))
	}))
	defer server.Close()
	settings := DefaultSettings()
	settings.Request.MaxRetries = 0
	settings.Gist = GistSettings{Enabled: true, Token: "secret", GistID: "missing", Filename: "ip.txt"}
	result := testService(server).PublishGist(t.Context(), testSummary(), settings)
	if result.State != "failed" || !strings.Contains(result.Message, "不存在") || requests != 1 {
		t.Fatalf("result=%+v requests=%d", result, requests)
	}

	requests = 0
	summary := testSummary()
	summary.Results = nil
	result = testService(server).PublishGist(t.Context(), summary, settings)
	if result.State != "skipped" || requests != 0 {
		t.Fatalf("result=%+v requests=%d", result, requests)
	}
}

func TestGistConnectionRejectsInvalidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"gist-id"}`))
	}))
	defer server.Close()
	settings := DefaultSettings()
	settings.Gist = GistSettings{Enabled: true, Token: "secret", GistID: "gist-id", Filename: "ip.txt"}
	if err := testService(server).TestGist(t.Context(), settings); err == nil || !strings.Contains(err.Error(), "响应格式无效") {
		t.Fatalf("err=%v", err)
	}
}
