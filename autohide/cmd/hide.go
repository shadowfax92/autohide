package cmd

import (
	"encoding/json"
	"fmt"

	"autohide/config"
	"autohide/ipc"

	"github.com/spf13/cobra"
)

var hideCmd = &cobra.Command{
	Use:   "hide",
	Short: "Hide apps immediately",
}

var hideAllCmd = &cobra.Command{
	Use:   "all",
	Short: "Hide all eligible background apps",
	RunE:  runHideAll,
}

var sendHideAllCmd = sendHideAllRequest

func init() {
	hideCmd.AddCommand(hideAllCmd)
	rootCmd.AddCommand(hideCmd)
}

// sendHideAllRequest asks the daemon to perform the one-shot hide-all action.
func sendHideAllRequest() (*ipc.HideAllData, error) {
	if err := ensureDaemon(); err != nil {
		return nil, err
	}
	client := ipc.NewClient(config.SocketPath())
	resp, err := client.Send(ipc.Request{Command: "hide_all"})
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("daemon: %s", resp.Error)
	}

	raw, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, err
	}
	var data ipc.HideAllData
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

// runHideAll performs a one-shot hide of all eligible background apps.
func runHideAll(cmd *cobra.Command, args []string) error {
	data, err := sendHideAllCmd()
	if err != nil {
		return err
	}

	msg := fmt.Sprintf("Hidden %d %s.", data.Hidden, plural("app", data.Hidden))
	if data.Failed > 0 {
		msg += fmt.Sprintf(" Failed to hide %d %s.", data.Failed, plural("app", data.Failed))
	}
	cmd.Println(msg)
	return nil
}

func plural(s string, n int) string {
	if n == 1 {
		return s
	}
	return s + "s"
}
