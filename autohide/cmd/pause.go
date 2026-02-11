package cmd

import (
	"encoding/json"
	"fmt"

	"autohide/config"
	"autohide/ipc"

	"github.com/spf13/cobra"
)

var pauseDuration string

var pauseCmd = &cobra.Command{
	Use:   "pause",
	Short: "Pause auto-hiding (e.g. for presentations)",
	Long:  "Pause auto-hiding. Optionally specify a duration after which hiding auto-resumes.",
	RunE:  runPause,
}

func init() {
	pauseCmd.Flags().StringVar(&pauseDuration, "duration", "", "auto-resume after duration (e.g. 30m, 1h)")
	rootCmd.AddCommand(pauseCmd)
}

func runPause(cmd *cobra.Command, args []string) error {
	if err := ensureDaemon(); err != nil {
		return err
	}
	reqArgs := map[string]string{}
	if pauseDuration != "" {
		reqArgs["duration"] = pauseDuration
	}

	client := ipc.NewClient(config.SocketPath())
	resp, err := client.Send(ipc.Request{Command: "pause", Args: reqArgs})
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("daemon: %s", resp.Error)
	}

	raw, _ := json.Marshal(resp.Data)
	var data ipc.PauseData
	json.Unmarshal(raw, &data)

	if data.ResumeAt != "" {
		fmt.Printf("Paused. Will auto-resume at %s\n", data.ResumeAt)
	} else {
		fmt.Println("Paused. Run 'autohide resume' to resume.")
	}
	return nil
}
