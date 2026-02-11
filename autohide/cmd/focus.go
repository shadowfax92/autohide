package cmd

import (
	"encoding/json"
	"fmt"

	"autohide/config"
	"autohide/ipc"

	"github.com/spf13/cobra"
)

var focusCmd = &cobra.Command{
	Use:   "focus",
	Short: "Manage focus mode (instantly hide all apps except frontmost)",
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

func sendFocusCmd(command string) (*ipc.FocusModeData, error) {
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

	raw, _ := json.Marshal(resp.Data)
	var data ipc.FocusModeData
	json.Unmarshal(raw, &data)
	return &data, nil
}

func runFocusOn(cmd *cobra.Command, args []string) error {
	data, err := sendFocusCmd("focus_on")
	if err != nil {
		return err
	}
	if data.Active {
		fmt.Println("Focus mode enabled. Only the frontmost app stays visible.")
	}
	return nil
}

func runFocusOff(cmd *cobra.Command, args []string) error {
	data, err := sendFocusCmd("focus_off")
	if err != nil {
		return err
	}
	if !data.Active {
		fmt.Println("Focus mode disabled. Resuming timeout-based hiding.")
	}
	return nil
}

func runFocusStatus(cmd *cobra.Command, args []string) error {
	data, err := sendFocusCmd("focus_status")
	if err != nil {
		return err
	}
	if data.Active {
		fmt.Println("Focus mode: on")
	} else {
		fmt.Println("Focus mode: off")
	}
	return nil
}
