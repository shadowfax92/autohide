package daemon

/*
#cgo LDFLAGS: -framework CoreGraphics -framework CoreFoundation

#include <CoreGraphics/CoreGraphics.h>
#include <CoreFoundation/CoreFoundation.h>

typedef int CGSConnectionID;
extern CGSConnectionID CGSMainConnectionID(void);
extern uint64_t CGSGetActiveSpace(CGSConnectionID cid);
extern CFArrayRef CGSCopyManagedDisplaySpaces(CGSConnectionID cid);
extern void CGSManagedDisplaySetCurrentSpace(CGSConnectionID cid, CFStringRef display, uint64_t space);

static bool switchToWorkspaceIndex(int target) {
	CGSConnectionID conn = CGSMainConnectionID();
	if (conn == 0) return false;

	uint64_t activeSpace = CGSGetActiveSpace(conn);
	CFArrayRef displaySpaces = CGSCopyManagedDisplaySpaces(conn);
	if (!displaySpaces) return false;

	CFIndex displayCount = CFArrayGetCount(displaySpaces);
	for (CFIndex d = 0; d < displayCount; d++) {
		CFDictionaryRef display = CFArrayGetValueAtIndex(displaySpaces, d);
		CFArrayRef spaces = CFDictionaryGetValue(display, CFSTR("Spaces"));
		if (!spaces) continue;

		CFIndex spaceCount = CFArrayGetCount(spaces);
		CFIndex activeIndex = -1;
		for (CFIndex i = 0; i < spaceCount; i++) {
			CFDictionaryRef space = CFArrayGetValueAtIndex(spaces, i);
			CFNumberRef idRef = CFDictionaryGetValue(space, CFSTR("ManagedSpaceID"));
			if (!idRef) continue;

			int64_t spaceId = 0;
			CFNumberGetValue(idRef, kCFNumberSInt64Type, &spaceId);
			if ((uint64_t)spaceId == activeSpace) {
				activeIndex = i;
				break;
			}
		}
		if (activeIndex < 0) continue;
		if (target < 1 || target > spaceCount) {
			CFRelease(displaySpaces);
			return false;
		}
		if ((CFIndex)(target - 1) == activeIndex) {
			CFRelease(displaySpaces);
			return true;
		}

		CFDictionaryRef targetSpace = CFArrayGetValueAtIndex(spaces, target - 1);
		CFNumberRef targetIDRef = CFDictionaryGetValue(targetSpace, CFSTR("ManagedSpaceID"));
		CFStringRef displayID = CFDictionaryGetValue(display, CFSTR("Display Identifier"));
		if (!targetIDRef || !displayID) {
			CFRelease(displaySpaces);
			return false;
		}

		int64_t targetSpaceID = 0;
		CFNumberGetValue(targetIDRef, kCFNumberSInt64Type, &targetSpaceID);
		CGSManagedDisplaySetCurrentSpace(conn, displayID, (uint64_t)targetSpaceID);
		CFRelease(displaySpaces);
		return true;
	}

	CFRelease(displaySpaces);
	return false;
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
	return switchToWorkspaceDirect(target)
}

func switchToWorkspaceDirect(target int) error {
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
	if ok := bool(C.switchToWorkspaceIndex(C.int(target))); !ok {
		return fmt.Errorf("switch to workspace %d failed", target)
	}
	time.Sleep(180 * time.Millisecond)

	newCurrent, _, err := GetWorkspaceInfo()
	if err != nil {
		return err
	}
	if newCurrent != target {
		return fmt.Errorf("switch to workspace %d failed", target)
	}
	return nil
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
