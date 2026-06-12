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
	"time"
)

const helperName = "autohide-helper"

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
type Snapshot struct {
	AXTrusted       bool         `json:"ax_trusted"`
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
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	return locateHelper(dirs)
}

func locateHelper(dirs []string) (string, error) {
	for _, dir := range dirs {
		path := filepath.Join(dir, helperName)
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return path, nil
		}
	}
	if path, err := exec.LookPath(helperName); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("%s not found next to daemon or on PATH", helperName)
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
	if ctx.Err() == context.DeadlineExceeded {
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
