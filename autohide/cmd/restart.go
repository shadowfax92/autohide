package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"github.com/spf13/cobra"
)

const launchdLabel = "com.autohide.daemon"

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the daemon via launchd",
	RunE:  runRestart,
}

func init() {
	rootCmd.AddCommand(restartCmd)
}

func runRestart(cmd *cobra.Command, args []string) error {
	if err := restartLaunchdDaemon(); err != nil {
		return err
	}
	fmt.Println("Daemon restarted.")
	return nil
}

func restartLaunchdDaemon() error {
	uid := strconv.Itoa(os.Getuid())
	target := "gui/" + uid + "/" + launchdLabel
	if err := exec.Command("launchctl", "kickstart", "-k", target).Run(); err == nil {
		return nil
	}

	plist := plistPath()
	if _, err := os.Stat(plist); os.IsNotExist(err) {
		return fmt.Errorf("launchd plist not found. Run 'autohide install' first")
	}
	_ = exec.Command("launchctl", "bootout", target).Run()
	if err := exec.Command("launchctl", "bootstrap", "gui/"+uid, plist).Run(); err != nil {
		if err2 := exec.Command("launchctl", "load", plist).Run(); err2 != nil {
			return fmt.Errorf("restart launchd service: %w", err2)
		}
	}
	return nil
}
