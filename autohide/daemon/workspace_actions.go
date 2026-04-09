package daemon

/*
#cgo LDFLAGS: -framework ApplicationServices

#include <ApplicationServices/ApplicationServices.h>
#include <unistd.h>

static bool autohideIsAccessibilityTrusted(void) {
	return AXIsProcessTrusted();
}

static void autohideRequestAccessibilityPermission(void) {
	const void *keys[] = { kAXTrustedCheckOptionPrompt };
	const void *values[] = { kCFBooleanTrue };
	CFDictionaryRef options = CFDictionaryCreate(kCFAllocatorDefault, keys, values, 1, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	AXIsProcessTrustedWithOptions(options);
	CFRelease(options);
}

static void postWorkspaceSwitchKey(int keyCode) {
	CGEventSourceRef source = CGEventSourceCreate(kCGEventSourceStateHIDSystemState);
	if (!source) {
		return;
	}

	CGEventRef controlDown = CGEventCreateKeyboardEvent(source, (CGKeyCode)59, true);
	CGEventRef arrowDown = CGEventCreateKeyboardEvent(source, (CGKeyCode)keyCode, true);
	CGEventRef arrowUp = CGEventCreateKeyboardEvent(source, (CGKeyCode)keyCode, false);
	CGEventRef controlUp = CGEventCreateKeyboardEvent(source, (CGKeyCode)59, false);

	if (!controlDown || !arrowDown || !arrowUp || !controlUp) {
		if (controlDown) CFRelease(controlDown);
		if (arrowDown) CFRelease(arrowDown);
		if (arrowUp) CFRelease(arrowUp);
		if (controlUp) CFRelease(controlUp);
		CFRelease(source);
		return;
	}

	CGEventSetFlags(arrowDown, kCGEventFlagMaskControl);
	CGEventSetFlags(arrowUp, kCGEventFlagMaskControl);

	CGEventPost(kCGHIDEventTap, controlDown);
	usleep(10000);
	CGEventPost(kCGHIDEventTap, arrowDown);
	usleep(10000);
	CGEventPost(kCGHIDEventTap, arrowUp);
	usleep(10000);
	CGEventPost(kCGHIDEventTap, controlUp);

	CFRelease(controlDown);
	CFRelease(arrowDown);
	CFRelease(arrowUp);
	CFRelease(controlUp);
	CFRelease(source);
}
*/
import "C"

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"autohide/config"
)

var ErrWorkspacePickerCanceled = errors.New("workspace picker canceled")

type WorkspaceEntry struct {
	Number      int
	Label       string
	DisplayName string
	Current     bool
}

type workspacePickerPayload struct {
	Title string                `json:"title"`
	Items []workspacePickerItem `json:"items"`
}

type workspacePickerItem struct {
	Workspace int    `json:"workspace"`
	Title     string `json:"title"`
	Subtitle  string `json:"subtitle,omitempty"`
	Current   bool   `json:"current"`
}

func NormalizeWorkspaceLabel(label string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(label)), " ")
}

func ListWorkspaceEntries(cfg *config.Config) ([]WorkspaceEntry, int, error) {
	workspaces, current, err := ListCurrentDisplayWorkspaces()
	if err != nil {
		return nil, 0, err
	}

	entries := make([]WorkspaceEntry, 0, len(workspaces))
	for _, ws := range workspaces {
		label := ""
		if cfg != nil {
			label = cfg.WorkspaceLabel(ws.Number)
		}

		name := fmt.Sprintf("Workspace %d", ws.Number)
		if label != "" {
			name = fmt.Sprintf("%s · %s", name, label)
		}

		entries = append(entries, WorkspaceEntry{
			Number:      ws.Number,
			Label:       label,
			DisplayName: name,
			Current:     ws.Current,
		})
	}
	return entries, current, nil
}

