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
	Running      bool   `json:"running"`
	Paused       bool   `json:"paused"`
	FocusMode    bool   `json:"focus_mode"`
	Uptime       string `json:"uptime"`
	TrackedCount int    `json:"tracked_count"`
	ResumeAt     string `json:"resume_at,omitempty"`
}

type AppInfo struct {
	Name          string `json:"name"`
	LastActive    string `json:"last_active"`
	Timeout       string `json:"timeout"`
	Hidden        bool   `json:"hidden"`
	TimeRemaining string `json:"time_remaining"`
	Disabled      bool   `json:"disabled"`
}

type ListData struct {
	Apps []AppInfo `json:"apps"`
}

type PauseData struct {
	Paused   bool   `json:"paused"`
	ResumeAt string `json:"resume_at,omitempty"`
}

type OverlayStatusData struct {
	Active        bool   `json:"active"`
	Task          string `json:"task,omitempty"`
	Duration      string `json:"duration,omitempty"`
	Remaining     string `json:"remaining,omitempty"`
	Paused        bool   `json:"paused,omitempty"`
	OverlayHidden bool   `json:"overlay_hidden,omitempty"`
}

type FocusModeData struct {
	Active bool `json:"active"`
}

type WorkspaceLabelData struct {
	Workspace int    `json:"workspace"`
	Label     string `json:"label,omitempty"`
}
