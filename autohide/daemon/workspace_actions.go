package daemon

/*
#cgo LDFLAGS: -framework ApplicationServices -framework CoreGraphics -framework CoreFoundation

#include <ApplicationServices/ApplicationServices.h>
#include <CoreFoundation/CoreFoundation.h>
#include <CoreGraphics/CoreGraphics.h>
#include <unistd.h>

typedef int CGSConnectionID;
extern CGSConnectionID CGSMainConnectionID(void);
extern uint64_t CGSGetActiveSpace(CGSConnectionID cid);
extern CFArrayRef CGSCopyManagedDisplaySpaces(CGSConnectionID cid);

static CFArrayRef copyAXChildren(AXUIElementRef element) {
	CFTypeRef value = NULL;
	if (AXUIElementCopyAttributeValue(element, kAXChildrenAttribute, &value) != kAXErrorSuccess || !value) {
		return NULL;
	}
	if (CFGetTypeID(value) != CFArrayGetTypeID()) {
		CFRelease(value);
		return NULL;
	}
	return (CFArrayRef)value;
}

static bool elementIdentifierEquals(AXUIElementRef element, CFStringRef expected) {
	CFTypeRef value = NULL;
	if (AXUIElementCopyAttributeValue(element, CFSTR("AXIdentifier"), &value) != kAXErrorSuccess || !value) {
		return false;
	}
	bool matches = CFGetTypeID(value) == CFStringGetTypeID() && CFStringCompare((CFStringRef)value, expected, 0) == kCFCompareEqualTo;
	CFRelease(value);
	return matches;
}

static AXUIElementRef copyChildWithIdentifier(AXUIElementRef parent, CFStringRef identifier) {
	CFArrayRef children = copyAXChildren(parent);
	if (!children) return NULL;

	AXUIElementRef result = NULL;
	CFIndex count = CFArrayGetCount(children);
	for (CFIndex i = 0; i < count; i++) {
		AXUIElementRef child = (AXUIElementRef)CFArrayGetValueAtIndex(children, i);
		if (elementIdentifierEquals(child, identifier)) {
			result = (AXUIElementRef)CFRetain(child);
			break;
		}
	}
	CFRelease(children);
	return result;
}

static bool elementDisplayIDEquals(AXUIElementRef element, uint32_t displayID) {
	CFTypeRef value = NULL;
	if (AXUIElementCopyAttributeValue(element, CFSTR("AXDisplayID"), &value) != kAXErrorSuccess || !value) {
		return false;
	}
	int actual = 0;
	bool matches = CFGetTypeID(value) == CFNumberGetTypeID() &&
		CFNumberGetValue((CFNumberRef)value, kCFNumberIntType, &actual) &&
		(uint32_t)actual == displayID;
	CFRelease(value);
	return matches;
}

static AXUIElementRef copyDisplayElement(AXUIElementRef missionControl, uint32_t displayID) {
	CFArrayRef children = copyAXChildren(missionControl);
	if (!children) return NULL;

	AXUIElementRef result = NULL;
	CFIndex count = CFArrayGetCount(children);
	for (CFIndex i = 0; i < count; i++) {
		AXUIElementRef child = (AXUIElementRef)CFArrayGetValueAtIndex(children, i);
		if (elementIdentifierEquals(child, CFSTR("mc.display")) && elementDisplayIDEquals(child, displayID)) {
			result = (AXUIElementRef)CFRetain(child);
			break;
		}
	}
	CFRelease(children);
	return result;
}

static AXUIElementRef copyMissionControlElement(AXUIElementRef dock) {
	return copyChildWithIdentifier(dock, CFSTR("mc"));
}

static AXUIElementRef waitForMissionControlElement(AXUIElementRef dock) {
	for (int i = 0; i < 40; i++) {
		AXUIElementRef missionControl = copyMissionControlElement(dock);
		if (missionControl) return missionControl;
		usleep(50000);
	}
	return NULL;
}

static bool activeDisplayIDAndWorkspaceCount(uint32_t *displayID, int *workspaceCount) {
	*displayID = 0;
	*workspaceCount = 0;

	CGSConnectionID conn = CGSMainConnectionID();
	if (conn == 0) return false;

	uint64_t activeSpace = CGSGetActiveSpace(conn);
	CFArrayRef displaySpaces = CGSCopyManagedDisplaySpaces(conn);
	if (!displaySpaces) return false;

	bool found = false;
	CFIndex displayCount = CFArrayGetCount(displaySpaces);
	for (CFIndex d = 0; d < displayCount && !found; d++) {
		CFDictionaryRef display = CFArrayGetValueAtIndex(displaySpaces, d);
		CFArrayRef spaces = CFDictionaryGetValue(display, CFSTR("Spaces"));
		CFStringRef displayUUID = CFDictionaryGetValue(display, CFSTR("Display Identifier"));
		if (!spaces || !displayUUID) continue;

		CFIndex spaceCount = CFArrayGetCount(spaces);
		for (CFIndex i = 0; i < spaceCount; i++) {
			CFDictionaryRef space = CFArrayGetValueAtIndex(spaces, i);
			CFNumberRef idRef = CFDictionaryGetValue(space, CFSTR("ManagedSpaceID"));
			if (!idRef) continue;

			int64_t spaceID = 0;
			CFNumberGetValue(idRef, kCFNumberSInt64Type, &spaceID);
			if ((uint64_t)spaceID != activeSpace) continue;

			if (CFStringCompare(displayUUID, CFSTR("Main"), 0) == kCFCompareEqualTo) {
				*displayID = CGMainDisplayID();
				*workspaceCount = (int)spaceCount;
				found = true;
				break;
			}

			CFUUIDRef targetUUID = CFUUIDCreateFromString(NULL, displayUUID);
			if (!targetUUID) break;

			uint32_t onlineCount = 0;
			CGDirectDisplayID displays[32];
			if (CGGetOnlineDisplayList(32, displays, &onlineCount) == kCGErrorSuccess) {
				for (uint32_t j = 0; j < onlineCount; j++) {
					CFUUIDRef uuid = CGDisplayCreateUUIDFromDisplayID(displays[j]);
					if (uuid && CFEqual(uuid, targetUUID)) {
						*displayID = displays[j];
						*workspaceCount = (int)spaceCount;
						found = true;
					}
					if (uuid) CFRelease(uuid);
					if (found) break;
				}
			}
			CFRelease(targetUUID);
			break;
		}
	}

	CFRelease(displaySpaces);
	return found;
}

static int pressMissionControlWorkspace(pid_t dockPID, int target) {
	uint32_t displayID = 0;
	int workspaceCount = 0;
	if (!activeDisplayIDAndWorkspaceCount(&displayID, &workspaceCount)) return 1;
	if (target < 1 || target > workspaceCount) return 2;

	AXUIElementRef dock = AXUIElementCreateApplication(dockPID);
	if (!dock) return 3;

	AXUIElementRef missionControl = waitForMissionControlElement(dock);
	if (!missionControl) {
		CFRelease(dock);
		return 5;
	}

	usleep(300000);

	AXUIElementRef display = copyDisplayElement(missionControl, displayID);
	AXUIElementRef spaces = display ? copyChildWithIdentifier(display, CFSTR("mc.spaces")) : NULL;
	AXUIElementRef list = spaces ? copyChildWithIdentifier(spaces, CFSTR("mc.spaces.list")) : NULL;
	CFArrayRef listChildren = list ? copyAXChildren(list) : NULL;

	int result = 0;
	if (!display) result = 6;
	if (display && !spaces) result = 7;
	if (spaces && !list) result = 8;
	if (list && !listChildren) result = 9;
	if (listChildren && (target - 1) < CFArrayGetCount(listChildren)) {
		AXUIElementRef child = (AXUIElementRef)CFArrayGetValueAtIndex(listChildren, target - 1);
		result = AXUIElementPerformAction(child, kAXPressAction) == kAXErrorSuccess ? 0 : 11;
	} else if (listChildren) {
		result = 10;
	}

	if (listChildren) CFRelease(listChildren);
	if (list) CFRelease(list);
	if (spaces) CFRelease(spaces);
	if (display) CFRelease(display);
	CFRelease(missionControl);
	CFRelease(dock);
	return result;
}

static bool accessibilityTrusted(void) {
	return AXIsProcessTrusted();
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

	err = cmd.Run()
	target, parseErr := parseWorkspacePickerSelection(stdout.String())
	if err != nil {
		if parseErr == nil {
			return target, nil
		}
		errText := pickerErrorText(stderr.String())
		if errText == "" {
			return 0, ErrWorkspacePickerCanceled
		}
		return 0, fmt.Errorf("workspace picker: %s", errText)
	}
	if parseErr != nil {
		return 0, fmt.Errorf("invalid workspace picker result: %w", parseErr)
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
	return switchToWorkspaceWithMissionControl(target, GetWorkspaceInfo, pressMissionControlWorkspace, time.Sleep)
}

type workspaceInfoFunc func() (int, int, error)
type workspacePressFunc func(int) error
type workspaceSleeper func(time.Duration)

func switchToWorkspaceWithMissionControl(target int, workspaceInfo workspaceInfoFunc, pressWorkspace workspacePressFunc, sleep workspaceSleeper) error {
	current, total, err := workspaceInfo()
	if err != nil {
		return err
	}
	if target < 1 || target > total {
		return fmt.Errorf("workspace %d does not exist on the current display (1-%d)", target, total)
	}
	if target == current {
		return nil
	}

	if err := pressWorkspace(target); err != nil {
		return fmt.Errorf("switch to workspace %d: %w", target, err)
	}
	if err := waitForWorkspace(target, 4*time.Second, workspaceInfo, sleep); err != nil {
		return fmt.Errorf("switch to workspace %d: %w", target, err)
	}
	return nil
}

func pressMissionControlWorkspace(target int) error {
	if !bool(C.accessibilityTrusted()) {
		return fmt.Errorf("Accessibility permission is required; grant it to autohide.app in System Settings > Privacy & Security > Accessibility, then restart autohide")
	}

	pid, err := dockPID()
	if err != nil {
		return err
	}
	if err := openMissionControl(); err != nil {
		return err
	}
	if code := int(C.pressMissionControlWorkspace(C.pid_t(pid), C.int(target))); code != 0 {
		return fmt.Errorf("%s", missionControlSwitchError(target, code))
	}
	return nil
}

func openMissionControl() error {
	if err := exec.Command("open", "-a", "Mission Control").Run(); err != nil {
		return fmt.Errorf("open Mission Control: %w", err)
	}
	return nil
}

func missionControlSwitchError(target int, code int) string {
	switch code {
	case 1:
		return "could not map the active Space to a Mission Control display"
	case 2:
		return fmt.Sprintf("workspace %d does not exist on the active display", target)
	case 3:
		return "could not create Dock accessibility element"
	case 4:
		return "could not open Mission Control through Dock"
	case 5:
		return "Dock did not expose the Mission Control accessibility group"
	case 6:
		return "Dock did not expose a Mission Control display matching the active display"
	case 7:
		return "Dock did not expose the Mission Control spaces group"
	case 8:
		return "Dock did not expose the Mission Control spaces list"
	case 9:
		return "Dock exposed a Mission Control spaces list without children"
	case 10:
		return fmt.Sprintf("Mission Control spaces list does not include workspace %d", target)
	case 11:
		return fmt.Sprintf("Mission Control could not press workspace %d", target)
	default:
		return fmt.Sprintf("Mission Control workspace switch failed with code %d", code)
	}
}

func dockPID() (int, error) {
	out, err := exec.Command("pgrep", "-x", "Dock").Output()
	if err != nil {
		return 0, fmt.Errorf("find Dock process: %w", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return 0, fmt.Errorf("Dock process not found")
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, fmt.Errorf("parse Dock pid %q: %w", fields[0], err)
	}
	return pid, nil
}

func waitForWorkspace(target int, timeout time.Duration, workspaceInfo workspaceInfoFunc, sleep workspaceSleeper) error {
	deadline := time.Now().Add(timeout)
	var lastCurrent int
	var lastErr error
	for {
		current, _, err := workspaceInfo()
		if err == nil && current == target {
			return nil
		}
		lastCurrent = current
		lastErr = err
		if time.Now().After(deadline) {
			break
		}
		sleep(40 * time.Millisecond)
	}
	if lastErr != nil {
		return fmt.Errorf("workspace %d was not reached: %w", target, lastErr)
	}
	return fmt.Errorf("workspace %d was not reached; current workspace is %d", target, lastCurrent)
}

func parseWorkspacePickerSelection(output string) (int, error) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		target, err := strconv.Atoi(line)
		if err == nil && target > 0 {
			return target, nil
		}
	}
	return 0, fmt.Errorf("no workspace number in picker output")
}

func pickerErrorText(output string) string {
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "+[IMKClient subclass]") || strings.Contains(line, "+[IMKInputSession subclass]") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
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
