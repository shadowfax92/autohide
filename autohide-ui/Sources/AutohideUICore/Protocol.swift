import Foundation

// Decodable mirrors of the Go daemon's IPC payloads (autohide/ipc/protocol.go),
// snake_case bridged by IPCClient.decoder.

public struct DaemonStatus: Decodable, Equatable {
    public let running: Bool
    public let paused: Bool
    public let focusMode: Bool
    public let uptime: String
    public let trackedCount: Int
    public let resumeAt: String?
    public let windowTracking: String?
    public let axTrusted: Bool?
    public let screenRecording: Bool?
    public let defaultTimeout: String?
    public let timeoutPresets: [String]?
}

public struct WindowInfo: Decodable, Equatable {
    public let id: UInt32
    public let title: String?
    public let lastActive: String
}

public struct AppInfo: Decodable, Equatable {
    public let name: String
    public let lastActive: String
    public let timeout: String
    public let hidden: Bool
    public let timeRemaining: String
    public let disabled: Bool
    public let windowCount: Int
    public let windows: [WindowInfo]?
}

public struct AppList: Decodable, Equatable {
    public let apps: [AppInfo]
}

public struct AXPromptResult: Decodable, Equatable {
    public let axTrusted: Bool
}

public struct PauseResult: Decodable, Equatable {
    public let paused: Bool
    public let resumeAt: String?
}
