package cmd

import (
	"encoding/json"
	"fmt"
	"io"
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

var listWindows bool

func init() {
	listCmd.Flags().BoolVar(&listWindows, "windows", false, "show per-window rows under each app")
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	if err := ensureDaemon(); err != nil {
		return err
	}
	req := ipc.Request{Command: "list"}
	if listWindows {
		req.Args = map[string]string{"windows": "true"}
	}
	client := ipc.NewClient(config.SocketPath())
	resp, err := client.Send(req)
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("daemon: %s", resp.Error)
	}

	raw, _ := json.Marshal(resp.Data)
	var data ipc.ListData
	json.Unmarshal(raw, &data)

	writeList(cmd.OutOrStdout(), data, time.Now())
	return nil
}

// writeList writes the tracked-app table, including reasons that prevent app-level hiding.
func writeList(out io.Writer, data ipc.ListData, now time.Time) {
	if len(data.Apps) == 0 {
		fmt.Fprintln(out, "No apps tracked yet.")
		return
	}

	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "APP\tTIMEOUT\tLAST ACTIVE\tHIDDEN\tREMAINING\tWINDOWS\tSTATUS")
	for _, app := range data.Apps {
		timeout := app.Timeout
		if app.Disabled {
			timeout = "disabled"
		}

		hidden := "no"
		if app.Hidden {
			hidden = "yes"
		}

		remaining := app.TimeRemaining
		if app.Disabled || app.Hidden || app.Unhidable != "" {
			remaining = "-"
		}

		status := "-"
		if app.Unhidable != "" {
			status = "unhidable: " + app.Unhidable
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
			app.Name, timeout, agoString(now, app.LastActive), hidden, remaining, app.WindowCount, status)

		for _, win := range app.Windows {
			title := win.Title
			if title == "" {
				title = fmt.Sprintf("window %d", win.ID)
			}
			if runes := []rune(title); len(runes) > 40 {
				title = string(runes[:37]) + "..."
			}
			fmt.Fprintf(w, "  · %s\t\t%s\t\t\t\t\n", title, agoString(now, win.LastActive))
		}
	}

	w.Flush()
}

func agoString(now time.Time, rfc3339 string) string {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return "-"
	}
	return now.Sub(t).Round(time.Second).String() + " ago"
}
