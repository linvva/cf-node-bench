package storage

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/linvva/cf-node-bench/internal/model"
	"github.com/linvva/cf-node-bench/internal/publish"
	runengine "github.com/linvva/cf-node-bench/internal/run"
)

const migrationBackupSuffix = ".pre-unified.bak"

func (s *Store) importLegacyProfiles(content []byte) error {
	var old struct {
		ActiveMode model.Mode      `json:"activeMode"`
		H3Settings json.RawMessage `json:"h3Settings"`
		H3Publish  json.RawMessage `json:"h3Publish"`
	}
	if err := json.Unmarshal(content, &old); err != nil {
		return err
	}
	s.data.Settings.RequireH3 = old.ActiveMode == model.ModeH3
	if len(old.H3Settings) > 0 && string(old.H3Settings) != "null" {
		var h3 struct {
			WorkerHost       *string `json:"workerHost"`
			ConnectTimeoutMS *int    `json:"connectTimeoutMs"`
			RequestTimeoutMS *int    `json:"requestTimeoutMs"`
			BandwidthTestURL *string `json:"bandwidthTestUrl"`
		}
		if err := json.Unmarshal(old.H3Settings, &h3); err != nil {
			return err
		}
		if h3.WorkerHost != nil {
			s.data.Settings.VerificationHost = *h3.WorkerHost
		}
		if h3.ConnectTimeoutMS != nil {
			s.data.Settings.H3ConnectTimeoutMS = *h3.ConnectTimeoutMS
		}
		if h3.RequestTimeoutMS != nil {
			s.data.Settings.H3RequestTimeoutMS = *h3.RequestTimeoutMS
		}
		if h3.BandwidthTestURL != nil {
			s.data.Settings.H3BandwidthTestURL = *h3.BandwidthTestURL
		}
	}
	s.data.Publish.Normalize()
	if len(old.H3Publish) > 0 && string(old.H3Publish) != "null" {
		h3 := publish.DefaultSettings()
		h3.Cloudflare.RecordType = "TXT"
		h3.GitHub.Path, h3.Gist.Filename = "h3.txt", "h3.txt"
		if err := json.Unmarshal(old.H3Publish, &h3); err != nil {
			return err
		}
		h3.Normalize()
		s.data.Publish, s.data.MigrationMessage = mergeLegacyPublish(s.data.Publish, h3)
	}
	return nil
}

func mergeLegacyPublish(tcp, h3 publish.Settings) (publish.Settings, string) {
	conflicts := []string{}
	choose := func(name string, left, right, different bool) bool {
		if left && right && different {
			conflicts = append(conflicts, name)
		}
		return !left && right
	}
	cf := func(v publish.CloudflareSettings) bool {
		return v.Enabled || v.APIToken != "" || v.ZoneID != "" || v.RecordName != ""
	}
	gh := func(v publish.GitHubSettings) bool {
		return v.Enabled || v.Token != "" || v.Owner != "" || v.Repository != ""
	}
	gist := func(v publish.GistSettings) bool { return v.Enabled || v.Token != "" || v.GistID != "" }
	tg := func(v publish.TelegramSettings) bool {
		return v.Enabled || v.BotToken != "" || v.ChatID != "" || v.RelayURL != "" || v.RelayKey != ""
	}
	if choose("Cloudflare", cf(tcp.Cloudflare), cf(h3.Cloudflare), tcp.Cloudflare != h3.Cloudflare) {
		tcp.Cloudflare = h3.Cloudflare
	}
	if choose("GitHub", gh(tcp.GitHub), gh(h3.GitHub), tcp.GitHub != h3.GitHub) {
		tcp.GitHub = h3.GitHub
	}
	if choose("Gist", gist(tcp.Gist), gist(h3.Gist), tcp.Gist != h3.Gist) {
		tcp.Gist = h3.Gist
	}
	if choose("Telegram", tg(tcp.Telegram), tg(h3.Telegram), tcp.Telegram != h3.Telegram) {
		tcp.Telegram = h3.Telegram
	}
	notice := ""
	if len(conflicts) > 0 {
		notice = "已合并旧配置；" + strings.Join(conflicts, "、") + " 的两套发布配置不同，已保留 TCP/HTTPS 配置，原 H3 配置仍在 .pre-unified.bak 备份中。"
	}
	return tcp, notice
}

