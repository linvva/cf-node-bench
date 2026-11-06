package storage

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
	"github.com/linvva/cf-node-bench/internal/publish"
	runengine "github.com/linvva/cf-node-bench/internal/run"
)

func writeLegacyData(t *testing.T, path string, active model.Mode) []byte {
	t.Helper()
	tcp := publish.DefaultSettings()
	tcp.Cloudflare = publish.CloudflareSettings{Enabled: true, APIToken: "tcp-token", ZoneID: "tcp-zone", RecordName: "tcp.example.com", RecordType: "A", TTL: 60}
	// A partial TCP configuration still belongs together; never borrow an H3 credential.
	tcp.GitHub.Owner = "tcp-owner"
	h3 := publish.DefaultSettings()
	h3.Cloudflare = publish.CloudflareSettings{Enabled: true, APIToken: "h3-token", ZoneID: "h3-zone", RecordName: "h3.example.com", RecordType: "TXT", TTL: 120}
	h3.GitHub = publish.GitHubSettings{Enabled: true, Token: "h3-github", Owner: "h3-owner", Repository: "repo", Branch: "main", Path: "h3.txt"}
	h3.Gist = publish.GistSettings{Enabled: true, Token: "h3-gist", GistID: "h3-id", Filename: "h3.txt"}
	h3.Telegram = publish.TelegramSettings{Enabled: true, BotToken: "h3-bot", ChatID: "h3-chat", ContentMode: "details", DeliveryMode: publish.TelegramDeliveryRelay, RelayURL: "https://relay.example.com/telegram", RelayKey: "h3-key"}
	old := map[string]any{
		"activeMode": active,
		"settings":   map[string]any{"mode": "tcp_https", "tcpConcurrency": 27, "httpsConcurrency": 7, "tcpProbeCount": 4, "httpsProbeCount": 5, "tcpCandidateCount": 80, "bandwidthCandidates": 10, "finalResultCount": 5, "allowedPorts": []int{443}, "connectTimeoutMs": 1600, "requestTimeoutMs": 4500, "bandwidthTestUrl": "https://tcp.example.com/download", "scoreWeights": map[string]int{"bandwidth": 40, "tcp": 15, "https": 25, "reliability": 15, "jitter": 5}},
		"h3Settings": map[string]any{"mode": "h3", "workerHost": "Worker.Example.com.", "connectTimeoutMs": 5500, "requestTimeoutMs": 8500, "bandwidthTestUrl": "https://h3.example.com/download", "quicConcurrency": 90, "http3Concurrency": 40, "finalResultCount": 12},
		"publish":    tcp, "h3Publish": h3,
		"history": []model.RunSummary{{RunID: "h3", Mode: model.ModeH3, State: "completed", Results: []model.ProbeResult{{Mode: model.ModeH3, Score: 91, QUIC: model.ProbeStats{P95MS: 28}}}, ScoreWeights: model.ScoreWeights{QUIC: 15, HTTP3: 25, Bandwidth: 40, Reliability: 15, Jitter: 5}}, {RunID: "tcp", Mode: model.ModeTCPHTTPS, State: "completed", Results: []model.ProbeResult{{Mode: model.ModeTCPHTTPS, Score: 82, TCP: model.ProbeStats{P95MS: 38}}}}},
	}
	content, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return content
}

func writeLegacyCheckpoint(t *testing.T, path, runID string, mode model.Mode) []byte {
	t.Helper()
	settings := map[string]any{"mode": mode, "workerHost": "worker.example.com", "connectTimeoutMs": 4500, "requestTimeoutMs": 6500, "bandwidthTestUrl": "https://old.example.com/file", "quicConcurrency": 19, "http3Concurrency": 9}
	checkpoint := map[string]any{"version": runengine.CheckpointVersion, "runId": runID, "settings": settings, "stageSettings": map[string]any{"quic": settings}, "completedAt": map[string]time.Time{"quic": time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)}, "stages": []model.StageProgress{{Name: "quic", State: "completed"}}, "quicResults": []model.ProbeResult{{Mode: mode, Score: 74, QUIC: model.ProbeStats{P95MS: 22}}}}
	var output bytes.Buffer
	writer := gzip.NewWriter(&output)
	if err := json.NewEncoder(writer).Encode(checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	content := output.Bytes()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return content
}

