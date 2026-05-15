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

type GeneralConfig struct {
	DefaultTimeout Duration `toml:"default_timeout"`
	CheckInterval  Duration `toml:"check_interval"`
	SystemExclude  []string `toml:"system_exclude"`
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
			SystemExclude:  []string{},
		},
		Apps: map[string]AppConfig{},
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
	migrateLegacyFinderDefault(cfg)

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

// migrateLegacyFinderDefault removes the old generated Finder opt-out so existing
// configs inherit the current behavior. Finder overrides with a non-legacy shape
// are left intact because they carry explicit user intent.
func migrateLegacyFinderDefault(cfg *Config) {
	app, ok := cfg.Apps["Finder"]
	if !ok || !app.Disabled || app.Timeout.Duration != 0 || !containsSystemExclude(cfg.General.SystemExclude, "Finder") {
		return
	}

	cfg.General.SystemExclude = removeSystemExclude(cfg.General.SystemExclude, "Finder")
	delete(cfg.Apps, "Finder")
}

func containsSystemExclude(excludes []string, appName string) bool {
	for _, exclude := range excludes {
		if exclude == appName {
			return true
		}
	}
	return false
}

func removeSystemExclude(excludes []string, appName string) []string {
	filtered := excludes[:0]
	for _, exclude := range excludes {
		if exclude != appName {
			filtered = append(filtered, exclude)
		}
	}
	return filtered
}
