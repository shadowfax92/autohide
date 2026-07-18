package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"autohide/config"
	"autohide/ipc"

	"github.com/spf13/cobra"
)

var focusCmd = &cobra.Command{
	Use:   "focus",
	Short: "Manage focus mode (keep recent apps visible)",
}

var focusOnCmd = &cobra.Command{
	Use:   "on",
	Short: "Enable focus mode",
	RunE:  runFocusOn,
}

var focusOffCmd = &cobra.Command{
	Use:   "off",
	Short: "Disable focus mode",
	RunE:  runFocusOff,
}

var focusStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show focus mode state",
	RunE:  runFocusStatus,
}

func init() {
	focusCmd.AddCommand(focusOnCmd)
	focusCmd.AddCommand(focusOffCmd)
	focusCmd.AddCommand(focusStatusCmd)
	rootCmd.AddCommand(focusCmd)
}

var sendFocusCmd = sendFocusRequest

func sendFocusRequest(command string) (*ipc.FocusModeData, error) {
	if err := ensureDaemon(); err != nil {
		return nil, err
	}
	client := ipc.NewClient(config.SocketPath())
	resp, err := client.Send(ipc.Request{Command: command})
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
	var data ipc.FocusModeData
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

func runFocusOn(cmd *cobra.Command, args []string) error {
	data, err := sendFocusCmd("focus_on")
	if err != nil {
		return err
	}
	if data.Active {
		cmd.Printf("Focus mode enabled. Keeping %d recent %s visible; others hide after %s.\n",
			data.KeepRecent, plural("app", data.KeepRecent), data.Grace)
	}
	return nil
}

func runFocusOff(cmd *cobra.Command, args []string) error {
	data, err := sendFocusCmd("focus_off")
	if err != nil {
		return err
	}
	if !data.Active {
		cmd.Println("Focus mode disabled. Resuming timeout-based hiding.")
	}
	return nil
}

func runFocusStatus(cmd *cobra.Command, args []string) error {
	data, err := sendFocusCmd("focus_status")
	if err != nil {
		return err
	}
	if data.Active {
		cmd.Println("Focus mode: on")
	} else {
		cmd.Println("Focus mode: off")
	}
	cmd.Printf("Keep recent: %d %s\n", data.KeepRecent, plural("app", data.KeepRecent))
	cmd.Printf("Grace: %s\n", data.Grace)
	keepSet := "(none yet)"
	if len(data.KeepSet) > 0 {
		keepSet = strings.Join(data.KeepSet, ", ")
	}
	cmd.Printf("Keep set: %s\n", keepSet)
	return nil
}