func TestUnifiedMigrationKeepsWholeTargetsHistoryAndActiveCheckpoint(t *testing.T) {
	for _, active := range []model.Mode{model.ModeTCPHTTPS, model.ModeH3} {
		t.Run(string(active), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "data.json")
			original := writeLegacyData(t, path, active)
			tcp := writeLegacyCheckpoint(t, filepath.Join(dir, "checkpoint.json.gz"), "tcp-checkpoint", model.ModeTCPHTTPS)
			h3 := writeLegacyCheckpoint(t, filepath.Join(dir, "checkpoint-h3.json.gz"), "h3-checkpoint", model.ModeH3)
			store, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			s := store.Settings()
			if s.RequireH3 != (active == model.ModeH3) || s.ConnectionConcurrency != 27 || s.HTTPConcurrency != 7 || s.ConnectionProbeCount != 4 || s.HTTPProbeCount != 5 || s.FinalResultCount != 5 || s.ConnectTimeoutMS != 1600 || s.RequestTimeoutMS != 4500 || s.BandwidthTestURL != "https://tcp.example.com/download" {
				t.Fatalf("lost TCP common settings: %+v", s)
			}
			if s.VerificationHost != "worker.example.com" || s.H3ConnectTimeoutMS != 5500 || s.H3RequestTimeoutMS != 8500 || s.H3BandwidthTestURL != "https://h3.example.com/download" {
				t.Fatalf("lost H3 options: %+v", s)
			}
			p := store.PublishSettings()
			if p.Cloudflare.APIToken != "tcp-token" || p.Cloudflare.ZoneID != "tcp-zone" || p.Cloudflare.RecordType != "A" || p.GitHub.Owner != "tcp-owner" || p.GitHub.Token != "" {
				t.Fatal("TCP target and credential were mixed with H3")
			}
			if p.Gist.Token != "h3-gist" || p.Gist.GistID != "h3-id" || p.Gist.Filename != "h3.txt" || p.Telegram.BotToken != "h3-bot" || p.Telegram.RelayKey != "h3-key" || p.Telegram.ChatID != "h3-chat" {
				t.Fatal("H3-only targets were not copied as a whole")
			}
			if !strings.Contains(store.MigrationNotice(), "Cloudflare") || !strings.Contains(store.MigrationNotice(), "GitHub") || strings.Contains(store.MigrationNotice(), "token") {
				t.Fatal("missing or unsafe conflict notice")
			}
			history := store.History()
			if len(history) != 2 || history[0].Mode != model.ModeH3 || history[0].Results[0].Score != 91 || history[0].Results[0].QUIC.P95MS != 28 || history[0].ScoreWeights.QUIC != 15 || history[1].Mode != model.ModeTCPHTTPS || history[1].Results[0].TCP.P95MS != 38 {
				t.Fatal("historical protocol or measurements changed")
			}
			c, ok, err := store.Checkpoint()
			if err != nil || !ok {
				t.Fatalf("checkpoint: %v %v", ok, err)
			}
			expected := "tcp-checkpoint"
			if active == model.ModeH3 {
				expected = "h3-checkpoint"
			}
			if c.RunID != expected || c.Settings.RequireH3 != (active == model.ModeH3) || c.CompletedAt["quic"].IsZero() {
				t.Fatalf("wrong selected checkpoint: %+v", c)
			}
			if active == model.ModeH3 && (c.Settings.ConnectionConcurrency != 19 || c.StageSettings["quic"].H3ConnectTimeoutMS != 4500) {
				t.Fatal("old H3 checkpoint settings not migrated")
			}
			if _, err := os.Stat(filepath.Join(dir, "checkpoint-h3.json.gz")); !os.IsNotExist(err) {
				t.Fatal("second active checkpoint remained")
			}
			backups := map[string][]byte{"data.json": original, "checkpoint.json.gz": tcp, "checkpoint-h3.json.gz": h3}
			for name, want := range backups {
				backup := filepath.Join(dir, name+migrationBackupSuffix)
				got, err := os.ReadFile(backup)
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("backup %s changed", name)
				}
				info, err := os.Stat(backup)
				if err != nil || info.Mode().Perm() != 0o600 {
					t.Fatalf("backup %s permissions", name)
				}
			}
			migrated, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var document map[string]json.RawMessage
			if err := json.Unmarshal(migrated, &document); err != nil {
				t.Fatal(err)
			}
			if document["activeMode"] != nil || document["h3Settings"] != nil || document["h3Publish"] != nil {
				t.Fatal("split profiles still persisted")
			}
			if _, err := Open(path); err != nil {
				t.Fatal(err)
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(after, migrated) {
				t.Fatal("second open migrated again")
			}
			backup, _ := os.ReadFile(path + migrationBackupSuffix)
			if !bytes.Equal(backup, original) {
				t.Fatal("reopen overwrote original backup")
			}
		})
	}
}

