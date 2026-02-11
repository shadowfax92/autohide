package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"text/template"

	"autohide/config"

	"github.com/spf13/cobra"
)

const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.autohide.daemon</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.BinaryPath}}</string>
        <string>daemon</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>{{.LogPath}}</string>
    <key>StandardErrorPath</key>
    <string>{{.LogPath}}</string>
</dict>
</plist>
`

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install launchd service for auto-start on login",
	RunE:  runInstall,
}

func init() {
	rootCmd.AddCommand(installCmd)
}

func plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", "com.autohide.daemon.plist")
}

func runInstall(cmd *cobra.Command, args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine binary path: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("resolve symlinks: %w", err)
	}

	logPath := filepath.Join(config.Dir(), "daemon.log")

	tmpl, err := template.New("plist").Parse(plistTemplate)
	if err != nil {
		return err
	}

	plist := plistPath()
	if err := os.MkdirAll(filepath.Dir(plist), 0755); err != nil {
		return err
	}

	f, err := os.Create(plist)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := tmpl.Execute(f, struct {
		BinaryPath string
		LogPath    string
	}{
		BinaryPath: exe,
		LogPath:    logPath,
	}); err != nil {
		return err
	}

	if err := os.MkdirAll(config.Dir(), 0755); err != nil {
		return err
	}

	uid := strconv.Itoa(os.Getuid())
	if err := exec.Command("launchctl", "bootstrap", "gui/"+uid, plist).Run(); err != nil {
		// Fallback to legacy load
		if err2 := exec.Command("launchctl", "load", plist).Run(); err2 != nil {
			return fmt.Errorf("launchctl load failed: %w", err2)
		}
	}

	fmt.Printf("Installed and started.\n")
	fmt.Printf("  Plist:  %s\n", plist)
	fmt.Printf("  Log:    %s\n", logPath)
	fmt.Printf("  Config: %s\n", config.DefaultPath())
	return nil
}
