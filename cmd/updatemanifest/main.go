// updatemanifest signs the six platform artifacts for a GitHub Release.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type artifact struct {
	URL           string `json:"url"`
	Filename      string `json:"filename"`
	Filetype      string `json:"filetype"`
	Size          int64  `json:"size"`
	Platform      string `json:"platform"`
	Arch          string `json:"arch"`
	DigestAlgo    string `json:"digestAlgo"`
	Digest        string `json:"digest"`
	SignatureAlgo string `json:"signatureAlgo"`
	Signature     string `json:"signature"`
}

type manifest struct {
	SchemaVersion int        `json:"schemaVersion"`
	Version       string     `json:"version"`
	Channel       string     `json:"channel"`
	Name          string     `json:"name"`
	Notes         string     `json:"notes"`
	Artifacts     []artifact `json:"artifacts"`
}

func signingKey(seedText, publicText string) (ed25519.PrivateKey, error) {
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(seedText))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("UPDATE_SIGNING_KEY must contain a base64 Ed25519 seed (32 bytes)")
	}
	key := ed25519.NewKeyFromSeed(seed)
	public, err := base64.StdEncoding.DecodeString(strings.TrimSpace(publicText))
	if err != nil || !bytes.Equal(public, key.Public().(ed25519.PublicKey)) {
		return nil, errors.New("UPDATE_PUBLIC_KEY does not match UPDATE_SIGNING_KEY")
	}
	return key, nil
}

func createManifest(dir, tag, repository string, key ed25519.PrivateKey) (manifest, error) {
	if !regexp.MustCompile(`^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$`).MatchString(tag) {
		return manifest{}, errors.New("version must be a v-prefixed release tag")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(repository) {
		return manifest{}, errors.New("repository must be owner/name")
	}
	m := manifest{SchemaVersion: 1, Version: strings.TrimPrefix(tag, "v"), Channel: "stable", Name: "CF Node Bench " + tag,
		Notes: "更新说明：https://github.com/" + repository + "/releases/tag/" + tag}
	for _, target := range []struct{ suffix, platform, arch, filetype string }{
		{"windows-amd64.exe", "windows", "amd64", "exe"},
		{"windows-arm64.exe", "windows", "arm64", "exe"},
		{"linux-amd64", "linux", "amd64", "binary"},
		{"linux-arm64", "linux", "arm64", "binary"},
		{"macos-amd64.zip", "darwin", "amd64", "zip"},
		{"macos-arm64.zip", "darwin", "arm64", "zip"},
	} {
		filename := "CF-Node-Bench-" + tag + "-" + target.suffix
		data, err := os.ReadFile(filepath.Join(dir, filename))
		if err != nil {
			return manifest{}, err
		}
		digest := sha256.Sum256(data)
		m.Artifacts = append(m.Artifacts, artifact{
			URL:      "https://github.com/" + repository + "/releases/download/" + tag + "/" + filename,
			Filename: filename, Filetype: target.filetype, Size: int64(len(data)), Platform: target.platform, Arch: target.arch,
			DigestAlgo: "sha256", Digest: base64.StdEncoding.EncodeToString(digest[:]),
			SignatureAlgo: "ed25519", Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, digest[:])),
		})
	}
	return m, nil
}

func generateKeys(dir string) error {
	if err := os.Mkdir(dir, 0o700); err != nil {
		return err
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	for name, data := range map[string][]byte{"private.key": private.Seed(), "public.key": public} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(base64.StdEncoding.EncodeToString(data)+"\n"), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func run() error {
	dir := flag.String("dir", "release-artifacts", "directory containing all six platform artifacts")
	tag := flag.String("version", "", "GitHub Release tag")
	repository := flag.String("repository", "linvva/cf-node-bench", "GitHub owner/name")
	keygen := flag.String("keygen", "", "create private.key/public.key in a new directory, then exit")
	flag.Parse()
	if *keygen != "" {
		return generateKeys(*keygen)
	}
	key, err := signingKey(os.Getenv("UPDATE_SIGNING_KEY"), os.Getenv("UPDATE_PUBLIC_KEY"))
	if err != nil {
		return err
	}
	m, err := createManifest(*dir, *tag, *repository, key)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(*dir, "update-manifest.json"), append(data, '\n'), 0o644)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
