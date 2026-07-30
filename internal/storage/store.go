package storage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
	"github.com/linvva/cf-node-bench/internal/publish"
	"github.com/linvva/cf-node-bench/internal/source"
)

type data struct {
	Settings config.Settings     `json:"settings"`
	Publish  publish.Settings    `json:"publish"`
	Sources  []source.HTTPSource `json:"sources"`
	History  []model.RunSummary  `json:"history"`
}

type Store struct {
	mu   sync.RWMutex
	path string
	data data
}

const appDataDirectory = "CF Node Bench"

func Open(path string) (*Store, error) {
	store := &Store{path: path, data: data{Settings: config.DefaultSettings(), Publish: publish.DefaultSettings(), Sources: defaultSources()}}
	content, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if err := json.Unmarshal(content, &store.data); err != nil {
			return nil, err
		}
	}
	store.data.Settings.MigrateLegacy()
	if err := store.data.Settings.Validate(); err != nil {
		store.data.Settings = config.DefaultSettings()
	}
	store.data.Publish.Normalize()
	if err := store.data.Publish.Validate(); err != nil {
		store.data.Publish = publish.DefaultSettings()
	}
	store.normalize()
	return store, nil
}

func (s *Store) normalize() {
	if s.data.Settings.AllowedPorts == nil {
		s.data.Settings.AllowedPorts = []int{}
	}
	if s.data.Settings.AllowedCountries == nil {
		s.data.Settings.AllowedCountries = []string{}
	}
	if s.data.Settings.BlockedCountries == nil {
		s.data.Settings.BlockedCountries = []string{}
	}
	if s.data.Sources == nil {
		s.data.Sources = []source.HTTPSource{}
	}
	if s.data.History == nil {
		s.data.History = []model.RunSummary{}
	}
	for index := range s.data.History {
		if s.data.History[index].Results == nil {
			s.data.History[index].Results = []model.ProbeResult{}
		}
		if s.data.History[index].Failures == nil {
			s.data.History[index].Failures = map[model.FailureReason]int{}
		}
		if s.data.History[index].Publications == nil {
			s.data.History[index].Publications = []model.PublicationResult{}
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
	return s.data.Settings
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

func (s *Store) PublishSettings() publish.Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Publish
}

func (s *Store) PublishSettingsView() publish.SettingsView {
	return s.PublishSettings().View()
}

func (s *Store) SaveSettings(settings config.Settings) error {
	if err := settings.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Settings = settings
	return s.persistLocked()
}

func (s *Store) SavePublishSettings(request publish.SaveRequest) (publish.SettingsView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := request.Merge(s.data.Publish)
	if err := next.Validate(); err != nil {
		return publish.SettingsView{}, err
	}
	s.data.Publish = next
	if err := s.persistLocked(); err != nil {
		return publish.SettingsView{}, err
	}
	return next.View(), nil
}

func (s *Store) ClearPublishCredential(target string) (publish.SettingsView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch target {
	case "cloudflare":
		s.data.Publish.Cloudflare.APIToken = ""
		s.data.Publish.Cloudflare.Enabled = false
	case "github":
		s.data.Publish.GitHub.Token = ""
		s.data.Publish.GitHub.Enabled = false
	case "gist":
		s.data.Publish.Gist.Token = ""
		s.data.Publish.Gist.Enabled = false
	case "telegram":
		s.data.Publish.Telegram.BotToken = ""
		s.data.Publish.Telegram.Enabled = false
	case "telegramRelay":
		s.data.Publish.Telegram.RelayKey = ""
		if s.data.Publish.Telegram.DeliveryMode == publish.TelegramDeliveryRelay {
			s.data.Publish.Telegram.Enabled = false
		}
	default:
		return publish.SettingsView{}, errors.New("未知发布目标")
	}
	if err := s.persistLocked(); err != nil {
		return publish.SettingsView{}, err
	}
	return s.data.Publish.View(), nil
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
	s.data.History = append([]model.RunSummary{summary}, s.data.History...)
	if len(s.data.History) > 20 {
		s.data.History = s.data.History[:20]
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

func defaultSources() []source.HTTPSource {
	return []source.HTTPSource{
		{ID: "example-community-1", Name: "社区示例源 A", URL: "https://raw.githubusercontent.com/ymyuuu/IPDB/main/BestCF/bestcfv4.txt", Enabled: true},
		{ID: "example-community-2", Name: "社区示例源 B", URL: "https://ip.164746.xyz/ipTop10.html", Enabled: false},
	}
}
