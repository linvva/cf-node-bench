package publish

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/linvva/cf-node-bench/internal/model"
)

func testSummary() model.RunSummary {
	started := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	return model.RunSummary{
		RunID: "run-test", StartedAt: started, FinishedAt: started.Add(1250 * time.Millisecond), State: "completed",
		Results:  []model.ProbeResult{sampleResult("1.1.1.1", 443, "US"), sampleResult("1.1.1.2", 8443, "CN")},
		Failures: map[model.FailureReason]int{},
	}
}

func testService(server *httptest.Server) *Service {
	return &Service{Client: server.Client(), CloudflareBaseURL: server.URL, GitHubBaseURL: server.URL, TelegramBaseURL: server.URL, Now: func() time.Time { return time.Date(2026, 7, 30, 10, 1, 0, 0, time.UTC) }}
}

func TestCloudflareReplacesOnlyManagedRecordsAcrossTypes(t *testing.T) {
	var payload struct {
		Deletes []map[string]string `json:"deletes"`
		Posts   []cloudflareRecord  `json:"posts"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			recordType := r.URL.Query().Get("type")
			_, _ = fmt.Fprintf(w, `{"success":true,"result":[{"id":"managed-%s","comment":"%s"},{"id":"user-%s","comment":"keep"}],"result_info":{"total_pages":1}}`, recordType, managedComment, recordType)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"success":true,"result":{}}`))
	}))
	defer server.Close()
	settings := DefaultSettings()
	settings.Request.MaxRetries = 0
	settings.Cloudflare = CloudflareSettings{Enabled: true, APIToken: "secret", ZoneID: "zone", RecordName: "cf.example.com", RecordType: "A", TTL: 120, Proxied: true}
	result := testService(server).PublishCloudflare(t.Context(), testSummary(), settings)
	if result.State != "succeeded" || result.Items != 1 || result.Eligible != 1 || result.Skipped != 1 {
		t.Fatalf("result=%+v", result)
	}
	if len(payload.Deletes) != 2 || payload.Deletes[0]["id"] != "managed-A" || payload.Deletes[1]["id"] != "managed-TXT" {
		t.Fatalf("deletes=%v", payload.Deletes)
	}
	if len(payload.Posts) != 1 || payload.Posts[0].Type != "A" || payload.Posts[0].Content != "1.1.1.1" || payload.Posts[0].Proxied == nil || !*payload.Posts[0].Proxied || payload.Posts[0].Comment != managedComment {
		t.Fatalf("posts=%+v", payload.Posts)
	}
}

func TestCloudflareTXTAndEmptyA(t *testing.T) {
	requests := 0
	var post cloudflareRecord
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"success":true,"result":[],"result_info":{"total_pages":1}}`))
			return
		}
		var payload struct {
			Posts []cloudflareRecord `json:"posts"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		post = payload.Posts[0]
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()
	settings := DefaultSettings()
	settings.Cloudflare = CloudflareSettings{Enabled: true, APIToken: "secret", ZoneID: "zone", RecordName: "cf.example.com", RecordType: "TXT", TTL: 60, Proxied: true}
	result := testService(server).PublishCloudflare(t.Context(), model.RunSummary{Results: []model.ProbeResult{sampleResult("1.1.1.2", 8443, "CN")}}, settings)
	if result.State != "succeeded" || post.Type != "TXT" || post.Proxied != nil || !strings.Contains(post.Content, "1.1.1.2:8443#CN") {
		t.Fatalf("result=%+v post=%+v", result, post)
	}
	requests = 0
	settings.Cloudflare.RecordType = "A"
	result = testService(server).PublishCloudflare(t.Context(), model.RunSummary{Results: []model.ProbeResult{sampleResult("1.1.1.2", 8443, "CN")}}, settings)
	if result.State != "failed" || requests != 0 {
		t.Fatalf("empty A must not mutate DNS: result=%+v requests=%d", result, requests)
	}
}

