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
	"testing"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/endpoint"
)

func TestManifestSignaturesAndSixPlatformTargets(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	targets := []struct{ suffix, platform, arch, filetype string }{
		{"windows-amd64.exe", "windows", "amd64", "exe"},
		{"windows-arm64.exe", "windows", "arm64", "exe"},
		{"linux-amd64", "linux", "amd64", "binary"},
		{"linux-arm64", "linux", "arm64", "binary"},
		{"macos-amd64.zip", "darwin", "amd64", "zip"},
		{"macos-arm64.zip", "darwin", "arm64", "zip"},
	}
	const prefix = "CF-Node-Bench-v0.10.0-"
	dir := t.TempDir()
	for _, target := range targets {
		if err := os.WriteFile(filepath.Join(dir, prefix+target.suffix), []byte(target.suffix), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m, err := createManifest(dir, "v0.10.0", "linvva/cf-node-bench", private)
	if err != nil {
		t.Fatal(err)
	}
	if m.SchemaVersion != 1 || m.Version != "0.10.0" || m.Channel != "stable" || len(m.Artifacts) != len(targets) {
		t.Fatalf("unexpected manifest: %+v", m)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(m)
	}))
	defer server.Close()
	provider, err := endpoint.New(endpoint.Config{URL: server.URL + "/update-manifest.json", Channel: "stable", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	for i, target := range targets {
		t.Run(target.suffix, func(t *testing.T) {
			artifact := m.Artifacts[i]
			filename := prefix + target.suffix
			if artifact.Filename != filename || artifact.Filetype != target.filetype || artifact.Platform != target.platform || artifact.Arch != target.arch || artifact.URL != "https://github.com/linvva/cf-node-bench/releases/download/v0.10.0/"+filename {
				t.Fatalf("unexpected artifact: %+v", artifact)
			}
			data, err := os.ReadFile(filepath.Join(dir, filename))
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(data)
			signature, err := base64.StdEncoding.DecodeString(artifact.Signature)
			if err != nil || artifact.Size != int64(len(data)) || artifact.DigestAlgo != "sha256" || artifact.SignatureAlgo != "ed25519" || artifact.Digest != base64.StdEncoding.EncodeToString(digest[:]) || !ed25519.Verify(public, digest[:], signature) {
				t.Fatalf("invalid signed artifact: %+v", artifact)
			}
			release, err := provider.Check(context.Background(), updater.CheckRequest{CurrentVersion: "0.9.0", Platform: target.platform, Arch: target.arch})
			if err != nil || release == nil || release.Artifact.Filename != filename || release.Artifact.Filetype != target.filetype || release.Verification == nil || !ed25519.Verify(public, digest[:], release.Verification.Signature) {
				t.Fatalf("official endpoint did not select the signed artifact: release=%+v, error=%v", release, err)
			}
		})
	}
	for _, platform := range []string{"windows", "linux", "darwin"} {
		t.Run(platform+" rejects unsupported architecture", func(t *testing.T) {
			release, err := provider.Check(context.Background(), updater.CheckRequest{CurrentVersion: "0.9.0", Platform: platform, Arch: "386"})
			if err == nil || release != nil || !strings.Contains(err.Error(), "no artifact") {
				t.Fatalf("unsupported architecture fell back: release=%+v, error=%v", release, err)
			}
		})
	}
	for _, target := range targets {
		t.Run("missing "+target.suffix, func(t *testing.T) {
			filename := filepath.Join(dir, prefix+target.suffix)
			if err := os.Remove(filename); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := os.WriteFile(filename, []byte(target.suffix), 0o644); err != nil {
					t.Error(err)
				}
			}()
			if _, err := createManifest(dir, "v0.10.0", "linvva/cf-node-bench", private); !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), prefix+target.suffix) {
				t.Fatalf("missing artifact must block a release: %v", err)
			}
		})
	}
	if _, err := signingKey(base64.StdEncoding.EncodeToString(private.Seed()), base64.StdEncoding.EncodeToString(make([]byte, 32))); err == nil {
		t.Fatal("mismatched build public key must block a release")
	}
}
