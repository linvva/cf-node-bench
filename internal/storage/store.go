package storage

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
	"github.com/linvva/cf-node-bench/internal/publish"
	runengine "github.com/linvva/cf-node-bench/internal/run"
	"github.com/linvva/cf-node-bench/internal/source"
)

const dataVersion = 1

type data struct {
	Version            int                 `json:"version"`
	Settings           config.Settings     `json:"settings"`
	Publish            publish.Settings    `json:"publish"`
	Sources            []source.HTTPSource `json:"sources"`
	History            []model.RunSummary  `json:"history"`
	MigrationMessage   string              `json:"migrationNotice,omitempty"`
	CurrentResultRunID string              `json:"currentResultRunId,omitempty"`
}

type Store struct {
	mu   sync.RWMutex
	path string
	data data
}

const appDataDirectory = "CF Node Bench"

func Open(path string) (*Store, error) {
	store := &Store{path: path, data: data{Version: dataVersion, Settings: config.DefaultSettings(), Publish: publish.DefaultSettings(), Sources: defaultSources()}}
	content, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	legacy := false
	if err == nil {
		var header struct {
			Version int `json:"version"`
		}
		if err := json.Unmarshal(content, &header); err != nil {
			return nil, err
		}
		if header.Version > dataVersion {
			return nil, errors.New("配置文件版本不兼容")
		}
		legacy = header.Version < dataVersion
		if err := json.Unmarshal(content, &store.data); err != nil {
			return nil, err
		}
		if legacy {
			if err := store.importLegacyProfiles(content); err != nil {
				return nil, err
			}
		}
	}
	store.data.Version = dataVersion
	if legacy {
		store.data.Settings.MigrateLegacy()
	}
	store.data.Settings.Normalize()
	if err := store.data.Settings.Validate(); err != nil {
		return nil, err
	}
	store.data.Publish.Normalize()
	if err := store.data.Publish.Validate(); err != nil {
		return nil, err
	}
	store.normalize()
	if legacy {
		if err := store.migrateFiles(content); err != nil {
			return nil, err
		}
	}
	return store, nil
}

func (s *Store) MigrationNotice() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.MigrationMessage
}

func (s *Store) normalize() {
	if len(s.data.History) > 40 {
		s.data.History = s.data.History[:40]
	}
	for _, settings := range []*config.Settings{&s.data.Settings} {
		if settings.AllowedPorts == nil {
			settings.AllowedPorts = []int{}
		}
		if settings.AllowedCountries == nil {
			settings.AllowedCountries = []string{}
		}
		if settings.BlockedCountries == nil {
			settings.BlockedCountries = []string{}
		}
	}
	if s.data.Sources == nil {
		s.data.Sources = []source.HTTPSource{}
	}
	if s.data.History == nil {
		s.data.History = []model.RunSummary{}
	}
	for index := range s.data.History {
		s.data.History[index].Mode = model.NormalizeMode(s.data.History[index].Mode)
		for resultIndex := range s.data.History[index].Results {
			if s.data.History[index].Results[resultIndex].Mode == "" {
				s.data.History[index].Results[resultIndex].Mode = s.data.History[index].Mode
			}
		}
		if s.data.History[index].Results == nil {
			s.data.History[index].Results = []model.ProbeResult{}
		}
		if s.data.History[index].Failures == nil {
			s.data.History[index].Failures = map[model.FailureReason]int{}
		}
		if s.data.History[index].Publications == nil {
			s.data.History[index].Publications = []model.PublicationResult{}
		}
		if s.data.History[index].ScoreWeights == (model.ScoreWeights{}) {
			if s.data.History[index].Mode == model.ModeH3 {
				s.data.History[index].ScoreWeights = model.ScoreWeights{Bandwidth: 40, HTTP3: 25, QUIC: 15, Reliability: 15, Jitter: 5}
			} else {
				s.data.History[index].ScoreWeights = config.LegacyScoreWeights()
			}
		}
		if s.data.History[index].ReusedStages == nil {
			s.data.History[index].ReusedStages = []string{}
		}
	}
}

func DefaultDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, appDataDirectory), nil
}

func OpenDefault() (*Store, error) {
	dir, err := DefaultDir()
	if err != nil {
		return nil, err
	}
	return Open(filepath.Join(dir, "data.json"))
}

func (s *Store) Settings() config.Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	settings := s.data.Settings
	settings.AllowedPorts = append([]int{}, settings.AllowedPorts...)
	settings.AllowedCountries = append([]string{}, settings.AllowedCountries...)
	settings.BlockedCountries = append([]string{}, settings.BlockedCountries...)
	return settings
}
func (s *Store) Sources() []source.HTTPSource {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]source.HTTPSource{}, s.data.Sources...)
}
func (s *Store) History() []model.RunSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]model.RunSummary{}, s.data.History...)
}

func (s *Store) CurrentResultID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.data.CurrentResultRunID != "" {
		return s.data.CurrentResultRunID
	}
	if len(s.data.History) > 0 {
		return s.data.History[0].RunID
	}
	return ""
}

func (s *Store) LatestCompletedResults() []model.ProbeResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, summary := range s.data.History {
		if summary.State == "completed" {
			return append([]model.ProbeResult{}, summary.Results...)
		}
	}
	return []model.ProbeResult{}
}

func (s *Store) PublishSettings() publish.Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Publish
}

func (s *Store) PublishSettingsView() publish.SettingsView {
	return s.PublishSettings().View()
}