func TestGitHubUpdatesWithSHAAndSkipsUnchangedContent(t *testing.T) {
	summary := testSummary()
	expected := FormatResults(summary.Results, DefaultSettings().Output)
	putSHA := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"sha":"old-sha","encoding":"base64","content":"b2xk"}`))
			return
		}
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		putSHA, _ = payload["sha"].(string)
		decoded, _ := base64.StdEncoding.DecodeString(payload["content"].(string))
		if string(decoded) != expected {
			t.Fatalf("content=%q", decoded)
		}
		_, _ = w.Write([]byte(`{"content":{"sha":"new"}}`))
	}))
	defer server.Close()
	settings := DefaultSettings()
	settings.GitHub = GitHubSettings{Enabled: true, Token: "secret", Owner: "owner", Repository: "repo", Branch: "main", Path: "ip.txt"}
	result := testService(server).PublishGitHub(t.Context(), summary, settings)
	if result.State != "succeeded" || putSHA != "old-sha" {
		t.Fatalf("result=%+v sha=%q", result, putSHA)
	}

	requests := 0
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		encoded := base64.StdEncoding.EncodeToString([]byte(expected))
		_, _ = fmt.Fprintf(w, `{"sha":"same","encoding":"base64","content":"%s"}`, encoded)
	})
	result = testService(server).PublishGitHub(t.Context(), summary, settings)
	if result.State != "succeeded" || requests != 1 || !strings.Contains(result.Message, "无需提交") {
		t.Fatalf("unchanged result=%+v requests=%d", result, requests)
	}
}

func TestTelegramSummaryDetailsAndRetry(t *testing.T) {
	var mu sync.Mutex
	texts := []string{}
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"ok":false,"description":"retry"}`))
			return
		}
		var payload struct {
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		mu.Lock()
		texts = append(texts, payload.Text)
		mu.Unlock()
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer server.Close()
	settings := DefaultSettings()
	settings.Request.MaxRetries, settings.Request.RetryDelayMS = 1, 0
	settings.Cloudflare.Enabled = true
	settings.GitHub.Enabled = true
	settings.Gist.Enabled = true
	settings.Telegram = TelegramSettings{Enabled: true, BotToken: "secret", ChatID: "123", ContentMode: "summary", DeliveryMode: TelegramDeliveryDirect}
	outcomes := map[string]model.PublicationResult{"cloudflare": {State: "succeeded", Items: 1}, "github": {State: "failed", Message: "denied"}, "gist": {State: "succeeded", Items: 2}}
	result := testService(server).PublishTelegram(t.Context(), testSummary(), settings, outcomes)
	if result.State != "succeeded" || attempts != 2 || result.Items != 2 || len(texts) != 1 || strings.Contains(texts[0], "1.1.1.1") || !strings.Contains(texts[0], "GitHub 仓库：失败") || !strings.Contains(texts[0], "GitHub Gist：成功") {
		t.Fatalf("summary result=%+v attempts=%d texts=%v", result, attempts, texts)
	}

	texts, attempts = nil, 1
	settings.Telegram.ContentMode = "details"
	result = testService(server).PublishTelegram(t.Context(), testSummary(), settings, outcomes)
	if result.State != "succeeded" || len(texts) != 2 || !strings.Contains(texts[1], "1.1.1.2:8443#CN") {
		t.Fatalf("details result=%+v texts=%v", result, texts)
	}
}

func TestTelegramErrorsRedactToken(t *testing.T) {
	token := "super-secret-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(w, `{"description":"%s"}`, token)
	}))
	defer server.Close()
	settings := DefaultSettings()
	settings.Request.MaxRetries = 0
	settings.Telegram = TelegramSettings{Enabled: true, BotToken: token, ChatID: "123", ContentMode: "summary", DeliveryMode: TelegramDeliveryDirect}
	result := testService(server).PublishTelegram(context.Background(), testSummary(), settings, nil)
	if strings.Contains(result.Message, token) || !strings.Contains(result.Message, "[REDACTED]") {
		t.Fatalf("message=%q", result.Message)
	}
}

