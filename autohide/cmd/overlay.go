package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"autohide/config"
	"autohide/ipc"

	"github.com/spf13/cobra"
)

var (
	pulseInterval float64
	pulseDuration float64
)

var overlayCmd = &cobra.Command{
	Use:   "overlay",
	Short: "Manage overlay timer sessions",
}

var overlayStartCmd = &cobra.Command{
	Use:   "start <task> <duration>",
	Short: "Start an overlay timer session",
	Long:  "Start an overlay timer. Duration supports Go duration strings: 30m, 1h, 1h30m",
	Args:  cobra.ExactArgs(2),
	RunE:  runOverlayStart,
}

var overlayStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the current overlay session",
	RunE:  runOverlayStop,
}

var overlayPauseCmd = &cobra.Command{
	Use:   "pause",
	Short: "Pause the overlay timer",
	RunE:  runOverlayPause,
}

var overlayResumeCmd = &cobra.Command{
	Use:   "resume",
	Short: "Resume the overlay timer",
	RunE:  runOverlayResume,
}

var overlayStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current overlay session status",
	RunE:  runOverlayStatus,
}

var overlayHideCmd = &cobra.Command{
	Use:   "hide",
	Short: "Hide the overlay without stopping the timer",
	RunE:  runOverlayHide,
}

var overlayShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the overlay again",
	RunE:  runOverlayShow,
}

func init() {
	overlayStartCmd.Flags().Float64Var(&pulseInterval, "pulse-interval", 60, "seconds between pulse animations")
	overlayStartCmd.Flags().Float64Var(&pulseDuration, "pulse-duration", 1.5, "pulse animation duration in seconds")
	overlayCmd.AddCommand(overlayStartCmd)
	overlayCmd.AddCommand(overlayStopCmd)
	overlayCmd.AddCommand(overlayPauseCmd)
	overlayCmd.AddCommand(overlayResumeCmd)
	overlayCmd.AddCommand(overlayStatusCmd)
	overlayCmd.AddCommand(overlayHideCmd)
	overlayCmd.AddCommand(overlayShowCmd)
	rootCmd.AddCommand(overlayCmd)
}

func sendOverlayCmd(command string, args map[string]string) (*ipc.OverlayStatusData, error) {
	if err := ensureDaemon(); err != nil {
		return nil, err
	}
	client := ipc.NewClient(config.SocketPath())
	resp, err := client.Send(ipc.Request{Command: command, Args: args})
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("daemon: %s", resp.Error)
	}

	raw, _ := json.Marshal(resp.Data)
	var data ipc.OverlayStatusData
	json.Unmarshal(raw, &data)
	return &data, nil
}

func printOverlayStatus(data *ipc.OverlayStatusData) {
	if !data.Active {
		fmt.Println("No active overlay session.")
		return
	}

	remaining, _ := time.ParseDuration(data.Remaining)
	minutes := int(remaining.Minutes())
	seconds := int(remaining.Seconds()) % 60

	paused := ""
	if data.Paused {
		paused = " (paused)"
	}

	overlay := "visible"
	if data.OverlayHidden {
		overlay = "hidden"
	}

	fmt.Printf("Task:      %s\n", data.Task)
	fmt.Printf("Duration:  %s\n", data.Duration)
	fmt.Printf("Remaining: %dm%02ds%s\n", minutes, seconds, paused)
	fmt.Printf("Overlay:   %s\n", overlay)
}

func runOverlayStart(cmd *cobra.Command, args []string) error {
	task, durStr := args[0], args[1]

	if _, err := time.ParseDuration(durStr); err != nil {
		return fmt.Errorf("invalid duration %q: %w", durStr, err)
	}

	data, err := sendOverlayCmd("overlay_start", map[string]string{
		"task":           task,
		"duration":       durStr,
		"pulse_interval": strconv.FormatFloat(pulseInterval, 'f', -1, 64),
		"pulse_duration": strconv.FormatFloat(pulseDuration, 'f', -1, 64),
	})
	if err != nil {
		return err
	}

	fmt.Printf("Overlay started: %s (%s)\n", task, durStr)
	printOverlayStatus(data)
	return nil
}

func runOverlayStop(cmd *cobra.Command, args []string) error {
	_, err := sendOverlayCmd("overlay_stop", nil)
	if err != nil {
		return err
	}
	fmt.Println("Overlay stopped.")
	return nil
}

func runOverlayPause(cmd *cobra.Command, args []string) error {
	data, err := sendOverlayCmd("overlay_pause", nil)
	if err != nil {
		return err
	}
	fmt.Println("Timer paused.")
	printOverlayStatus(data)
	return nil
}

func runOverlayResume(cmd *cobra.Command, args []string) error {
	data, err := sendOverlayCmd("overlay_resume", nil)
	if err != nil {
		return err
	}
	fmt.Println("Timer resumed.")
	printOverlayStatus(data)
	return nil
}

func runOverlayStatus(cmd *cobra.Command, args []string) error {
	data, err := sendOverlayCmd("overlay_status", nil)
	if err != nil {
		return err
	}
	printOverlayStatus(data)
	return nil
}

func runOverlayHide(cmd *cobra.Command, args []string) error {
	_, err := sendOverlayCmd("overlay_hide", nil)
	if err != nil {
		return err
	}
	fmt.Println("Overlay hidden. Timer still running.")
	return nil
}

func runOverlayShow(cmd *cobra.Command, args []string) error {
	_, err := sendOverlayCmd("overlay_show", nil)
	if err != nil {
		return err
	}
	fmt.Println("Overlay shown.")
	return nil
}