func (s *Store) SaveSettings(settings config.Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	settings.Normalize()
	if err := settings.Validate(); err != nil {
		return err
	}
	previous := s.data.Settings
	s.data.Settings = settings
	if err := s.persistLocked(); err != nil {
		s.data.Settings = previous
		return err
	}
	return nil
}

func (s *Store) SavePublishSettings(request publish.SaveRequest) (publish.SettingsView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := request.Merge(s.data.Publish)
	if err := next.Validate(); err != nil {
		return publish.SettingsView{}, err
	}
	previous := s.data.Publish
	s.data.Publish = next
	if err := s.persistLocked(); err != nil {
		s.data.Publish = previous
		return publish.SettingsView{}, err
	}
	return next.View(), nil
}

func (s *Store) ClearPublishCredential(target string) (publish.SettingsView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := &s.data.Publish
	switch target {
	case "cloudflare":
		current.Cloudflare.APIToken = ""
		current.Cloudflare.Enabled = false
	case "github":
		current.GitHub.Token = ""
		current.GitHub.Enabled = false
	case "gist":
		current.Gist.Token = ""
		current.Gist.Enabled = false
	case "telegram":
		current.Telegram.BotToken = ""
		current.Telegram.Enabled = false
	case "telegramRelay":
		current.Telegram.RelayKey = ""
		if current.Telegram.DeliveryMode == publish.TelegramDeliveryRelay {
			current.Telegram.Enabled = false
		}
	default:
		return publish.SettingsView{}, errors.New("未知发布目标")
	}
	if err := s.persistLocked(); err != nil {
		return publish.SettingsView{}, err
	}
	return current.View(), nil
}

func (s *Store) SaveSources(sources []source.HTTPSource) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Sources = append([]source.HTTPSource(nil), sources...)
	return s.persistLocked()
}

func (s *Store) UpdateSourceStatus(id string, fetched time.Time, status string, count int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.data.Sources {
		if s.data.Sources[index].ID == id {
			s.data.Sources[index].LastFetched = fetched
			s.data.Sources[index].LastStatus = status
			s.data.Sources[index].NodeCount = count
			return s.persistLocked()
		}
	}
	return nil
}

func (s *Store) AddHistory(summary model.RunSummary) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	summary.Mode = model.NormalizeMode(summary.Mode)
	for index := range summary.Results {
		summary.Results[index].Mode = summary.Mode
	}
	s.data.History = append([]model.RunSummary{summary}, s.data.History...)
	s.data.CurrentResultRunID = summary.RunID
	if len(s.data.History) > 40 {
		s.data.History = s.data.History[:40]
	}
	return s.persistLocked()
}

func (s *Store) HistoryByID(runID string) (model.RunSummary, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, summary := range s.data.History {
		if summary.RunID == runID {
			return summary, true
		}
	}
	return model.RunSummary{}, false
}

func (s *Store) UpdatePublication(runID string, result model.PublicationResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for historyIndex := range s.data.History {
		if s.data.History[historyIndex].RunID != runID {
			continue
		}
		// History readers and queued jobs may still hold the previous slice.
		s.data.History[historyIndex].Publications = append([]model.PublicationResult{}, s.data.History[historyIndex].Publications...)
		for publicationIndex := range s.data.History[historyIndex].Publications {
			if s.data.History[historyIndex].Publications[publicationIndex].Target == result.Target {
				s.data.History[historyIndex].Publications[publicationIndex] = result
				return s.persistLocked()
			}
		}
		s.data.History[historyIndex].Publications = append(s.data.History[historyIndex].Publications, result)
		return s.persistLocked()
	}
	return errors.New("未找到测速历史")
}

func (s *Store) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	content, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	temporary := s.path + ".tmp"
	if err := os.WriteFile(temporary, content, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, s.path)
}

func (s *Store) SaveCheckpoint(checkpoint runengine.Checkpoint) error {
	checkpoint.Version = runengine.CheckpointVersion
	checkpoint.UpdatedAt = time.Now()
	path := s.checkpointPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary := path + ".tmp"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	writer := gzip.NewWriter(file)
	encodeErr := json.NewEncoder(writer).Encode(checkpoint)
	closeGzipErr := writer.Close()
	closeFileErr := file.Close()
	if encodeErr != nil {
		return encodeErr
	}
	if closeGzipErr != nil {
		return closeGzipErr
	}
	if closeFileErr != nil {
		return closeFileErr
	}
	return os.Rename(temporary, path)
}

func (s *Store) checkpointPath() string {
	return filepath.Join(filepath.Dir(s.path), "checkpoint.json.gz")
}

func (s *Store) Checkpoint() (runengine.Checkpoint, bool, error) {
	path := s.checkpointPath()
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return runengine.Checkpoint{}, false, nil
	}
	if err != nil {
		return runengine.Checkpoint{}, false, err
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		return runengine.Checkpoint{}, false, err
	}
	defer reader.Close()
	var checkpoint runengine.Checkpoint
	if err := json.NewDecoder(reader).Decode(&checkpoint); err != nil {
		return runengine.Checkpoint{}, false, err
	}
	if checkpoint.Version != runengine.CheckpointVersion {
		return runengine.Checkpoint{}, false, errors.New("检查点版本不兼容")
	}
	return checkpoint, true, nil
}

func defaultSources() []source.HTTPSource {
	return []source.HTTPSource{
		{ID: "example-community-1", Name: "社区示例源 A", URL: "https://raw.githubusercontent.com/ymyuuu/IPDB/main/BestCF/bestcfv4.txt", Enabled: true},
		{ID: "example-community-2", Name: "社区示例源 B", URL: "https://ip.164746.xyz/ipTop10.html", Enabled: false},
	}
}