func TestGitHubConflictRefreshesSHA(t *testing.T) {
	getCount, putCount := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			getCount++
			sha := "old"
			if getCount > 1 {
				sha = "refreshed"
			}
			_, _ = fmt.Fprintf(w, `{"sha":"%s","encoding":"base64","content":"b2xk"}`, sha)
			return
		}
		putCount++
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if putCount == 1 {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"message":"conflict"}`))
			return
		}
		if payload["sha"] != "refreshed" {
			t.Errorf("sha=%v", payload["sha"])
		}
		_, _ = w.Write([]byte(`{"content":{"sha":"new"}}`))
	}))
	defer server.Close()
	settings := DefaultSettings()
	settings.Request.MaxRetries = 0
	settings.GitHub = GitHubSettings{Enabled: true, Token: "secret", Owner: "owner", Repository: "repo", Branch: "main", Path: "ip.txt"}
	result := testService(server).PublishGitHub(t.Context(), testSummary(), settings)
	if result.State != "succeeded" || getCount != 2 || putCount != 2 {
		t.Fatalf("result=%+v get=%d put=%d", result, getCount, putCount)
	}
}

func TestCloudflareListsManagedRecordsAcrossPages(t *testing.T) {
	pages := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			pages++
			page := r.URL.Query().Get("page")
			totalPages := 1
			if r.URL.Query().Get("type") == "A" {
				totalPages = 2
			}
			_, _ = fmt.Fprintf(w, `{"success":true,"result":[{"id":"%s-%s","comment":"%s"}],"result_info":{"total_pages":%d}}`, r.URL.Query().Get("type"), page, managedComment, totalPages)
			return
		}
		var payload struct {
			Deletes []map[string]string `json:"deletes"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if len(payload.Deletes) != 3 {
			t.Errorf("deletes=%v", payload.Deletes)
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()
	settings := DefaultSettings()
	settings.Request.MaxRetries = 0
	settings.Cloudflare = CloudflareSettings{Enabled: true, APIToken: "secret", ZoneID: "zone", RecordName: "cf.example.com", RecordType: "A", TTL: 60}
	result := testService(server).PublishCloudflare(t.Context(), testSummary(), settings)
	if result.State != "succeeded" || pages != 3 {
		t.Fatalf("result=%+v pages=%d", result, pages)
	}
}

func TestDirectTransportAndTelegramZeroResult(t *testing.T) {
	transport, ok := NewService().Client.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatal("publish client must not use environment or system proxies")
	}
	text := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		text = payload.Text
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer server.Close()
	settings := DefaultSettings()
	settings.Request.MaxRetries = 0
	settings.Telegram = TelegramSettings{Enabled: true, BotToken: "secret", ChatID: "chat", ContentMode: "details", DeliveryMode: TelegramDeliveryDirect}
	summary := testSummary()
	summary.Results = nil
	result := testService(server).PublishTelegram(t.Context(), summary, settings, nil)
	if result.State != "succeeded" || !strings.Contains(text, "通过节点：0") || strings.Contains(text, "节点列表") {
		t.Fatalf("result=%+v text=%q", result, text)
	}
}

func TestTelegramRelayForwardsOnlyRequiredRequestData(t *testing.T) {
	botToken, relayKey := "123456:bot-secret", "relay-secret"
	methods := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/telegram" || strings.Contains(r.URL.String(), botToken) {
			t.Errorf("unexpected relay URL: %s", r.URL.String())
		}
		if r.Header.Get("Authorization") != "Bearer "+relayKey {
			t.Errorf("authorization=%q", r.Header.Get("Authorization"))
		}
		var request struct {
			BotToken string         `json:"botToken"`
			Method   string         `json:"method"`
			Payload  map[string]any `json:"payload"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.BotToken != botToken || request.Payload["chat_id"] != "chat" {
			t.Errorf("request=%+v", request)
		}
		methods = append(methods, request.Method)
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer server.Close()
	settings := DefaultSettings()
	settings.Request.MaxRetries = 0
	settings.Telegram = TelegramSettings{Enabled: true, BotToken: botToken, ChatID: "chat", ContentMode: "summary", DeliveryMode: TelegramDeliveryRelay, RelayURL: server.URL + "/telegram", RelayKey: relayKey}
	service := testService(server)
	if err := service.TestTelegram(t.Context(), settings); err != nil {
		t.Fatal(err)
	}
	result := service.PublishTelegram(t.Context(), testSummary(), settings, nil)
	if result.State != "succeeded" || len(methods) != 2 || methods[0] != "getChat" || methods[1] != "sendMessage" {
		t.Fatalf("result=%+v methods=%v", result, methods)
	}
}

func TestTelegramRelayErrorsRedactBothCredentials(t *testing.T) {
	botToken, relayKey := "123456:bot-secret", "relay-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(w, `{"ok":false,"description":"%s %s"}`, botToken, relayKey)
	}))
	defer server.Close()
	settings := DefaultSettings()
	settings.Request.MaxRetries = 0
	settings.Telegram = TelegramSettings{Enabled: true, BotToken: botToken, ChatID: "chat", ContentMode: "summary", DeliveryMode: TelegramDeliveryRelay, RelayURL: server.URL + "/telegram", RelayKey: relayKey}
	result := testService(server).PublishTelegram(t.Context(), testSummary(), settings, nil)
	if strings.Contains(result.Message, botToken) || strings.Contains(result.Message, relayKey) || !strings.Contains(result.Message, "[REDACTED]") {
		t.Fatalf("message=%q", result.Message)
	}
}
