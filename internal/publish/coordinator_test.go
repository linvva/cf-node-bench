package publish

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/linvva/cf-node-bench/internal/model"
)

func TestCoordinatorRunsCloudflareAndGitHubBeforeTelegram(t *testing.T) {
	var cloudflareDone atomic.Bool
	var githubDone atomic.Bool
	var telegramAfterTargets atomic.Bool
	cloudflareStarted := make(chan struct{})
	githubStarted := make(chan struct{})
	var cloudflareOnce sync.Once
	var githubOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/dns_records/batch"):
			cloudflareOnce.Do(func() { close(cloudflareStarted) })
			select {
			case <-githubStarted:
			case <-time.After(time.Second):
				t.Error("GitHub did not run in parallel with Cloudflare")
			}
			cloudflareDone.Store(true)
			_, _ = w.Write([]byte(`{"success":true}`))
		case strings.Contains(r.URL.Path, "/dns_records"):
			_, _ = w.Write([]byte(`{"success":true,"result":[],"result_info":{"total_pages":1}}`))
		case strings.Contains(r.URL.Path, "/contents/") && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"not found"}`))
		case strings.Contains(r.URL.Path, "/contents/"):
			githubOnce.Do(func() { close(githubStarted) })
			select {
			case <-cloudflareStarted:
			case <-time.After(time.Second):
				t.Error("Cloudflare did not run in parallel with GitHub")
			}
			githubDone.Store(true)
			_, _ = w.Write([]byte(`{"content":{"sha":"new"}}`))
		case strings.Contains(r.URL.Path, "/sendMessage"):
			telegramAfterTargets.Store(cloudflareDone.Load() && githubDone.Load())
			_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	settings := DefaultSettings()
	settings.Request.MaxRetries = 0
	settings.Cloudflare = CloudflareSettings{Enabled: true, APIToken: "cf", ZoneID: "zone", RecordName: "cf.example.com", RecordType: "A", TTL: 60}
	settings.GitHub = GitHubSettings{Enabled: true, Token: "gh", Owner: "owner", Repository: "repo", Branch: "main", Path: "ip.txt"}
	settings.Telegram = TelegramSettings{Enabled: true, BotToken: "tg", ChatID: "chat", ContentMode: "summary"}

	updates := make(chan Update, 16)
	coordinator := NewCoordinator(testService(server), func(update Update) { updates <- update })
	if err := coordinator.Enqueue(t.Context(), testSummary(), settings, "all"); err != nil {
		t.Fatal(err)
	}
	terminal := map[string]model.PublicationResult{}
	deadline := time.After(2 * time.Second)
	for len(terminal) < 3 {
		select {
		case update := <-updates:
			if update.Result.State == "succeeded" || update.Result.State == "failed" || update.Result.State == "skipped" {
				terminal[update.Result.Target] = update.Result
			}
		case <-deadline:
			t.Fatalf("timed out: %+v", terminal)
		}
	}
	if !telegramAfterTargets.Load() {
		t.Fatal("telegram ran before Cloudflare and GitHub completed")
	}
}

func TestCoordinatorRejectsDuplicateRun(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"sha":"old","encoding":"base64","content":"b2xk"}`))
			return
		}
		<-release
		_ = json.NewEncoder(w).Encode(map[string]any{"content": map[string]string{"sha": "new"}})
	}))
	defer server.Close()
	settings := DefaultSettings()
	settings.Request.MaxRetries = 0
	settings.GitHub = GitHubSettings{Enabled: true, Token: "gh", Owner: "owner", Repository: "repo", Branch: "main", Path: "ip.txt"}
	coordinator := NewCoordinator(testService(server), nil)
	if err := coordinator.Enqueue(t.Context(), testSummary(), settings, "github"); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Enqueue(t.Context(), testSummary(), settings, "github"); err != ErrAlreadyPublishing {
		t.Fatalf("duplicate error=%v", err)
	}
	close(release)
}
