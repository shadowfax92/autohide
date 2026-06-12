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
	DefaultTimeout Duration `toml:"default_timeout"`
	CheckInterval  Duration `toml:"check_interval"`
	SystemExclude  []string `toml:"system_exclude"`
	WindowTracking bool     `toml:"window_tracking"`
}

type AppConfig struct {
	Timeout  Duration `toml:"timeout,omitempty"`
	Disabled bool     `toml:"disabled,omitempty"`
}

type MenubarConfig struct {
	TimeoutPresets []Duration `toml:"timeout_presets"`
}

type Config struct {
	General GeneralConfig        `toml:"general"`
	Apps    map[string]AppConfig `toml:"apps"`
	Menubar MenubarConfig        `toml:"menubar"`
}

func Default() *Config {
	return &Config{
		General: GeneralConfig{
			DefaultTimeout: Duration{1 * time.Minute},
			CheckInterval:  Duration{5 * time.Second},
			SystemExclude:  []string{"Finder"},
			WindowTracking: true,
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

	return cfg, nil
}

func Save(cfg *Config, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
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
