package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// overlaySettings are set in the app window and pushed live to OBS.
type overlaySettings struct {
	HidePaused bool  `json:"hidePaused"`
	Speed      int   `json:"speed"`  // marquee px/s
	CoverV     int64 `json:"coverV"` // changes when the picture file changes
}

type appConfig struct {
	Twitch  twitchConfig    `json:"twitch"`
	Overlay overlaySettings `json:"overlay"`
}

// configStore keeps %APPDATA%\NowPlaying\config.json.
type configStore struct {
	mu   sync.Mutex
	path string
	cfg  appConfig
}

func loadConfig() *configStore {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	s := &configStore{path: filepath.Join(dir, "NowPlaying", "config.json")}
	if data, err := os.ReadFile(s.path); err == nil {
		json.Unmarshal(data, &s.cfg)
	}
	s.cfg.Twitch.defaults()
	if s.cfg.Overlay.Speed <= 0 {
		s.cfg.Overlay.Speed = 45
	}
	return s
}

func (s *configStore) get() appConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

func (s *configStore) update(f func(c *appConfig)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(&s.cfg)
	os.MkdirAll(filepath.Dir(s.path), 0o700)
	data, _ := json.MarshalIndent(s.cfg, "", "  ")
	return os.WriteFile(s.path, data, 0o600)
}