// Backups are immutable. When a previous attempt stopped before committing data,
// they also supply the original checkpoints for a deterministic retry.
func migrationOriginal(path string) ([]byte, error) {
	content, err := os.ReadFile(path + migrationBackupSuffix)
	if err == nil {
		return content, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	content, err = os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return content, err
}

func backupMigrationFile(path string, content []byte) error {
	if content == nil {
		return nil
	}
	backup := path + migrationBackupSuffix
	existing, err := os.ReadFile(backup)
	if err == nil {
		if !bytes.Equal(existing, content) {
			return fmt.Errorf("迁移备份已存在且内容不同：%s", filepath.Base(backup))
		}
		return os.Chmod(backup, 0o600)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	staged, err := stageMigrationFile(backup, content)
	if err != nil {
		return err
	}
	defer os.Remove(staged)
	// Linking publishes the fully written file atomically without replacing an
	// existing backup, including one created concurrently after the initial read.
	if err := os.Link(staged, backup); err != nil {
		if errors.Is(err, os.ErrExist) {
			existing, readErr := os.ReadFile(backup)
			if readErr != nil {
				return readErr
			}
			if !bytes.Equal(existing, content) {
				return fmt.Errorf("迁移备份已存在且内容不同：%s", filepath.Base(backup))
			}
			return os.Chmod(backup, 0o600)
		}
		return err
	}
	return nil
}

func (s *Store) migrateFiles(originalData []byte) error {
	var old struct {
		ActiveMode model.Mode `json:"activeMode"`
	}
	if err := json.Unmarshal(originalData, &old); err != nil {
		return err
	}
	tcpPath := s.checkpointPath()
	h3Path := filepath.Join(filepath.Dir(s.path), "checkpoint-h3.json.gz")
	tcp, err := migrationOriginal(tcpPath)
	if err != nil {
		return err
	}
	h3, err := migrationOriginal(h3Path)
	if err != nil {
		return err
	}
	selected := tcp
	if old.ActiveMode == model.ModeH3 {
		selected = h3
	}
	var nextCheckpoint []byte
	if selected != nil {
		reader, err := gzip.NewReader(bytes.NewReader(selected))
		if err != nil {
			return err
		}
		var checkpoint runengine.Checkpoint
		decodeErr := json.NewDecoder(reader).Decode(&checkpoint)
		closeErr := reader.Close()
		if err := errors.Join(decodeErr, closeErr); err != nil {
			return err
		}
		if checkpoint.Version != runengine.CheckpointVersion {
			return errors.New("检查点版本不兼容，未迁移配置")
		}
		checkpoint.Settings.MigrateLegacy()
		checkpoint.Settings.Normalize()
		for stage, settings := range checkpoint.StageSettings {
			settings.MigrateLegacy()
			settings.Normalize()
			checkpoint.StageSettings[stage] = settings
		}
		var output bytes.Buffer
		writer := gzip.NewWriter(&output)
		if err := json.NewEncoder(writer).Encode(checkpoint); err != nil {
			return err
		}
		if err := writer.Close(); err != nil {
			return err
		}
		nextCheckpoint = output.Bytes()
		for _, summary := range s.data.History {
			if summary.RunID == checkpoint.RunID {
				s.data.CurrentResultRunID = summary.RunID
				break
			}
		}
	}
	nextData, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	dataBackup, backupErr := os.ReadFile(s.path + migrationBackupSuffix)
	recovering := backupErr == nil && bytes.Equal(dataBackup, originalData)
	for _, file := range []migrationFile{{path: tcpPath, before: tcp, after: nextCheckpoint}, {path: h3Path, before: h3}} {
		current, err := os.ReadFile(file.path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if !bytes.Equal(current, file.before) && !(recovering && bytes.Equal(current, file.after)) {
			return fmt.Errorf("检查点与迁移备份不同，未覆盖原文件：%s", filepath.Base(file.path))
		}
	}
	for _, file := range []migrationFile{{path: s.path, after: originalData}, {path: tcpPath, after: tcp}, {path: h3Path, after: h3}} {
		if err := backupMigrationFile(file.path, file.after); err != nil {
			return err
		}
	}
	// data.json is the commit marker and must be replaced last.
	return commitMigration([]migrationFile{{path: tcpPath, after: nextCheckpoint}, {path: h3Path}, {path: s.path, after: nextData}})
}

type migrationFile struct {
	path          string
	before, after []byte
	staged        string
}

func stageMigrationFile(path string, content []byte) (string, error) {
	file, err := os.CreateTemp(filepath.Dir(path), ".unified-migration-*")
	if err != nil {
		return "", err
	}
	name := file.Name()
	_, writeErr := file.Write(content)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

func commitMigration(files []migrationFile) error {
	defer func() {
		for _, file := range files {
			if file.staged != "" {
				_ = os.Remove(file.staged)
			}
		}
	}()
	for i := range files {
		before, err := os.ReadFile(files[i].path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		files[i].before = before
		if files[i].after != nil {
			staged, err := stageMigrationFile(files[i].path, files[i].after)
			if err != nil {
				return err
			}
			files[i].staged = staged
		}
	}
	for i, file := range files {
		var err error
		if file.after == nil {
			err = os.Remove(file.path)
			if errors.Is(err, os.ErrNotExist) {
				err = nil
			}
		} else {
			err = os.Rename(file.staged, file.path)
		}
		if err != nil {
			for j := i - 1; j >= 0; j-- {
				previous := files[j]
				if previous.before == nil {
					restoreErr := os.Remove(previous.path)
					if !errors.Is(restoreErr, os.ErrNotExist) {
						err = errors.Join(err, restoreErr)
					}
				} else {
					staged, restoreErr := stageMigrationFile(previous.path, previous.before)
					if restoreErr == nil {
						restoreErr = os.Rename(staged, previous.path)
						_ = os.Remove(staged)
					}
					err = errors.Join(err, restoreErr)
				}
			}
			return err
		}
	}
	return nil
}
