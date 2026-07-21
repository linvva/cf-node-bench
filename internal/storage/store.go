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
	"github.com/linvva/cf-node-bench/internal/source"
)

type data struct {
	Settings config.Settings     `json:"settings"`
	Sources  []source.HTTPSource `json:"sources"`
	History  []model.RunSummary  `json:"history"`
}

type Store struct {
	mu   sync.RWMutex
	path string
	data data
}

func Open(path string) (*Store, error) {
	store := &Store{path: path, data: data{Settings: config.DefaultSettings(), Sources: defaultSources()}}
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
	}
}

func OpenDefault() (*Store, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return Open(filepath.Join(dir, "CF Node Bench", "data.json"))
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

func (s *Store) SaveSettings(settings config.Settings) error {
	if err := settings.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Settings = settings
	return s.persistLocked()
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
