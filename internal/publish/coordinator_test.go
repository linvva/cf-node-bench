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

func TestCoordinatorRunsUpstreamTargetsInParallelBeforeTelegram(t *testing.T) {
	var cloudflareDone atomic.Bool
	var githubDone atomic.Bool
	var gistDone atomic.Bool
	var telegramAfterTargets atomic.Bool
	cloudflareStarted := make(chan struct{})
	githubStarted := make(chan struct{})
	gistStarted := make(chan struct{})
	var cloudflareOnce sync.Once
	var githubOnce sync.Once
	var gistOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/dns_records/batch"):
			cloudflareOnce.Do(func() { close(cloudflareStarted) })
			select {
			case <-githubStarted:
			case <-time.After(time.Second):
				t.Error("GitHub did not run in parallel with Cloudflare")
			}
			select {
			case <-gistStarted:
			case <-time.After(time.Second):
				t.Error("Gist did not run in parallel with Cloudflare")
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
			select {
			case <-gistStarted:
			case <-time.After(time.Second):
				t.Error("Gist did not run in parallel with GitHub")
			}
			githubDone.Store(true)
			_, _ = w.Write([]byte(`{"content":{"sha":"new"}}`))
		case strings.Contains(r.URL.Path, "/gists/") && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"id":"gist-id","files":{"ip.txt":{"filename":"ip.txt","content":"old"}}}`))
		case strings.Contains(r.URL.Path, "/gists/"):
			gistOnce.Do(func() { close(gistStarted) })
			select {
			case <-cloudflareStarted:
			case <-time.After(time.Second):
				t.Error("Cloudflare did not run in parallel with Gist")
			}
			select {
			case <-githubStarted:
			case <-time.After(time.Second):
				t.Error("GitHub did not run in parallel with Gist")
			}
			gistDone.Store(true)
			_, _ = w.Write([]byte(`{"id":"gist-id","files":{"ip.txt":{"filename":"ip.txt","content":"new","raw_url":"https://raw.test/ip.txt"}}}`))
		case strings.Contains(r.URL.Path, "/sendMessage"):
			telegramAfterTargets.Store(cloudflareDone.Load() && githubDone.Load() && gistDone.Load())
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
	settings.Gist = GistSettings{Enabled: true, Token: "gist", GistID: "gist-id", Filename: "ip.txt"}
	settings.Telegram = TelegramSettings{Enabled: true, BotToken: "tg", ChatID: "chat", ContentMode: "summary"}

	updates := make(chan Update, 16)
	coordinator := NewCoordinator(testService(server), func(update Update) { updates <- update })
	if err := coordinator.Enqueue(t.Context(), testSummary(), settings, "all"); err != nil {
		t.Fatal(err)
	}
	terminal := map[string]model.PublicationResult{}
	deadline := time.After(2 * time.Second)
	for len(terminal) < 4 {
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
		t.Fatal("telegram ran before Cloudflare, GitHub and Gist completed")
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

func TestCoordinatorSelectsDestinationsFromRunProtocol(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record := func(value string) { mu.Lock(); seen[value]++; mu.Unlock() }
		switch {
		case strings.Contains(r.URL.Path, "/dns_records"):
			if r.Header.Get("Authorization") != "Bearer shared-cf" {
				t.Error("Cloudflare did not use shared token")
			}
			if r.Method == http.MethodGet {
				record("cf-query:" + r.URL.Query().Get("name"))
				_, _ = w.Write([]byte(`{"success":true,"result":[],"result_info":{"total_pages":1}}`))
			} else {
				var body struct {
					Posts []cloudflareRecord `json:"posts"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				for _, post := range body.Posts {
					record("cf-post:" + post.Name + ":" + post.Type)
				}
				_, _ = w.Write([]byte(`{"success":true}`))
			}
		case strings.Contains(r.URL.Path, "/contents/"):
			if r.Header.Get("Authorization") != "Bearer shared-gh" {
				t.Error("GitHub did not use shared token")
			}
			if r.Method == http.MethodGet {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			record("github:" + r.URL.Path)
			_, _ = w.Write([]byte(`{"content":{"sha":"new"}}`))
		case strings.Contains(r.URL.Path, "/gists/"):
			if r.Header.Get("Authorization") != "Bearer shared-gist" {
				t.Error("Gist did not use shared token")
			}
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`{"id":"found","files":{}}`))
				return
			}
			var body struct {
				Files map[string]map[string]string `json:"files"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			for filename, file := range body.Files {
				record("gist:" + r.URL.Path + ":" + filename)
				file["filename"] = filename
				file["raw_url"] = "https://raw.example.test/" + filename
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "updated", "files": body.Files})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	settings := DefaultSettings()
	settings.Request.MaxRetries = 0
	settings.Cloudflare = CloudflareSettings{Enabled: true, APIToken: "shared-cf", ZoneID: "zone", RecordName: "ordinary.example.com", RecordType: "A", TTL: 60, H3: CloudflareH3Target{RecordName: "h3.example.com", RecordType: "TXT"}}
	settings.GitHub = GitHubSettings{Enabled: true, Token: "shared-gh", Owner: "owner", Repository: "repo", Branch: "main", Path: "ip.txt", H3: GitHubH3Target{Path: "h3.txt"}}
	settings.Gist = GistSettings{Enabled: true, Token: "shared-gist", GistID: "ordinary-gist", Filename: "ip.txt", H3: GistH3Target{GistID: "h3-gist", Filename: "h3.txt"}}
	updates := make(chan Update, 32)
	coordinator := NewCoordinator(testService(server), func(u Update) { updates <- u })
	for _, mode := range []model.Mode{model.ModeH3, model.ModeTCPHTTPS} {
		summary := testSummary()
		summary.RunID = string(mode)
		summary.Mode = mode
		if err := coordinator.Enqueue(t.Context(), summary, settings, "all"); err != nil {
			t.Fatal(err)
		}
	}
	completed := 0
	for completed < 6 {
		select {
		case update := <-updates:
			if update.Result.State == "failed" || update.Result.State == "skipped" {
				t.Fatalf("publish failed: %+v", update)
			}
			if update.Result.State == "succeeded" {
				completed++
			}
		case <-time.After(3 * time.Second):
			t.Fatal("publication did not complete")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	for _, key := range []string{"cf-query:ordinary.example.com", "cf-query:h3.example.com", "cf-post:ordinary.example.com:A", "cf-post:h3.example.com:TXT", "github:/repos/owner/repo/contents/ip.txt", "github:/repos/owner/repo/contents/h3.txt", "gist:/gists/ordinary-gist:ip.txt", "gist:/gists/h3-gist:h3.txt"} {
		if seen[key] == 0 {
			t.Errorf("destination was not used: %s; seen=%v", key, seen)
		}
	}
}

func TestCoordinatorKeepsResolvedDestinationSnapshotForRetry(t *testing.T) {
	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		requests <- r.URL.Path + "|" + r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"content":{"sha":"new"}}`))
	}))
	defer server.Close()
	settings := DefaultSettings()
	settings.Request.MaxRetries = 0
	settings.GitHub = GitHubSettings{Enabled: true, Token: "shared-old", Owner: "owner", Repository: "repo", Branch: "main", Path: "ip.txt", H3: GitHubH3Target{Path: "h3.txt"}}
	updates := make(chan Update, 4)
	coordinator := NewCoordinator(testService(server), func(u Update) { updates <- u })
	// Hold the queue as though an older job were still running.
	coordinator.working = true
	summary := testSummary()
	summary.Mode = model.ModeH3
	summary.Publications = []model.PublicationResult{{Target: "github", State: "failed"}}
	if err := coordinator.Enqueue(t.Context(), summary, settings, "github"); err != nil {
		t.Fatal(err)
	}
	settings.GitHub.Token = "shared-new"
	settings.GitHub.H3.Path = "changed.txt"
	settings.GitHub.H3 = GitHubH3Target{}
	coordinator.work()
	select {
	case got := <-requests:
		if got != "/repos/owner/repo/contents/h3.txt|Bearer shared-old" {
			t.Fatalf("retry used changed configuration: %s", got)
		}
	case <-time.After(time.Second):
		t.Fatal("retry did not publish")
	}
}

