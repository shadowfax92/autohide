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

	if len(data.Apps) == 0 {
		fmt.Println("No apps tracked yet.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "APP\tTIMEOUT\tLAST ACTIVE\tHIDDEN\tREMAINING\tWINDOWS")

	now := time.Now()
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
		if app.Disabled || app.Hidden {
			remaining = "-"
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\n",
			app.Name, timeout, agoString(now, app.LastActive), hidden, remaining, app.WindowCount)

		for _, win := range app.Windows {
			title := win.Title
			if title == "" {
				title = fmt.Sprintf("window %d", win.ID)
			}
			if runes := []rune(title); len(runes) > 40 {
				title = string(runes[:37]) + "..."
			}
			fmt.Fprintf(w, "  · %s\t\t%s\t\t\t\n", title, agoString(now, win.LastActive))
		}
	}

	w.Flush()
	return nil
}

func agoString(now time.Time, rfc3339 string) string {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return "-"
	}
	return now.Sub(t).Round(time.Second).String() + " ago"
}
