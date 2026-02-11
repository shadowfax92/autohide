package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the daemon via launchd",
	RunE:  runStart,
}

func init() {
	rootCmd.AddCommand(startCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	plist := plistPath()
	if _, err := os.Stat(plist); os.IsNotExist(err) {
		return fmt.Errorf("launchd plist not found. Run 'autohide install' first")
	}

	uid := strconv.Itoa(os.Getuid())
	if err := exec.Command("launchctl", "bootstrap", "gui/"+uid, plist).Run(); err != nil {
		if err2 := exec.Command("launchctl", "load", plist).Run(); err2 != nil {
			return fmt.Errorf("launchctl load failed: %w", err2)
		}
	}

	fmt.Println("Daemon started.")
	return nil
}