func TestCoordinatorRejectsConflictingIndependentTargets(t *testing.T) {
	settings := DefaultSettings()
	settings.GitHub.Enabled = true
	settings.GitHub.Path = "ip.txt"
	settings.GitHub.H3 = GitHubH3Target{Path: settings.GitHub.Path}
	coordinator := NewCoordinator(nil, nil)
	summary := testSummary()
	summary.Mode = model.ModeH3
	if err := coordinator.Enqueue(t.Context(), summary, settings, "github"); err == nil || !strings.Contains(err.Error(), "必须与普通测速不同") {
		t.Fatalf("conflicting target enqueued: %v", err)
	}
	if len(coordinator.queue) != 0 || len(coordinator.pending) != 0 {
		t.Fatal("invalid job changed queue")
	}
}

func TestCoordinatorMissingH3DestinationsNeverRequestsOrdinaryTargets(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	settings := DefaultSettings()
	settings.Cloudflare = CloudflareSettings{Enabled: true, APIToken: "cf", ZoneID: "zone", RecordName: "normal.example.com", RecordType: "A", TTL: 60}
	settings.GitHub = GitHubSettings{Enabled: true, Token: "gh", Owner: "owner", Repository: "repo", Branch: "main", Path: "ip.txt"}
	settings.Gist = GistSettings{Enabled: true, Token: "gist", GistID: "normal-gist", Filename: "ip.txt"}
	coordinator := NewCoordinator(testService(server), nil)
	summary := testSummary()
	summary.Mode = model.ModeH3
	for _, target := range []string{"all", "cloudflare", "github", "gist"} {
		if err := coordinator.Enqueue(t.Context(), summary, settings, target); err != ErrNoTargets {
			t.Fatalf("target %s accepted missing H3 destination: %v", target, err)
		}
	}
	if requests.Load() != 0 || len(coordinator.queue) != 0 || len(coordinator.pending) != 0 || coordinator.working {
		t.Fatal("missing H3 destinations started a publishing job")
	}
}

