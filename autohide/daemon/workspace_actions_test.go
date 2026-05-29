package daemon

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseWorkspacePickerSelection(t *testing.T) {
	target, err := parseWorkspacePickerSelection("3\n")
	if err != nil {
		t.Fatalf("parse picker selection: %v", err)
	}
	if target != 3 {
		t.Fatalf("got target=%d, want 3", target)
	}
}

func TestParseWorkspacePickerSelectionIgnoresNoise(t *testing.T) {
	output := "2026-04-15 AppKit noise\n\n7\n"
	target, err := parseWorkspacePickerSelection(output)
	if err != nil {
		t.Fatalf("parse picker selection with noise: %v", err)
	}
	if target != 7 {
		t.Fatalf("got target=%d, want 7", target)
	}
}

func TestParseWorkspacePickerSelectionRejectsEmptyOutput(t *testing.T) {
	_, err := parseWorkspacePickerSelection("\n \n")
	if err == nil {
		t.Fatal("expected empty picker output error")
	}
}

func TestPickerErrorTextFiltersIMKNoise(t *testing.T) {
	output := "2026-04-15 autohide-workspace-ui[1] +[IMKClient subclass]: chose IMKClient_Modern\nworkspace picker input error: bad json\n"
	errText := pickerErrorText(output)
	if strings.Contains(errText, "IMKClient") {
		t.Fatalf("expected IMK noise to be filtered, got %q", errText)
	}
	if !strings.Contains(errText, "bad json") {
		t.Fatalf("expected meaningful picker error, got %q", errText)
	}
}

func TestSwitchToWorkspaceWithMissionControlPressesTarget(t *testing.T) {
	current := 2
	var pressed []int
	err := switchToWorkspaceWithMissionControl(4, func() (int, int, error) {
		return current, 5, nil
	}, func(target int) error {
		pressed = append(pressed, target)
		current = target
		return nil
	}, func(time.Duration) {})
	if err != nil {
		t.Fatalf("switch workspace: %v", err)
	}
	if want := []int{4}; !reflect.DeepEqual(pressed, want) {
		t.Fatalf("pressed workspaces=%v, want %v", pressed, want)
	}
}

func TestSwitchToWorkspaceWithMissionControlRejectsInvalidTarget(t *testing.T) {
	err := switchToWorkspaceWithMissionControl(6, func() (int, int, error) {
		return 2, 5, nil
	}, func(int) error {
		t.Fatal("press workspace should not be called")
		return nil
	}, func(time.Duration) {})
	if err == nil {
		t.Fatal("expected invalid target error")
	}
}

func TestSwitchToWorkspaceWithMissionControlReportsPressError(t *testing.T) {
	pressErr := errors.New("mission control unavailable")
	err := switchToWorkspaceWithMissionControl(4, func() (int, int, error) {
		return 2, 5, nil
	}, func(int) error {
		return pressErr
	}, func(time.Duration) {})
	if !errors.Is(err, pressErr) {
		t.Fatalf("got err=%v, want %v", err, pressErr)
	}
}

func TestWaitForWorkspaceReportsLastReadError(t *testing.T) {
	readErr := errors.New("spaces unavailable")
	err := waitForWorkspace(3, time.Millisecond, func() (int, int, error) {
		return 0, 0, readErr
	}, func(time.Duration) {})
	if !errors.Is(err, readErr) {
		t.Fatalf("got err=%v, want %v", err, readErr)
	}
}