func TestMigrationWithoutActiveCheckpointDoesNotUseOtherMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")
	writeLegacyData(t, path, model.ModeH3)
	original := writeLegacyCheckpoint(t, filepath.Join(dir, "checkpoint.json.gz"), "tcp", model.ModeTCPHTTPS)
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.Checkpoint(); err != nil || ok {
		t.Fatal("inactive checkpoint reused")
	}
	backup, _ := os.ReadFile(filepath.Join(dir, "checkpoint.json.gz"+migrationBackupSuffix))
	if !bytes.Equal(backup, original) {
		t.Fatal("inactive checkpoint not preserved")
	}
}

func TestMigrationFailureLeavesSourcesUntouchedAndCanRetry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")
	data := writeLegacyData(t, path, model.ModeH3)
	tcpPath, h3Path := filepath.Join(dir, "checkpoint.json.gz"), filepath.Join(dir, "checkpoint-h3.json.gz")
	tcp := writeLegacyCheckpoint(t, tcpPath, "tcp", model.ModeTCPHTTPS)
	h3 := writeLegacyCheckpoint(t, h3Path, "h3", model.ModeH3)
	// A backup collision must never be overwritten or allow a partial migration.
	backupPath := h3Path + migrationBackupSuffix
	if err := os.Mkdir(backupPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("backup failure ignored")
	}
	for filename, want := range map[string][]byte{path: data, tcpPath: tcp, h3Path: h3} {
		got, _ := os.ReadFile(filename)
		if !bytes.Equal(got, want) {
			t.Fatalf("partial migration at %s", filename)
		}
	}
	if err := os.Remove(backupPath); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
}

func TestMigrationNeverOverwritesExistingDifferentDataBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	original := writeLegacyData(t, path, model.ModeTCPHTTPS)
	sentinel := []byte("existing-backup")
	if err := os.WriteFile(path+migrationBackupSuffix, sentinel, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("conflicting backup accepted")
	}
	got, _ := os.ReadFile(path)
	backup, _ := os.ReadFile(path + migrationBackupSuffix)
	if !bytes.Equal(got, original) || !bytes.Equal(backup, sentinel) {
		t.Fatal("original or backup overwritten")
	}
}