func PickWorkspace(cfg *config.Config, title string) (int, error) {
	entries, _, err := ListWorkspaceEntries(cfg)
	if err != nil {
		return 0, err
	}

	items := make([]workspacePickerItem, 0, len(entries))
	for _, entry := range entries {
		items = append(items, workspacePickerItem{
			Workspace: entry.Number,
			Title:     fmt.Sprintf("Workspace %d", entry.Number),
			Subtitle:  entry.Label,
			Current:   entry.Current,
		})
	}

	payload, err := json.Marshal(workspacePickerPayload{
		Title: title,
		Items: items,
	})
	if err != nil {
		return 0, fmt.Errorf("marshal workspace picker payload: %w", err)
	}

	bin, err := findWorkspacePickerBinary()
	if err != nil {
		return 0, err
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd := exec.Command(bin)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		out := strings.TrimSpace(stdout.String())
		errText := strings.TrimSpace(stderr.String())
		if out == "" && errText == "" {
			return 0, ErrWorkspacePickerCanceled
		}
		if errText != "" {
			return 0, fmt.Errorf("workspace picker: %s", errText)
		}
		return 0, fmt.Errorf("workspace picker failed: %w", err)
	}

	target, err := strconv.Atoi(strings.TrimSpace(stdout.String()))
	if err != nil {
		return 0, fmt.Errorf("invalid workspace picker result: %w", err)
	}
	return target, nil
}

func PickWorkspaceAndSwitch(cfg *config.Config) error {
	target, err := PickWorkspace(cfg, "Switch Workspace")
	if err != nil {
		return err
	}
	return SwitchToWorkspace(target)
}

func SwitchToWorkspace(target int) error {
	current, total, err := GetWorkspaceInfo()
	if err != nil {
		return err
	}
	if target < 1 || target > total {
		return fmt.Errorf("workspace %d does not exist on the current display (1-%d)", target, total)
	}
	if target == current {
		return nil
	}
	if !accessibilityTrusted() {
		promptForAccessibilityPermission()
		return fmt.Errorf("switch to workspace %d failed (grant Accessibility access to autohide in System Settings > Privacy & Security > Accessibility)", target)
	}

	keyCode := 124
	steps := target - current
	if steps < 0 {
		keyCode = 123
		steps = -steps
	}

	for step := 0; step < steps; step++ {
		C.postWorkspaceSwitchKey(C.int(keyCode))
		time.Sleep(120 * time.Millisecond)
	}

	time.Sleep(180 * time.Millisecond)
	newCurrent, _, err := GetWorkspaceInfo()
	if err != nil {
		return err
	}
	if newCurrent != target {
		return fmt.Errorf("switch to workspace %d failed (grant Accessibility access to autohide in System Settings > Privacy & Security > Accessibility)", target)
	}
	return nil
}

func accessibilityTrusted() bool {
	return bool(C.autohideIsAccessibilityTrusted())
}

func promptForAccessibilityPermission() {
	C.autohideRequestAccessibilityPermission()
}

func findWorkspacePickerBinary() (string, error) {
	candidates := make([]string, 0, 8)

	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "autohide-workspace-ui"))
		if realExe, err := filepath.EvalSymlinks(exe); err == nil {
			candidates = append(candidates, filepath.Join(filepath.Dir(realExe), "autohide-workspace-ui"))
		}
	}

	if _, file, _, ok := runtime.Caller(0); ok {
		repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(file)))
		candidates = append(candidates,
			filepath.Join(repoRoot, "build", "autohide-workspace-ui"),
			filepath.Join(repoRoot, "autohide-workspace-ui", ".build", "release", "autohide-workspace-ui"),
		)
	}

	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "build", "autohide-workspace-ui"),
			filepath.Join(wd, "autohide-workspace-ui", ".build", "release", "autohide-workspace-ui"),
			filepath.Join(wd, "..", "build", "autohide-workspace-ui"),
			filepath.Join(wd, "..", "autohide-workspace-ui", ".build", "release", "autohide-workspace-ui"),
		)
	}

	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	if path, err := exec.LookPath("autohide-workspace-ui"); err == nil {
		return path, nil
	}

	return "", fmt.Errorf("could not find autohide-workspace-ui helper")
}
