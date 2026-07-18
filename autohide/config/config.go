package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalText(text []byte) error {
	var err error
	d.Duration, err = time.ParseDuration(string(text))
	return err
}

func (d Duration) MarshalText() ([]byte, error) {
	return []byte(d.Duration.String()), nil
}

// FormatDuration renders short user-facing durations ("30s", "1m", "2m30s")
// the way the menu bar and status surfaces display them; hours fall back to
// Go's verbose form.
func FormatDuration(d time.Duration) string {
	if d > 0 && d < time.Second {
		return d.String()
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		if s == 0 {
			return fmt.Sprintf("%dm", m)
		}
		return fmt.Sprintf("%dm%ds", m, s)
	}
	return d.String()
}

type GeneralConfig struct {
	DefaultTimeout  Duration `toml:"default_timeout"`
	CheckInterval   Duration `toml:"check_interval"`
	SystemExclude   []string `toml:"system_exclude"`
	WindowTracking  bool     `toml:"window_tracking"`
	HideOtherSpaces bool     `toml:"hide_other_spaces"`
}

type AppConfig struct {
	Timeout  Duration `toml:"timeout,omitempty"`
	Disabled bool     `toml:"disabled,omitempty"`
}

type MenubarConfig struct {
	TimeoutPresets []Duration `toml:"timeout_presets"`
}

type FocusConfig struct {
	KeepRecent int      `toml:"keep_recent"`
	Grace      Duration `toml:"grace"`
}

type Config struct {
	General GeneralConfig        `toml:"general"`
	Apps    map[string]AppConfig `toml:"apps"`
	Menubar MenubarConfig        `toml:"menubar"`
	Focus   FocusConfig          `toml:"focus"`
}

func Default() *Config {
	return &Config{
		General: GeneralConfig{
			DefaultTimeout:  Duration{1 * time.Minute},
			CheckInterval:   Duration{5 * time.Second},
			SystemExclude:   []string{"Finder"},
			WindowTracking:  true,
			HideOtherSpaces: true,
		},
		Apps: map[string]AppConfig{
			"Finder": {Disabled: true},
		},
		Menubar: MenubarConfig{
			TimeoutPresets: []Duration{
				{30 * time.Second},
				{1 * time.Minute},
				{2 * time.Minute},
				{5 * time.Minute},
			},
		},
		Focus: FocusConfig{
			KeepRecent: 3,
			Grace:      Duration{10 * time.Second},
		},
	}
}

func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "autohide", "config.toml")
}

func Dir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "autohide")
}

func SocketPath() string {
	return filepath.Join(Dir(), "autohide.sock")
}

func Load(path string) (*Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if err := Save(cfg, path); err != nil {
				return nil, fmt.Errorf("creating default config: %w", err)
			}
			return cfg, nil
		}
		return nil, err
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if cfg.Apps == nil {
		cfg.Apps = make(map[string]AppConfig)
	}
	cfg.normalize()

	return cfg, nil
}

func (c *Config) normalize() {
	if c.Focus.KeepRecent < 1 {
		c.Focus.KeepRecent = 1
	}
	if c.Focus.Grace.Duration < 0 {
		c.Focus.Grace.Duration = 0
	}
}

// Save writes atomically (temp file + rename): the daemon hot-reloads this
// path every tick, so an in-place truncate would let a reload race read a
// half-written file and revert to defaults for a tick.
func Save(cfg *Config, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := toml.NewEncoder(tmp).Encode(cfg); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func (c *Config) EffectiveTimeout(appName string) (time.Duration, bool) {
	if app, ok := c.Apps[appName]; ok {
		if app.Disabled {
			return 0, true
		}
		if app.Timeout.Duration > 0 {
			return app.Timeout.Duration, false
		}
	}
	for _, exc := range c.General.SystemExclude {
		if exc == appName {
			return 0, true
		}
	}
	return c.General.DefaultTimeout.Duration, false
}
