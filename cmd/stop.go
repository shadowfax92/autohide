package cmd

import (
	"fmt"
	"os/exec"
	"os"
	"strconv"

	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the daemon via launchd",
	RunE:  runStop,
}

func init() {
	rootCmd.AddCommand(stopCmd)
}

func runStop(cmd *cobra.Command, args []string) error {
	uid := strconv.Itoa(os.Getuid())
	if err := exec.Command("launchctl", "bootout", "gui/"+uid+"/com.autohide.daemon").Run(); err != nil {
		plist := plistPath()
		if err2 := exec.Command("launchctl", "unload", plist).Run(); err2 != nil {
			return fmt.Errorf("failed to stop daemon: %w", err2)
		}
	}

	fmt.Println("Daemon stopped.")
	return nil
}
