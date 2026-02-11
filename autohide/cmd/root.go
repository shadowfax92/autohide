package cmd

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"time"

	"autohide/config"

	"github.com/spf13/cobra"
)

var (
	cfgFile string
	verbose bool
	version = "dev"
)

func SetVersion(v string) {
	version = v
}

var rootCmd = &cobra.Command{
	Use:   "autohide",
	Short: "Auto-hide inactive macOS application windows",
	Long:  "A CLI daemon that automatically hides macOS app windows after a period of inactivity.",
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("autohide", version)
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default ~/.config/autohide/config.toml)")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "verbose logging")
	rootCmd.AddCommand(versionCmd)
}

func configPath() string {
	if cfgFile != "" {
		return cfgFile
	}
	return ""
}

// ensureDaemon starts the daemon in the background if it isn't running.
// Returns nil if the daemon is reachable (already running or just started).
func ensureDaemon() error {
	sock := config.SocketPath()

	// Check if daemon is already running
	conn, err := net.DialTimeout("unix", sock, 500*time.Millisecond)
	if err == nil {
		conn.Close()
		return nil
	}

	// Not running — start it
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot find own binary: %w", err)
	}

	cmd := exec.Command(exe, "daemon")
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Detach — don't wait for it
	go cmd.Wait()

	// Wait for socket to appear
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		conn, err := net.DialTimeout("unix", sock, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
	}

	return fmt.Errorf("daemon started but socket not ready after 2s")
}
