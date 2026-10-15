// Manages the per-user LaunchAgent that keeps the daemon running. With
// KeepAlive{SuccessfulExit=false}, a crash restarts the daemon but a menu Quit
// (exit 0) stays quit until next login.
//
// Every change goes through /bin/launchctl in the gui/<uid> domain. For tests,
// AUTOHIDE_LAUNCHD_LABEL and AUTOHIDE_LAUNCHD_PLIST point all of it at a
// throwaway job, so the real com.autohide.daemon is never touched.

import Foundation
import AutohideCore

enum LaunchAgentError: Error, CustomStringConvertible {
    case notInstalled(label: String)
    case launchctl(arguments: [String], status: Int32, output: String)

    var description: String {
        switch self {
        case .notInstalled(let label):
            return "LaunchAgent \(label) is not loaded; run `autohide install`"
        case .launchctl(let arguments, let status, let output):
            let detail = output.trimmingCharacters(in: .whitespacesAndNewlines)
            return "launchctl \(arguments.joined(separator: " ")) exited \(status)" + (detail.isEmpty ? "" : ": \(detail)")
        }
    }
}

enum LaunchAgent {
    static let label = "com.autohide.daemon"

    /// The label this process manages: `label`, unless AUTOHIDE_LAUNCHD_LABEL
    /// names a throwaway job for testing.
    static var activeLabel: String {
        environment("AUTOHIDE_LAUNCHD_LABEL") ?? label
    }

    static var plistURL: URL {          // ~/Library/LaunchAgents/com.autohide.daemon.plist
        if let override = environment("AUTOHIDE_LAUNCHD_PLIST") {
            return URL(fileURLWithPath: (override as NSString).expandingTildeInPath)
        }
        return FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/LaunchAgents/\(activeLabel).plist")
    }

    private static var domain: String { "gui/\(getuid())" }
    private static var service: String { "\(domain)/\(activeLabel)" }

    /// Writes the plist (RunAtLoad, KeepAlive{SuccessfulExit=false}, ProcessType Interactive,
    /// LimitLoadToSessionType Aqua, ThrottleInterval 5, stderr → <config dir>/launchd.log),
    /// then bootout (ignore errors) + bootstrap gui/$UID + kickstart.
    ///
    /// `configDir` bakes `--config-dir` into the job so a test instance stays
    /// isolated; `menubar: false` adds `--no-menubar`. Both default to the
    /// real install.
    static func install(executable: URL, configDir: URL? = nil, menubar: Bool = true) throws {
        let paths = configDir.map(ConfigPaths.init(directory:)) ?? ConfigPaths.standard()
        // launchd opens the log itself and will not create a missing directory.
        try FileManager.default.createDirectory(at: paths.directory, withIntermediateDirectories: true)
        let logPath = paths.directory.appendingPathComponent("launchd.log").path

        var arguments = [executable.path]
        if let configDir { arguments += ["--config-dir", configDir.path] }
        arguments.append("daemon")
        if !menubar { arguments.append("--no-menubar") }

        let job: [String: Any] = [
            "Label": activeLabel,
            "ProgramArguments": arguments,
            "RunAtLoad": true,
            "KeepAlive": ["SuccessfulExit": false],
            "ProcessType": "Interactive",
            "LimitLoadToSessionType": "Aqua",
            "ThrottleInterval": 5,
            "StandardOutPath": logPath,
            "StandardErrorPath": logPath,
        ]
        let data = try PropertyListSerialization.data(fromPropertyList: job, format: .xml, options: 0)
        try FileManager.default.createDirectory(
            at: plistURL.deletingLastPathComponent(), withIntermediateDirectories: true)
        try data.write(to: plistURL, options: .atomic)

        // A loaded job keeps the old plist in launchd's memory, so it must be
        // booted out before the new file can take effect.
        if isLoaded() {
            launchctl(["bootout", service])
            waitUntilUnloaded()
        }
        try bootstrap()
        try run(["kickstart", service])
    }

    /// Stops the job and removes its plist. Not being installed is not an error.
    static func uninstall() throws {
        if isLoaded() {
            let result = launchctl(["bootout", service])
            waitUntilUnloaded()
            if isLoaded() {
                throw LaunchAgentError.launchctl(arguments: ["bootout", service],
                                                 status: result.status, output: result.output)
            }
        }
        if FileManager.default.fileExists(atPath: plistURL.path) {
            try FileManager.default.removeItem(at: plistURL)
        }
    }

    static func restart() throws {      // launchctl kickstart -k gui/$UID/label
        guard isLoaded() else { throw LaunchAgentError.notInstalled(label: activeLabel) }
        try run(["kickstart", "-k", service])
    }

    /// Starts a loaded job that is not running (for example after a menu Quit,
    /// which KeepAlive deliberately does not undo). A no-op when it is running.
    static func start() throws {
        guard isLoaded() else { throw LaunchAgentError.notInstalled(label: activeLabel) }
        try run(["kickstart", service])
    }

    static func isLoaded() -> Bool {
        launchctl(["print", service], captureOutput: false).status == 0
    }

    // MARK: - launchctl

    /// launchd tears a booted-out job down asynchronously; bootstrapping the
    /// same label before it is gone fails with "Input/output error".
    private static func waitUntilUnloaded(timeout: TimeInterval = 5) {
        let deadline = Date().addingTimeInterval(timeout)
        while isLoaded() && Date() < deadline {
            Thread.sleep(forTimeInterval: 0.1)
        }
    }

    private static func bootstrap() throws {
        var attempt = 0
        while true {
            let result = launchctl(["bootstrap", domain, plistURL.path])
            if result.status == 0 { return }
            attempt += 1
            guard attempt < 3 else {
                throw LaunchAgentError.launchctl(arguments: ["bootstrap", domain, plistURL.path],
                                                 status: result.status, output: result.output)
            }
            Thread.sleep(forTimeInterval: 0.5)
        }
    }

    private static func run(_ arguments: [String]) throws {
        let result = launchctl(arguments)
        guard result.status == 0 else {
            throw LaunchAgentError.launchctl(arguments: arguments, status: result.status, output: result.output)
        }
    }

    /// Runs /bin/launchctl synchronously. Output is read to EOF before waiting
    /// for exit, so a chatty `print` cannot fill the pipe and deadlock us.
    @discardableResult
    private static func launchctl(_ arguments: [String], captureOutput: Bool = true) -> (status: Int32, output: String) {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/bin/launchctl")
        process.arguments = arguments
        let pipe = Pipe()
        if captureOutput {
            process.standardOutput = pipe
            process.standardError = pipe
        } else {
            process.standardOutput = FileHandle.nullDevice
            process.standardError = FileHandle.nullDevice
        }
        do {
            try process.run()
        } catch {
            return (-1, "could not run /bin/launchctl: \(error)")
        }
        let output = captureOutput ? pipe.fileHandleForReading.readDataToEndOfFile() : Data()
        process.waitUntilExit()
        return (process.terminationStatus, String(decoding: output, as: UTF8.self))
    }

    private static func environment(_ name: String) -> String? {
        guard let value = ProcessInfo.processInfo.environment[name], !value.isEmpty else { return nil }
        return value
    }
}
