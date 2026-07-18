package cmd

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"autohide/ipc"
)

func TestWriteListShowsUnhidableReason(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	data := ipc.ListData{Apps: []ipc.AppInfo{{
		Name:          "Preview",
		LastActive:    now.Add(-time.Minute).Format(time.RFC3339),
		Timeout:       "1m0s",
		TimeRemaining: "0s",
		WindowCount:   1,
		Unhidable:     "fullscreen",
	}}}
	var out bytes.Buffer

	writeList(&out, data, now)

	if got := out.String(); !strings.Contains(got, "unhidable: fullscreen") {
		t.Fatalf("list output missing reason:\n%s", got)
	}
}
