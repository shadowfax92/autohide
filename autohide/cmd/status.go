package cmd

import (
	"encoding/json"
	"fmt"

	"autohide/config"
	"autohide/ipc"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon status",
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	if err := ensureDaemon(); err != nil {
		return err
	}
	client := ipc.NewClient(config.SocketPath())
	resp, err := client.Send(ipc.Request{Command: "status"})
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("daemon: %s", resp.Error)
	}

	raw, _ := json.Marshal(resp.Data)
	var data ipc.StatusData
	json.Unmarshal(raw, &data)

	paused := "no"
	if data.Paused {
		paused = "yes"
		if data.ResumeAt != "" {
			paused = fmt.Sprintf("yes (resumes at %s)", data.ResumeAt)
		}
	}

	fmt.Printf("Status:    running\n")
	fmt.Printf("Paused:    %s\n", paused)
	fmt.Printf("Uptime:    %s\n", data.Uptime)
	fmt.Printf("Tracked:   %d apps\n", data.TrackedCount)
	return nil
}
