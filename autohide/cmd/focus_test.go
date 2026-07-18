package cmd

import (
	"bytes"
	"strings"
	"testing"

	"autohide/ipc"

	"github.com/spf13/cobra"
)

func TestRunFocusStatusPrintsPolicyAndKeepSet(t *testing.T) {
	original := sendFocusCmd
	sendFocusCmd = func(command string) (*ipc.FocusModeData, error) {
		return &ipc.FocusModeData{
			Active:     true,
			KeepRecent: 3,
			Grace:      "10s",
			KeepSet:    []string{"Terminal", "Slack", "Google Chrome"},
		}, nil
	}
	defer func() { sendFocusCmd = original }()

	var buf bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&buf)
	if err := runFocusStatus(command, nil); err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	for _, want := range []string{
		"Focus mode: on",
		"Keep recent: 3 apps",
		"Grace: 10s",
		"Keep set: Terminal, Slack, Google Chrome",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q:\n%s", want, output)
		}
	}
}
