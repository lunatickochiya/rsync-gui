package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// AppConfig is remembered between runs.
type AppConfig struct {
	Version            int      `json:"version"`
	Options            Options  `json:"options"`
	Sources            []string `json:"sources,omitempty"`
	Destination        string   `json:"destination,omitempty"`
	RecentDestinations []string `json:"recentDestinations,omitempty"`
}

// ConfigPath returns the per-user configuration file.
func ConfigPath() string {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "rsync-tui", "config.json")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "rsync-tui", "config.json")
	}
	return "rsync-tui.json"
}

// LoadConfig loads remembered settings, falling back to defaults.
func LoadConfig() AppConfig {
	cfg := AppConfig{Version: 1, Options: DefaultOptions()}
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		return cfg
	}
	var raw struct {
		Version            int             `json:"version"`
		Options            json.RawMessage `json:"options"`
		Sources            []string        `json:"sources"`
		Destination        string          `json:"destination"`
		RecentDestinations []string        `json:"recentDestinations"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return cfg
	}
	opts := DefaultOptions()
	if len(raw.Options) > 0 {
		if err := json.Unmarshal(raw.Options, &opts); err != nil {
			opts = DefaultOptions()
		}
	}
	if opts.Progress == "" {
		opts.Progress = "per-file"
	}
	if opts.Existing == "" {
		opts.Existing = "replace"
	}
	// Destructive flags are session-only: never start a new session in move or
	// delete mode just because the previous run used it. Presets and --config
	// can still enable them explicitly.
	opts.DeleteExtraneous = false
	opts.RemoveSourceFiles = false
	opts.RemoveSourceDirs = false
	opts.DryRun = false
	cfg.Options = opts
	cfg.Sources = raw.Sources
	cfg.Destination = raw.Destination
	cfg.RecentDestinations = raw.RecentDestinations
	return cfg
}

// Save persists the configuration atomically.
func (c *AppConfig) Save() error {
	c.Version = 1
	data, err := json.MarshalIndent(c, "", "    ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// RememberDestination moves dest to the front of the recent list.
func (c *AppConfig) RememberDestination(dest string) {
	if dest == "" {
		return
	}
	out := []string{dest}
	for _, d := range c.RecentDestinations {
		if d != dest && len(out) < 10 {
			out = append(out, d)
		}
	}
	c.RecentDestinations = out
	c.Destination = dest
}
