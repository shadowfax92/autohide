package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"autohide/config"

	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print current config",
	RunE:  runConfigShow,
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print config file path",
	Run: func(cmd *cobra.Command, args []string) {
		p := configPath()
		if p == "" {
			p = config.DefaultPath()
		}
		fmt.Println(p)
	},
}

var configEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Open config in $EDITOR",
	RunE:  runConfigEdit,
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a global config value",
	Long:  "Keys: default_timeout, check_interval",
	Args:  cobra.ExactArgs(2),
	RunE:  runConfigSet,
}

var configSetAppCmd = &cobra.Command{
	Use:   "set-app <app> <key> <value>",
	Short: "Set a per-app config value",
	Long:  "Keys: timeout, disabled",
	Args:  cobra.ExactArgs(3),
	RunE:  runConfigSetApp,
}

func init() {
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configPathCmd)
	configCmd.AddCommand(configEditCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configSetAppCmd)
	rootCmd.AddCommand(configCmd)
}

func loadConfig() (*config.Config, string) {
	p := configPath()
	if p == "" {
		p = config.DefaultPath()
	}
	cfg, err := config.Load(p)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}
	return cfg, p
}

func runConfigShow(cmd *cobra.Command, args []string) error {
	p := configPath()
	if p == "" {
		p = config.DefaultPath()
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := config.Default()
			if err := config.Save(cfg, p); err != nil {
				return err
			}
			data, err = os.ReadFile(p)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}
	fmt.Print(string(data))
	return nil
}

func runConfigEdit(cmd *cobra.Command, args []string) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	p := configPath()
	if p == "" {
		p = config.DefaultPath()
	}
	// Ensure file exists
	if _, err := os.Stat(p); os.IsNotExist(err) {
		cfg := config.Default()
		if err := config.Save(cfg, p); err != nil {
			return err
		}
	}
	c := exec.Command(editor, p)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	key, value := args[0], args[1]
	cfg, p := loadConfig()

	switch key {
	case "default_timeout":
		d, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("invalid duration: %w", err)
		}
		cfg.General.DefaultTimeout = config.Duration{Duration: d}
	case "check_interval":
		d, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("invalid duration: %w", err)
		}
		cfg.General.CheckInterval = config.Duration{Duration: d}
	default:
		return fmt.Errorf("unknown key: %s (valid: default_timeout, check_interval)", key)
	}

	if err := config.Save(cfg, p); err != nil {
		return err
	}
	fmt.Printf("Set %s = %s\n", key, value)
	return nil
}

func runConfigSetApp(cmd *cobra.Command, args []string) error {
	appName, key, value := args[0], args[1], args[2]
	cfg, p := loadConfig()

	app := cfg.Apps[appName]

	switch key {
	case "timeout":
		d, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("invalid duration: %w", err)
		}
		app.Timeout = config.Duration{Duration: d}
		app.Disabled = false
	case "disabled":
		switch value {
		case "true", "1", "yes":
			app.Disabled = true
		case "false", "0", "no":
			app.Disabled = false
		default:
			return fmt.Errorf("invalid value for disabled: %s (use true/false)", value)
		}
	default:
		return fmt.Errorf("unknown key: %s (valid: timeout, disabled)", key)
	}

	cfg.Apps[appName] = app

	if err := config.Save(cfg, p); err != nil {
		return err
	}
	fmt.Printf("Set %s.%s = %s\n", appName, key, value)
	return nil
}
