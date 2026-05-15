package daemon

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

const windowSnapshotScript = `
on replaceText(findText, replacementText, sourceText)
	set oldDelimiters to AppleScript's text item delimiters
	set AppleScript's text item delimiters to findText
	set textItems to text items of sourceText
	set AppleScript's text item delimiters to replacementText
	set newText to textItems as text
	set AppleScript's text item delimiters to oldDelimiters
	return newText
end replaceText

on cleanField(sourceText)
	set cleanedText to sourceText as text
	set cleanedText to my replaceText(tab, " ", cleanedText)
	set cleanedText to my replaceText(linefeed, " ", cleanedText)
	set cleanedText to my replaceText(return, " ", cleanedText)
	return cleanedText
end cleanField

set windowRows to {}
tell application "System Events"
	set frontAppName to ""
	try
		set frontProcess to first application process whose frontmost is true
		set frontAppName to name of frontProcess as text
	end try

	repeat with proc in application processes
		try
			if visible of proc is true then
				set appName to name of proc as text
				repeat with windowIndex from 1 to count of windows of proc
					set windowSlot to windowIndex as integer
					set candidateWindow to window windowSlot of proc
					set windowTitle to ""
					try
						set windowTitle to name of candidateWindow as text
					end try

					set windowNumber to ""
					try
						set windowNumber to value of attribute "AXWindowNumber" of candidateWindow as text
					end try

					set windowPosition to ""
					try
						set windowPosition to value of attribute "AXPosition" of candidateWindow as text
					end try

					set windowSize to ""
					try
						set windowSize to value of attribute "AXSize" of candidateWindow as text
					end try

					set isMinimized to false
					try
						set isMinimized to value of attribute "AXMinimized" of candidateWindow
					end try

					set isFrontWindow to false
					if appName is frontAppName and windowSlot is 1 and isMinimized is false then
						set isFrontWindow to true
					end if

					set rowText to my cleanField(appName) & tab & my cleanField(windowTitle) & tab & (windowSlot as text) & tab & windowNumber & tab & windowPosition & tab & windowSize & tab & (isMinimized as text) & tab & (isFrontWindow as text)
					copy rowText to end of windowRows
				end repeat
			end if
		end try
	end repeat
end tell

set AppleScript's text item delimiters to linefeed
return windowRows as text
`

// GetWindowSnapshot returns the current front window and all visible-process
// windows so the daemon can age and hide each window independently.
func GetWindowSnapshot() (WindowInfo, []WindowInfo, error) {
	out, err := exec.Command("osascript", "-e", windowSnapshotScript).Output()
	if err != nil {
		return WindowInfo{}, nil, fmt.Errorf("get window snapshot: %w (grant Automation permissions in System Preferences)", err)
	}
	return parseWindowSnapshot(string(out))
}

// HideWindow minimizes one Accessibility window. macOS "hide" is app-scoped,
// so per-window hiding uses AXMinimized to avoid hiding sibling windows.
func HideWindow(window WindowInfo) error {
	script := `
on run argv
	set targetApp to item 1 of argv
	set targetNumber to item 2 of argv
	set targetIndex to (item 3 of argv) as integer
	set targetTitle to item 4 of argv

	tell application "System Events"
		tell application process targetApp
			set matchedWindow to missing value

			if targetNumber is not "" then
				repeat with candidateWindow in windows
					try
						if (value of attribute "AXWindowNumber" of candidateWindow as text) is targetNumber then
							set matchedWindow to candidateWindow
							exit repeat
						end if
					end try
				end repeat
			end if

			if matchedWindow is missing value then
				if targetIndex > 0 and targetIndex is less than or equal to (count of windows) then
					set candidateWindow to window targetIndex
					try
						if targetTitle is "" or (name of candidateWindow as text) is targetTitle then
							set matchedWindow to candidateWindow
						end if
					on error
						set matchedWindow to candidateWindow
					end try
				end if
			end if

			if matchedWindow is missing value then
				error "window not found"
			end if

			set value of attribute "AXMinimized" of matchedWindow to true
		end tell
	end tell
end run
`
	out, err := exec.Command("osascript", "-e", script, window.AppName, window.WindowNumber, strconv.Itoa(window.Index), window.Title).CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail != "" {
			return fmt.Errorf("hide window %q in %q: %w: %s", window.Title, window.AppName, err, detail)
		}
		return fmt.Errorf("hide window %q in %q: %w", window.Title, window.AppName, err)
	}
	return nil
}

func parseWindowSnapshot(raw string) (WindowInfo, []WindowInfo, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return WindowInfo{}, nil, nil
	}

	var frontmost WindowInfo
	lines := strings.Split(raw, "\n")
	windows := make([]WindowInfo, 0, len(lines))
	for _, line := range lines {
		fields := strings.Split(line, "\t")
		if len(fields) != 8 {
			return WindowInfo{}, nil, fmt.Errorf("parse window snapshot row %q: got %d fields, want 8", line, len(fields))
		}

		index, err := strconv.Atoi(fields[2])
		if err != nil {
			return WindowInfo{}, nil, fmt.Errorf("parse window index %q: %w", fields[2], err)
		}

		window := normalizeWindow(WindowInfo{
			AppName:      fields[0],
			Title:        fields[1],
			Index:        index,
			WindowNumber: fields[3],
			Position:     fields[4],
			Size:         fields[5],
			Minimized:    fields[6] == "true",
		})

		windows = append(windows, window)
		if fields[7] == "true" {
			frontmost = window
		}
	}

	return frontmost, windows, nil
}