func TestMigrationResumesAfterCheckpointCommitBeforeDataCommit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")
	data := writeLegacyData(t, path, model.ModeH3)
	tcpPath, h3Path := filepath.Join(dir, "checkpoint.json.gz"), filepath.Join(dir, "checkpoint-h3.json.gz")
	tcp := writeLegacyCheckpoint(t, tcpPath, "tcp", model.ModeTCPHTTPS)
	h3 := writeLegacyCheckpoint(t, h3Path, "h3", model.ModeH3)
	for name, content := range map[string][]byte{path: data, tcpPath: tcp, h3Path: h3} {
		if err := backupMigrationFile(name, content); err != nil {
			t.Fatal(err)
		}
	}
	reader, err := gzip.NewReader(bytes.NewReader(h3))
	if err != nil {
		t.Fatal(err)
	}
	var converted runengine.Checkpoint
	if err := json.NewDecoder(reader).Decode(&converted); err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	var staged bytes.Buffer
	writer := gzip.NewWriter(&staged)
	if err := json.NewEncoder(writer).Encode(converted); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tcpPath, staged.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(h3Path); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, ok, err := store.Checkpoint()
	if err != nil || !ok || checkpoint.RunID != "h3" {
		t.Fatal("interrupted migration not recovered")
	}
	backup, _ := os.ReadFile(tcpPath + migrationBackupSuffix)
	if !bytes.Equal(backup, tcp) {
		t.Fatal("original TCP backup overwritten")
	}
}

func TestUnifiedHistoryKeepsFortyRunsAcrossProtocols(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 44; i++ {
		mode := model.ModeTCPHTTPS
		if i%2 == 0 {
			mode = model.ModeH3
		}
		if err := store.AddHistory(model.RunSummary{RunID: fmt.Sprint(i), Mode: mode, State: "completed"}); err != nil {
			t.Fatal(err)
		}
	}
	if history := store.History(); len(history) != 40 || history[0].RunID != "43" || history[39].RunID != "4" {
		t.Fatal("history does not use a shared 40-run limit")
	}
}

func TestUnifiedSettingsPreserveBothTransportOptions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	settings := store.Settings()
	settings.RequireH3 = true
	settings.VerificationHost = "worker.example.com"
	settings.ConnectionConcurrency = 31
	if err := store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	settings.RequireH3 = false
	if err := store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Settings(); got.RequireH3 || got.ConnectionConcurrency != 31 || got.VerificationHost != "worker.example.com" || got.H3BandwidthTestURL != config.DefaultH3BandwidthTestURL {
		t.Fatal("changing H3 requirement reset settings")
	}
}

func TestCorruptSelectedCheckpointPreventsPartialMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")
	original := writeLegacyData(t, path, model.ModeH3)
	checkpointPath := filepath.Join(dir, "checkpoint-h3.json.gz")
	corrupt := []byte("invalid checkpoint")
	if err := os.WriteFile(checkpointPath, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("corrupt checkpoint accepted")
	}
	data, _ := os.ReadFile(path)
	checkpoint, _ := os.ReadFile(checkpointPath)
	if !bytes.Equal(data, original) || !bytes.Equal(checkpoint, corrupt) {
		t.Fatal("failed migration changed source files")
	}
}

func TestUnifiedConfigDoesNotSilentlyRepairInvalidWeights(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSettings(store.Settings()); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(content, &raw); err != nil {
		t.Fatal(err)
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(raw["settings"], &settings); err != nil {
		t.Fatal(err)
	}
	settings["scoreWeights"] = json.RawMessage(`{"connection":0,"http":0,"bandwidth":0,"reliability":0,"jitter":0}`)
	raw["settings"], err = json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	invalid, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, invalid, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("invalid unified weights silently replaced with defaults")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, invalid) {
		t.Fatal("invalid unified file rewritten")
	}
}

