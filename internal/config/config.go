// Package config loads and saves the desktop app's settings (config.toml).
package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Version    int        `toml:"version"`
	Language   string     `toml:"language"` // "tr" | "en" | "system"
	Theme      string     `toml:"theme"`    // "light" | "dark" | "system"
	Backup     Backup     `toml:"backup"`
	Sources    []Source   `toml:"sources"`
	Vault      Vault      `toml:"vault"`
	Automation Automation `toml:"automation"`

	path string
}

type Backup struct {
	ConfirmBeforeRun bool     `toml:"confirm_before_run"`
	MassChangeGuard  float64  `toml:"mass_change_guard"`
	Exclude          []string `toml:"exclude"`
}

type Source struct {
	ID      string   `toml:"id"`
	Name    string   `toml:"name"`
	Path    string   `toml:"path"`
	Enabled bool     `toml:"enabled"`
	Exclude []string `toml:"exclude,omitempty"`
}

type Vault struct {
	ID    string `toml:"id,omitempty"`
	Label string `toml:"label,omitempty"`
	Path  string `toml:"path,omitempty"`
}

// Automation is reserved for the future opt-in scheduler; nothing reads it yet.
type Automation struct {
	Enabled bool `toml:"enabled"`
}

// Dir is where settings live. A file named "bleen.portable" next to the
// executable switches to portable mode: settings stay beside the program.
func Dir() (string, error) {
	if exe, err := os.Executable(); err == nil {
		d := filepath.Dir(exe)
		if _, err := os.Stat(filepath.Join(d, "bleen.portable")); err == nil {
			return filepath.Join(d, "bleen-data"), nil
		}
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "bleen"), nil
}

func defaults() *Config {
	return &Config{
		Version:  1,
		Language: "system",
		Theme:    "system",
		Backup: Backup{
			ConfirmBeforeRun: true,
			MassChangeGuard:  0.30,
		},
	}
}

// Load reads config.toml from dir, or returns defaults if there is none.
func Load(dir string) (*Config, error) {
	c := defaults()
	c.path = filepath.Join(dir, "config.toml")
	b, err := os.ReadFile(c.path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	if err := toml.Unmarshal(b, c); err != nil {
		return nil, err
	}
	return c, nil
}

// Save writes config.toml atomically.
func (c *Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	b, err := toml.Marshal(c)
	if err != nil {
		return err
	}
	b = append([]byte("# bleen settings. Written by the app; safe to edit by hand while bleen is closed.\n"), b...)
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

// Dir returns the folder holding this config file.
func (c *Config) Dir() string { return filepath.Dir(c.path) }

// AddSource adds a folder; it returns the existing entry if already present.
func (c *Config) AddSource(path, name string) Source {
	path = filepath.Clean(strings.TrimSpace(path))
	for _, s := range c.Sources {
		if strings.EqualFold(s.Path, path) {
			return s
		}
	}
	if name == "" {
		name = filepath.Base(path)
	}
	s := Source{ID: uuid.NewString(), Name: name, Path: path, Enabled: true}
	c.Sources = append(c.Sources, s)
	return s
}

func (c *Config) RemoveSource(id string) {
	out := c.Sources[:0]
	for _, s := range c.Sources {
		if s.ID != id {
			out = append(out, s)
		}
	}
	c.Sources = out
}

func (c *Config) Source(id string) (Source, bool) {
	for _, s := range c.Sources {
		if s.ID == id {
			return s, true
		}
	}
	return Source{}, false
}
