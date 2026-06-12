package cmd

import (
	"autohide/daemon"

	"github.com/spf13/cobra"
)

// spawnUIFn is swappable so tests can assert launch behavior without exec'ing.
var spawnUIFn = daemon.SpawnUI

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Open the autohide window",
	RunE:  runUI,
}

func init() {
	rootCmd.AddCommand(uiCmd)
}

func runUI(cmd *cobra.Command, args []string) error {
	if err := ensureDaemon(); err != nil {
		return err
	}
	return spawnUIFn()
}
