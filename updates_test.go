package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/linvva/cf-node-bench/internal/model"
	"github.com/wailsapp/wails/v3/pkg/updater"
)

type updateTestHost struct{ progress func(updater.Progress) }

func (h updateTestHost) Emit(name string, data ...any) bool {
	if name == updater.EventDownloadProgress && h.progress != nil {
		h.progress(data[0].(updater.Progress))
	}
	return false
}
func (updateTestHost) OnEvent(string, func(any)) func() { return func() {} }
func (updateTestHost) OpenWindow(updater.WindowOptions) updater.WindowHandle {
	panic("headless updates must not open a window")
}
func (updateTestHost) Quit() { panic("test must not exit the application") }

func TestUpdateManifestRequiresTrustedArtifactSignature(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("signed update fixture")
	digest := sha256.Sum256(payload)
	for _, test := range []struct{ name, failure string }{
		{"signed", ""}, {"signed exe", ""}, {"missing signature", "signature"},
		{"invalid signature", "signature"}, {"tampered payload", "digest"},
	} {
		t.Run(test.name, func(t *testing.T) {
			filename, filetype := "CF-Node-Bench-v1.1.0-linux-amd64", "binary"
			if test.name == "signed exe" {
				filename, filetype = "CF-Node-Bench-v1.1.0-windows-arm64.exe", "exe"
			}
			signature := ed25519.Sign(private, digest[:])
			if test.name == "invalid signature" {
				signature[0] ^= 1
			}
			artifact := map[string]any{
				"url": "/artifact", "filename": filename, "filetype": filetype, "size": len(payload),
				"digestAlgo": "sha256", "digest": base64.StdEncoding.EncodeToString(digest[:]),
				"signatureAlgo": "ed25519", "signature": base64.StdEncoding.EncodeToString(signature),
			}
			if test.name == "missing signature" {
				delete(artifact, "signature")
				delete(artifact, "signatureAlgo")
			}
			var downloads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/artifact" {
					downloads.Add(1)
					if test.name == "tampered payload" {
						_, _ = w.Write([]byte("modified artifact"))
					} else {
						_, _ = w.Write(payload)
					}
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"schemaVersion": 1, "version": "v1.1.0", "channel": "stable", "notes": "new version",
					"artifacts": []any{artifact},
				})
			}))
			defer server.Close()
			var controller *updateController
			engine := updater.New(updateTestHost{progress: func(p updater.Progress) { controller.progress(p) }})
			defer func() {
				if staged := engine.DownloadedPath(); staged != "" {
					_ = os.RemoveAll(filepath.Dir(staged))
				}
			}()
			controller = newUpdateController("v1.0.0", base64.StdEncoding.EncodeToString(public), server.URL+"/update-manifest.json", server.Client(), engine, nil)
			app := &App{updates: controller}
			state, err := app.CheckForUpdate()
			if err != nil || state.Status != "available" || state.Version != "1.1.0" || state.Notes != "new version" {
				t.Fatalf("check: state=%+v, error=%v", state, err)
			}
			if downloads.Load() != 0 {
				t.Fatal("checking downloaded an artifact without user action")
			}
			state, err = app.DownloadUpdate()
			if test.failure != "" {
				if err == nil || state.Status != "error" || !strings.Contains(state.Error, test.failure) || engine.DownloadedPath() != "" {
					t.Fatalf("untrusted artifact accepted: state=%+v, error=%v", state, err)
				}
				return
			}
			if err != nil || state.Status != "ready" || state.Downloaded != int64(len(payload)) {
				t.Fatalf("download: state=%+v, error=%v", state, err)
			}
			staged, err := os.ReadFile(engine.DownloadedPath())
			if err != nil || string(staged) != string(payload) || filepath.Base(engine.DownloadedPath()) != filename {
				t.Fatalf("staged payload=%q, error=%v", staged, err)
			}
			controller.progress(updater.Progress{Written: 1, Total: 2})
			if app.UpdateStatus() != state {
				t.Fatal("late download event overwrote ready state")
			}
		})
	}
}

func TestUpdateUnavailableBuildsDoNotConfigureUnsignedUpdates(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, ed25519.PublicKeySize))
	for _, test := range []struct{ version, key string }{
		{"dev", key}, {"v1.0.0", ""}, {"v1.0.0", "invalid"},
	} {
		controller := newUpdateController(test.version, test.key, updateManifestURL, nil, nil, nil)
		app := &App{updates: controller}
		state, err := app.DownloadUpdate()
		if state.Status != "unavailable" || state.Error == "" || err == nil || controller.backend != nil {
			t.Fatalf("unsafe configuration: %+v, error=%v", state, err)
		}
	}
}

