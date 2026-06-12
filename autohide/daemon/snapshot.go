package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	helperName = "autohide-helper"
	uiName     = "autohide-ui"
)

type AppRef struct {
	Pid  int32  `json:"pid"`
	Name string `json:"name"`
}

type SnapApp struct {
	Pid    int32  `json:"pid"`
	Name   string `json:"name"`
	Hidden bool   `json:"hidden"`
}

type SnapWindow struct {
	ID    uint32 `json:"id"`
	Pid   int32  `json:"pid"`
	App   string `json:"app"`
	Title string `json:"title"`
}

// Snapshot is the autohide-helper view of one poll tick: regular running
// apps, on-screen windows of the current Space, and what has focus.
// ScreenRecording is a pointer for the same reason as CheckResult's: old
// helper builds omit it, and absent must stay "unknown", not "denied".
type Snapshot struct {
	AXTrusted       bool         `json:"ax_trusted"`
	ScreenRecording *bool        `json:"screen_recording"`
	Frontmost       AppRef       `json:"frontmost"`
	FocusedWindowID uint32       `json:"focused_window_id"`
	Apps            []SnapApp    `json:"apps"`
	Windows         []SnapWindow `json:"windows"`
}

type WindowRef struct {
	ID    uint32
	Pid   int32
	App   string
	Title string
}

type Decisions struct {
	HideApps        []AppRef
	MinimizeWindows []WindowRef
}

// Helper invokes the one-shot autohide-helper binary; every call is a fresh
// process so a wedged helper can only ever cost one tick.
type Helper struct {
	path    string
	timeout time.Duration
}

func NewHelper(path string) *Helper {
	return &Helper{path: path, timeout: 3 * time.Second}
}

// LocateHelper finds autohide-helper next to the daemon binary (the .app
// bundle layout) or on PATH (dev runs).
func LocateHelper() (string, error) {
	return locateBinary(helperName, siblingDirs())
}

// LocateUI finds the bundled window app the same way.
func LocateUI() (string, error) {
	return locateBinary(uiName, siblingDirs())
}

// SpawnUI launches the window app detached; the UI handles single-instancing
// itself (a second launch activates the first and exits).
func SpawnUI() error {
	path, err := LocateUI()
	if err != nil {
		return err
	}
	cmd := exec.Command(path)
	// Own session: without it a UI spawned from the launchd daemon shares
	// the job's process group and gets killed on every daemon exit/restart
	// instead of reconnecting.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

func siblingDirs() []string {
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		// The CLI is installed as a GOBIN symlink to the bundle binary;
		// without resolving it, sibling lookup searches GOBIN and misses.
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		dirs = append(dirs, filepath.Dir(exe))
	}
	return dirs
}

func locateHelper(dirs []string) (string, error) {
	return locateBinary(helperName, dirs)
}

func locateBinary(name string, dirs []string) (string, error) {
	for _, dir := range dirs {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return path, nil
		}
	}
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("%s not found next to daemon or on PATH", name)
}

func (h *Helper) Snapshot() (*Snapshot, error) {
	out, err := h.run("snapshot")
	if err != nil {
		return nil, err
	}
	return parseSnapshot(out)
}

func (h *Helper) Minimize(pid int32, windowID uint32) error {
	_, err := h.run("minimize", strconv.Itoa(int(pid)), strconv.FormatUint(uint64(windowID), 10))
	return err
}

func (h *Helper) Hide(pid int32) error {
	_, err := h.run("hide", strconv.Itoa(int(pid)))
	return err
}

// CheckResult is the helper's permission probe; ScreenRecording is nil for
// old helper builds that only report ax_trusted.
type CheckResult struct {
	AXTrusted       bool  `json:"ax_trusted"`
	ScreenRecording *bool `json:"screen_recording"`
}

// Check reports permission state; with prompt it also triggers the system
// Accessibility dialog (AXIsProcessTrustedWithOptions returns immediately —
// the dialog is async — so the normal helper timeout holds).
func (h *Helper) Check(prompt bool) (*CheckResult, error) {
	args := []string{"check"}
	if prompt {
		args = append(args, "--prompt")
	}
	out, err := h.run(args...)
	if err != nil {
		return nil, err
	}
	var result CheckResult
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parse check output: %w", err)
	}
	return &result, nil
}

func (h *Helper) run(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), h.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, h.path, args...)
	// Without WaitDelay, an orphaned grandchild holding the pipes makes Run
	// block long past the kill (helper children outliving a timeout).
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil && ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("%s %s timed out after %s", helperName, args[0], h.timeout)
	}
	if err != nil {
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return nil, fmt.Errorf("%s %s: %w: %s", helperName, args[0], err, detail)
		}
		return nil, fmt.Errorf("%s %s: %w", helperName, args[0], err)
	}
	return stdout.Bytes(), nil
}

func parseSnapshot(raw []byte) (*Snapshot, error) {
	var snap Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, fmt.Errorf("parse snapshot: %w", err)
	}
	return &snap, nil
}
