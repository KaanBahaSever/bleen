// Package config loads and saves the desktop app's settings (config.toml).
package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pelletier/go-toml/v2"
)

// ErrReset means config.toml could not be read and defaults are used; the
// old file was renamed to config.toml.broken-<time>.
var ErrReset = errors.New("settings file was unreadable and has been reset")

type Config struct {
	Version    int        `toml:"version"`
	Language   string     `toml:"language"` // "tr" | "en" | "system"
	Theme      string     `toml:"theme"`    // "light" | "dark" | "system"
	Backup     Backup     `toml:"backup"`
	Sources    []Source   `toml:"sources"`
	Vault      Vault      `toml:"vault"`  // the disk in use
	Vaults     []Vault    `toml:"vaults"` // every known disk (rotation)
	Automation Automation `toml:"automation"`

	path string
}

type Backup struct {
	ConfirmBeforeRun bool     `toml:"confirm_before_run"`
	MassChangeGuard  float64  `toml:"mass_change_guard"`
	Exclude          []string `toml:"exclude"`
	NewFullEvery     int      `toml:"new_full_every"`   // start a fresh full backup after N backups (0 = never)
	KeepGenerations  int      `toml:"keep_generations"` // keep the newest N full backups with their changes (0 = all)
}

type Source struct {
	ID      string   `toml:"id"`
	Name    string   `toml:"name"`
	Path    string   `toml:"path"`
	Enabled *bool    `toml:"enabled,omitempty"` // missing means on
	Exclude []string `toml:"exclude,omitempty"`
}

// On reports whether "Back up" and scheduled runs include this location.
func (s Source) On() bool { return s.Enabled == nil || *s.Enabled }

type Vault struct {
	ID    string `toml:"id,omitempty"`
	Label string `toml:"label,omitempty"`
	Path  string `toml:"path,omitempty"`
}

// Automation is the opt-in daily backup. When enabled, bleen registers one
// OS scheduled task; no bleen process runs in the background.
type Automation struct {
	Enabled bool   `toml:"enabled"`
	Time    string `toml:"time"` // "18:00"
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
			NewFullEvery:     30,
		},
		Automation: Automation{Time: "18:00"},
	}
}

// Load reads config.toml from dir, or returns defaults if there is none.
func Load(dir string) (*Config, error) {
	c := defaults()
	c.path = filepath.Join(dir, "config.toml")
	b, err := os.ReadFile(c.path)
	if errors.Is(err, os.ErrNotExist) {
		// A broken file was set aside and nothing was saved since (for
		// example a scheduled run found it): still tell the user.
		if old, _ := filepath.Glob(c.path + ".broken-*"); len(old) > 0 {
			return c, ErrReset
		}
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	if err := toml.Unmarshal(b, c); err != nil {
		// Keep the broken file for inspection and start with defaults
		// instead of refusing to start.
		os.Rename(c.path, c.path+".broken-"+time.Now().Format("20060102-150405"))
		d := defaults()
		d.path = c.path
		return d, ErrReset
	}
	for i := range c.Sources {
		if c.Sources[i].ID == "" { // added by hand
			c.Sources[i].ID = uuid.NewString()
		}
	}
	if c.Vault.Path != "" {
		c.RememberVault(c.Vault)
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
	s := Source{ID: uuid.NewString(), Name: name, Path: path}
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

// RememberVault adds or updates a disk in the known list.
func (c *Config) RememberVault(v Vault) {
	for i := range c.Vaults {
		if c.Vaults[i].ID == v.ID {
			c.Vaults[i] = v
			return
		}
	}
	c.Vaults = append(c.Vaults, v)
}

// ForgetVault removes a disk from the known list (its backups are untouched).
func (c *Config) ForgetVault(id string) {
	out := c.Vaults[:0]
	for _, v := range c.Vaults {
		if v.ID != id {
			out = append(out, v)
		}
	}
	c.Vaults = out
	if c.Vault.ID == id {
		c.Vault = Vault{}
		if len(c.Vaults) > 0 {
			c.Vault = c.Vaults[0]
		}
	}
}
