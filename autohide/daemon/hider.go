package daemon

import (
	"fmt"
	"os/exec"
	"strings"
)

func GetFrontmostApp() (string, error) {
	out, err := exec.Command("osascript", "-e",
		`tell application "System Events" to get name of first application process whose frontmost is true`,
	).Output()
	if err != nil {
		return "", fmt.Errorf("get frontmost app: %w (grant Automation permissions in System Preferences)", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func GetVisibleApps() ([]string, error) {
	out, err := exec.Command("osascript", "-e",
		`tell application "System Events" to get name of every application process whose visible is true`,
	).Output()
	if err != nil {
		return nil, fmt.Errorf("get visible apps: %w", err)
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ", ")
	apps := make([]string, 0, len(parts))
	for _, p := range parts {
		if name := strings.TrimSpace(p); name != "" {
			apps = append(apps, name)
		}
	}
	return apps, nil
}

func HideApp(name string) error {
	escaped := strings.ReplaceAll(name, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	script := fmt.Sprintf(
		`tell application "System Events" to set visible of application process "%s" to false`,
		escaped,
	)
	if err := exec.Command("osascript", "-e", script).Run(); err != nil {
		return fmt.Errorf("hide %q: %w", name, err)
	}
	return nil
}
