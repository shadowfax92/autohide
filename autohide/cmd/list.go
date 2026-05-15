package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"autohide/config"
	"autohide/ipc"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List tracked apps and their hide status",
	RunE:  runList,
}

func init() {
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	if err := ensureDaemon(); err != nil {
		return err
	}
	client := ipc.NewClient(config.SocketPath())
	resp, err := client.Send(ipc.Request{Command: "list"})
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("daemon: %s", resp.Error)
	}

	raw, _ := json.Marshal(resp.Data)
	var data ipc.ListData
	json.Unmarshal(raw, &data)

	if len(data.Apps) == 0 {
		fmt.Println("No apps tracked yet.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "APP\tTIMEOUT\tLAST ACTIVE\tHIDDEN\tREMAINING")

	now := time.Now()
	for _, app := range data.Apps {
		timeout := app.Timeout
		if app.Disabled {
			timeout = "disabled"
		}

		lastActive := "-"
		if t, err := time.Parse(time.RFC3339, app.LastActive); err == nil {
			ago := now.Sub(t).Round(time.Second)
			lastActive = ago.String() + " ago"
		}

		hidden := "no"
		if app.Hidden {
			hidden = "yes"
		}

		remaining := app.TimeRemaining
		if app.Disabled || app.Hidden {
			remaining = "-"
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", app.Name, timeout, lastActive, hidden, remaining)
	}

	w.Flush()
	return nil
}
