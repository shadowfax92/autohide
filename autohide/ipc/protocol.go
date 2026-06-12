package ipc

type Request struct {
	Command string            `json:"command"`
	Args    map[string]string `json:"args,omitempty"`
}

type Response struct {
	OK    bool        `json:"ok"`
	Data  interface{} `json:"data,omitempty"`
	Error string      `json:"error,omitempty"`
}

type StatusData struct {
	Running        bool   `json:"running"`
	Paused         bool   `json:"paused"`
	FocusMode      bool   `json:"focus_mode"`
	Uptime         string `json:"uptime"`
	TrackedCount   int    `json:"tracked_count"`
	ResumeAt       string `json:"resume_at,omitempty"`
	WindowTracking string `json:"window_tracking,omitempty"`
	// Permission state is nil until the daemon's first native tick observes it.
	AXTrusted       *bool    `json:"ax_trusted,omitempty"`
	ScreenRecording *bool    `json:"screen_recording,omitempty"`
	DefaultTimeout  string   `json:"default_timeout,omitempty"`
	TimeoutPresets  []string `json:"timeout_presets,omitempty"`
}

type WindowInfo struct {
	ID            uint32 `json:"id"`
	Title         string `json:"title,omitempty"`
	LastActive    string `json:"last_active"`
	TimeRemaining string `json:"time_remaining"`
}

type AppInfo struct {
	Name          string       `json:"name"`
	LastActive    string       `json:"last_active"`
	Timeout       string       `json:"timeout"`
	Hidden        bool         `json:"hidden"`
	TimeRemaining string       `json:"time_remaining"`
	Disabled      bool         `json:"disabled"`
	WindowCount   int          `json:"window_count"`
	Windows       []WindowInfo `json:"windows,omitempty"`
}

type ListData struct {
	Apps []AppInfo `json:"apps"`
}

type PauseData struct {
	Paused   bool   `json:"paused"`
	ResumeAt string `json:"resume_at,omitempty"`
}

type AXPromptData struct {
	AXTrusted bool `json:"ax_trusted"`
}

type FocusModeData struct {
	Active bool `json:"active"`
}