func TestCoordinatorAllSkipsUnconfiguredH3ChannelsAndKeepsTelegram(t *testing.T) {
	var githubRequests, telegramRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/contents/h3.txt":
			githubRequests.Add(1)
			if r.Method == http.MethodGet {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(`{"content":{"sha":"new"}}`))
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			telegramRequests.Add(1)
			_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
		default:
			t.Errorf("unconfigured H3 channel received request: %s", r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	settings := DefaultSettings()
	settings.Request.MaxRetries = 0
	settings.Cloudflare = CloudflareSettings{Enabled: true, APIToken: "cf", ZoneID: "zone", RecordName: "normal.example.com", RecordType: "A", TTL: 60}
	settings.GitHub = GitHubSettings{Enabled: true, Token: "gh", Owner: "owner", Repository: "repo", Branch: "main", H3: GitHubH3Target{Path: "h3.txt"}}
	settings.Gist = GistSettings{Enabled: true, Token: "gist", GistID: "normal-gist", Filename: "ip.txt"}
	settings.Telegram.Enabled, settings.Telegram.BotToken, settings.Telegram.ChatID = true, "tg", "chat"
	updates := make(chan Update, 8)
	coordinator := NewCoordinator(testService(server), func(update Update) { updates <- update })
	summary := testSummary()
	summary.Mode = model.ModeH3
	if err := coordinator.Enqueue(t.Context(), summary, settings, "all"); err != nil {
		t.Fatal(err)
	}
	completed := 0
	for completed < 2 {
		select {
		case update := <-updates:
			if update.Result.Target != "github" && update.Result.Target != "telegram" {
				t.Fatalf("unconfigured target was queued: %+v", update)
			}
			if update.Result.State == "failed" || update.Result.State == "skipped" {
				t.Fatalf("configured target failed: %+v", update)
			}
			if update.Result.State == "succeeded" {
				completed++
			}
		case <-time.After(2 * time.Second):
			t.Fatal("configured targets did not finish")
		}
	}
	if githubRequests.Load() != 2 || telegramRequests.Load() != 1 {
		t.Fatalf("requests github=%d telegram=%d", githubRequests.Load(), telegramRequests.Load())
	}
}