func TestUpdateCheckFailureKeepsApplicationUsable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	key := base64.StdEncoding.EncodeToString(make([]byte, ed25519.PublicKeySize))
	controller := newUpdateController("v1.0.0", key, server.URL, server.Client(), updater.New(updateTestHost{}), nil)
	app := &App{updates: controller}
	state, err := app.CheckForUpdate()
	if err == nil || state.Status != "error" || !strings.Contains(state.Error, "503") || app.restarting {
		t.Fatalf("failed check: state=%+v, error=%v", state, err)
	}
	if _, err := app.DownloadUpdate(); err == nil {
		t.Fatal("failed check left a downloadable release")
	}
}

type updateTestBackend struct {
	restarts int
	check    func() (*updater.Release, error)
	restart  func() error
}

func (b *updateTestBackend) Check(context.Context) (*updater.Release, error) {
	if b.check != nil {
		return b.check()
	}
	return nil, nil
}
func (*updateTestBackend) DownloadAndInstall(context.Context) error { return nil }
func (b *updateTestBackend) Restart(context.Context) error {
	b.restarts++
	if b.restart != nil {
		return b.restart()
	}
	return nil
}

func TestUpdateSerializesChecksAndDownloads(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	backend := &updateTestBackend{check: func() (*updater.Release, error) {
		close(started)
		<-release
		return &updater.Release{Version: "1.1.0"}, nil
	}}
	app := &App{updates: &updateController{backend: backend, state: UpdateState{Status: "idle"}}}
	done := make(chan error, 1)
	go func() { _, err := app.CheckForUpdate(); done <- err }()
	<-started
	defer func() {
		close(release)
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	if state, err := app.CheckForUpdate(); err == nil || state.Status != "checking" {
		t.Fatalf("concurrent check changed active state: %+v, %v", state, err)
	}
	if state, err := app.DownloadUpdate(); err == nil || state.Status != "checking" {
		t.Fatalf("download ran during a check: %+v, %v", state, err)
	}
}

func TestUpdateRestartWaitsForRunCompletionAndReleasesGateOnFailure(t *testing.T) {
	backend := &updateTestBackend{}
	app := &App{updates: &updateController{backend: backend, state: UpdateState{Status: "ready"}}, runPending: true}
	if err := app.RestartForUpdate(); err == nil || backend.restarts != 0 || app.restarting {
		t.Fatal("restart was allowed while a completed run was still being saved or published")
	}
	app.runPending = false
	backend.restart = func() error {
		app.mu.Lock()
		defer app.mu.Unlock()
		if !app.restarting {
			t.Fatal("restart did not block new work before calling the helper")
		}
		return errors.New("helper failed to start")
	}
	if err := app.RestartForUpdate(); err == nil || app.restarting || app.UpdateStatus().Status != "ready" {
		t.Fatal("helper failure did not release work gate or allow retry")
	}
	backend.restart = nil
	if err := app.RestartForUpdate(); err != nil || !app.restarting || backend.restarts != 2 {
		t.Fatalf("restart retry: %v", err)
	}
}

func TestUpdateRestartWaitsForActivePublication(t *testing.T) {
	app := testApp(t)
	configureH3Gist(t, app)
	started, release := make(chan struct{}, 1), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "normal-gist", "files": map[string]any{}})
	}))
	defer server.Close()
	app.publish.GitHubBaseURL = server.URL
	backend := &updateTestBackend{}
	app.updates = &updateController{backend: backend, state: UpdateState{Status: "ready"}}
	summary := model.RunSummary{RunID: "busy-publication", State: "completed", Results: []model.ProbeResult{{Candidate: model.Candidate{IP: "192.0.2.1", Port: 443}}}}
	if err := app.store.AddHistory(summary); err != nil {
		t.Fatal(err)
	}
	if err := app.PublishRun(summary.RunID, "gist"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		close(release)
		deadline := time.Now().Add(2 * time.Second)
		for app.queue.Busy() && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("publication did not start")
	}
	if err := app.RestartForUpdate(); err == nil || backend.restarts != 0 || app.restarting {
		t.Fatal("restart was allowed while publication was active")
	}
}