func TestMigrationNeverReplacesCheckpointWithUnrelatedBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")
	original := writeLegacyData(t, path, model.ModeTCPHTTPS)
	checkpointPath := filepath.Join(dir, "checkpoint.json.gz")
	checkpoint := writeLegacyCheckpoint(t, checkpointPath, "current", model.ModeTCPHTTPS)
	backup := writeLegacyCheckpoint(t, checkpointPath+migrationBackupSuffix, "unrelated", model.ModeTCPHTTPS)
	if _, err := Open(path); err == nil {
		t.Fatal("unrelated checkpoint backup replaced the original")
	}
	dataAfter, _ := os.ReadFile(path)
	checkpointAfter, _ := os.ReadFile(checkpointPath)
	backupAfter, _ := os.ReadFile(checkpointPath + migrationBackupSuffix)
	if !bytes.Equal(dataAfter, original) || !bytes.Equal(checkpointAfter, checkpoint) || !bytes.Equal(backupAfter, backup) {
		t.Fatal("conflicting checkpoint backup changed original files")
	}
}

func TestMigrationCurrentResultFollowsActiveCheckpointWithoutReorderingHistory(t *testing.T) {
	for _, checkpointID := range []string{"h3", "missing-history"} {
		t.Run(checkpointID, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "data.json")
			body := writeLegacyData(t, path, model.ModeH3)
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(body, &raw); err != nil {
				t.Fatal(err)
			}
			var history []model.RunSummary
			if err := json.Unmarshal(raw["history"], &history); err != nil {
				t.Fatal(err)
			}
			history[0], history[1] = history[1], history[0]
			raw["history"], _ = json.Marshal(history)
			body, _ = json.Marshal(raw)
			if err := os.WriteFile(path, body, 0o600); err != nil {
				t.Fatal(err)
			}
			writeLegacyCheckpoint(t, filepath.Join(dir, "checkpoint-h3.json.gz"), checkpointID, model.ModeH3)
			store, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			expected := "h3"
			if checkpointID == "missing-history" {
				expected = "tcp"
			}
			if store.CurrentResultID() != expected || store.History()[0].RunID != "tcp" || store.History()[1].RunID != "h3" {
				t.Fatal("selected result lost or chronological history reordered")
			}
			store, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
			if store.CurrentResultID() != expected {
				t.Fatal("current result did not survive reopen")
			}
			if err := store.AddHistory(model.RunSummary{RunID: "new-run", Mode: model.ModeTCPHTTPS, State: "cancelled"}); err != nil {
				t.Fatal(err)
			}
			if store.CurrentResultID() != "new-run" {
				t.Fatal("new run did not replace current result")
			}
			store, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
			if store.CurrentResultID() != "new-run" {
				t.Fatal("new current result not persisted")
			}
		})
	}
}

func TestCurrentResultOfEmptyStoreIsEmpty(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	if store.CurrentResultID() != "" {
		t.Fatal("empty store has current result")
	}
}

func TestMigrationBackupIsCompleteAsSoonAsItBecomesVisible(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	content := bytes.Repeat([]byte("complete-backup\n"), 512*1024)
	done := make(chan error, 1)
	go func() { done <- backupMigrationFile(path, content) }()
	for {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path + migrationBackupSuffix)
			if err != nil || !bytes.Equal(got, content) {
				t.Fatal("published backup is incomplete")
			}
			leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".unified-migration-*"))
			if err != nil || len(leftovers) > 0 {
				t.Fatal("backup temporary file not removed")
			}
			return
		default:
			got, err := os.ReadFile(path + migrationBackupSuffix)
			if err == nil && !bytes.Equal(got, content) {
				t.Error("partial backup became visible under its final name")
			}
			if err != nil && !os.IsNotExist(err) {
				t.Error(err)
			}
			runtime.Gosched()
		}
	}
}

func TestConcurrentMigrationBackupCreationDoesNotOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	content := []byte("immutable original")
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := backupMigrationFile(path, content); err != nil {
				t.Error(err)
			}
		}()
	}
	workers.Wait()
	if err := backupMigrationFile(path, []byte("different")); err == nil {
		t.Fatal("different backup replaced existing content")
	}
	got, err := os.ReadFile(path + migrationBackupSuffix)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatal("concurrent creation corrupted backup")
	}
	info, err := os.Stat(path + migrationBackupSuffix)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("backup permission changed")
	}
}
