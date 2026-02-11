package cmd

import (
	"fmt"

	"autohide/config"
	"autohide/ipc"

	"github.com/spf13/cobra"
)

var resumeCmd = &cobra.Command{
	Use:   "resume",
	Short: "Resume auto-hiding after a pause",
	RunE:  runResume,
}

func init() {
	rootCmd.AddCommand(resumeCmd)
}

func runResume(cmd *cobra.Command, args []string) error {
	client := ipc.NewClient(config.SocketPath())
	resp, err := client.Send(ipc.Request{Command: "resume"})
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("daemon: %s", resp.Error)
	}

	fmt.Println("Resumed.")
	return nil
}
