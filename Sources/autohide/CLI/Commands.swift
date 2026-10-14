// Argument parsing and dispatch for every `autohide` subcommand. Runs in the
// short-lived CLI process, except `daemon`, which hands the process to runDaemon.
// Live state goes through IPCClient; config edits write config.json directly,
// then send `reload`, so they work even when the daemon is down.
//
// Exit codes: 0 success, 1 failure (daemon down, launchctl error, bad config),
// 2 usage error.

import Foundation
import AutohideCore

enum CLI {
    /// Global flag: --config-dir DIR (sets AUTOHIDE_CONFIG_DIR semantics for this call).
    /// Commands:
    ///   daemon [--no-menubar]           run the agent (launchd uses this)
    ///   status [--json]                 daemon state; "not running" + exit 1 when down
    ///   list [--json]                   per-app state and seconds until hide
    ///   pause [DURATION] | resume
    ///   hide-others
    ///   timeout [DURATION]              show / set default timeout (edits config, sends reload)
    ///   exempt [add|remove APP]         list / change exemptions
    ///   app-timeout APP DURATION|default
    ///   only [APP...] | only --clear    test allowlist
    ///   config [path|show|edit]
    ///   install | uninstall | restart   LaunchAgent management
    ///   version
    /// No args: when stdin is not a TTY (Finder launch) → ensure the LaunchAgent
    /// is installed/loaded and exit 0; when a TTY → status.
    static func run(_ args: [String]) -> Int32 {
        do {
            // Older macOS passes -psn_X_Y to apps launched from Finder.
            var args = args.filter { !$0.hasPrefix("-psn_") }
            let context = try Context(consumingGlobalFlagsFrom: &args)
            guard let command = args.first else { return try noArguments(context) }
            let rest = Array(args.dropFirst())
            if command == "help" || command == "-h" || command == "--help" || rest.contains("-h") || rest.contains("--help") {
                print(usage, terminator: "")
                return 0
            }
            return try dispatch(command, rest, context)
        } catch let error as CLIError {
            switch error {
            case .usage(let message):
                printError(message)
                FileHandle.standardError.write(Data(usage.utf8))
                return 2
            case .failure(let message):
                printError(message)
                return 1
            }
        } catch IPCClientError.notRunning(let socket) {
            printError("not running (nothing is listening on \(socket))")
            printError("start it with `autohide install`, or `autohide daemon` in the foreground")
            return 1
        } catch {
            printError(String(describing: error))
            return 1
        }
    }

    private static func dispatch(_ command: String, _ rest: [String], _ context: Context) throws -> Int32 {
        switch command {
        case "daemon":
            let flags = try parseFlags(rest, allowed: ["--no-menubar"])
            runDaemon(paths: context.paths, menubar: !flags.contains("--no-menubar"))
        case "status":
            return try status(json: parseFlags(rest, allowed: ["--json"]).contains("--json"), context)
        case "list":
            return try list(json: parseFlags(rest, allowed: ["--json"]).contains("--json"), context)
        case "pause":
            return try pause(rest, context)
        case "resume":
            try expectNoArguments(rest)
            _ = try request(.resume, context)
            print("Resumed. Every visible app starts a fresh timeout.")
            return 0
        case "hide-others":
            try expectNoArguments(rest)
            let hidden = try request(.hideOthers, context).hidden ?? 0
            print("Hid \(hidden) app\(hidden == 1 ? "" : "s").")
            return 0
        case "timeout":
            return try timeout(rest, context)
        case "exempt":
            return try exempt(rest, context)
        case "app-timeout":
            return try appTimeout(rest, context)
        case "only":
            return try only(rest, context)
        case "config":
            return try config(rest, context)
        case "install":
            let flags = try parseFlags(rest, allowed: ["--no-menubar"])
            return try install(menubar: !flags.contains("--no-menubar"), context)
        case "uninstall":
            try expectNoArguments(rest)
            return try uninstall(context)
        case "restart":
            try expectNoArguments(rest)
            try LaunchAgent.restart()
            print("Restarted \(LaunchAgent.activeLabel).")
            return 0
        case "version", "--version":
            try expectNoArguments(rest)
            print(AutohideVersion.current)
            return 0
        default:
            throw CLIError.usage("unknown command \"\(command)\"")
        }
    }

    // MARK: - No arguments

    /// A double-click in Finder runs the bundle with no arguments and stdin on
    /// /dev/null. That means "make sure it runs": install the LaunchAgent if
    /// needed, or start a loaded job that was quit from the menu. In a terminal,
    /// no arguments is just `status`.
    private static func noArguments(_ context: Context) throws -> Int32 {
        if isatty(STDIN_FILENO) != 0 {
            return try status(json: false, context)
        }
        if LaunchAgent.isLoaded() {
            try LaunchAgent.start()
        } else {
            try LaunchAgent.install(executable: currentExecutable(), configDir: context.configOverride)
        }
        return 0
    }

    // MARK: - Daemon queries

    private static func status(json: Bool, _ context: Context) throws -> Int32 {
        let response = try request(.status, context)
        if json { return printJSON(response) }
        guard let status = response.status else { throw CLIError.failure("the daemon sent no status") }

        let now = Date()
        var rows: [(String, String)] = []
        rows.append(("state", stateLine(status, now: now)))
        rows.append(("timeout", formatDuration(status.timeout)))
        rows.append(("frontmost", status.protectedApp ?? "-"))
        rows.append(("exempt", status.exempt.isEmpty ? "-" : status.exempt.joined(separator: ", ")))
        if !status.appTimeouts.isEmpty {
            let overrides = status.appTimeouts.sorted { $0.key.lowercased() < $1.key.lowercased() }
                .map { "\($0.key) \(formatDuration($0.value))" }
            rows.append(("per-app", overrides.joined(separator: ", ")))
        }
        if !status.onlyApps.isEmpty {
            rows.append(("only", status.onlyApps.joined(separator: ", ") + " (test allowlist)"))
        }
        rows.append(("access.", status.accessibility
            ? "granted (fallback for apps that refuse to hide)"
            : "not granted (optional)"))
        rows.append(("config", status.configPath))
        rows.append(("log", status.logPath))

        print("autohide \(status.version) running, pid \(status.pid), up \(formatDuration(now.timeIntervalSince(status.startedAt)))")
        if status.version != AutohideVersion.current {
            print("note: this CLI is \(AutohideVersion.current); run `autohide restart` to load it")
        }
        let width = rows.map(\.0.count).max() ?? 0
        for (label, value) in rows {
            print("  \(pad(label, width))  \(value)")
        }
        return 0
    }

    private static func stateLine(_ status: IPC.StatusPayload, now: Date) -> String {
        if status.paused {
            guard let until = status.pausedUntil else { return "paused until `autohide resume`" }
            return "paused until \(clock(until)) (\(formatDuration(until.timeIntervalSince(now))) left)"
        }
        if status.away { return "away (asleep, locked, or switched out): not hiding" }
        if status.fullscreenSpace { return "on a fullscreen Space: not hiding" }
        return "active"
    }

    private static func list(json: Bool, _ context: Context) throws -> Int32 {
        let response = try request(.list, context)
        if json { return printJSON(response) }
        let apps = response.apps ?? []
        guard !apps.isEmpty else {
            print("No regular apps are running.")
            return 0
        }

        // Next to hide first, then everything else by name.
        let sorted = apps.sorted { a, b in
            let ka = a.hidesIn ?? .infinity, kb = b.hidesIn ?? .infinity
            if ka != kb { return ka < kb }
            return displayName(a.name).lowercased() < displayName(b.name).lowercased()
        }
        let table = [["APP", "PID", "STATE", "HIDES IN", "TIMEOUT"]] + sorted.map { row in
            [displayName(row.name),
             String(row.pid),
             row.state,
             row.state == "due" ? "now" : row.hidesIn.map { formatDuration($0.rounded(.up)) } ?? "-",
             row.timeout.map(formatDuration) ?? "-"]
        }
        let widths = (0..<5).map { column in table.map { $0[column].count }.max() ?? 0 }
        for line in table {
            let cells = line.enumerated().map { pad($0.element, widths[$0.offset]) }
            print(cells.joined(separator: "  ").trimmingCharacters(in: .whitespaces))
        }
        return 0
    }

    private static func pause(_ rest: [String], _ context: Context) throws -> Int32 {
        guard rest.count <= 1 else { throw CLIError.usage("pause takes at most one DURATION") }
        let seconds = try rest.first.map { try duration($0) }
        _ = try request(.pause, seconds: seconds, context)
        if let seconds {
            print("Paused for \(formatDuration(seconds)), until \(clock(Date().addingTimeInterval(seconds))).")
        } else {
            print("Paused until `autohide resume`.")
        }
        return 0
    }

    // MARK: - Config edits

    private static func timeout(_ rest: [String], _ context: Context) throws -> Int32 {
        switch rest.count {
        case 0:
            print(formatDuration(try loadConfig(context).timeout))
        case 1:
            var config = try loadConfig(context)
            config.timeout = try duration(rest[0], minimum: Config.minimumTimeout)
            try saveAndReload(config, context, "Timeout set to \(formatDuration(config.timeout)).")
        default:
            throw CLIError.usage("timeout takes at most one DURATION")
        }
        return 0
    }

    private static func exempt(_ rest: [String], _ context: Context) throws -> Int32 {
        var config = try loadConfig(context)
        guard let action = rest.first else {
            print(config.exempt.isEmpty ? "No apps are exempt." : config.exempt.joined(separator: "\n"))
            return 0
        }
        let apps = Array(rest.dropFirst())
        guard action == "add" || action == "remove", !apps.isEmpty else {
            throw CLIError.usage("expected `exempt add APP...` or `exempt remove APP...`")
        }
        let adding = action == "add"
        var changed = false
        var report: [String] = []
        for app in apps {
            let present = config.exempt.contains { AppMatch.normalize($0) == AppMatch.normalize(app) }
            if present == adding {
                report.append(adding ? "\(app) is already exempt." : "\(app) was not exempt.")
                continue
            }
            config.setExempt(app, adding)
            changed = true
            report.append(adding ? "\(app) will never be hidden." : "\(app) can be hidden again.")
        }
        if changed {
            try saveAndReload(config, context, report.joined(separator: "\n"))
        } else {
            print(report.joined(separator: "\n"))
        }
        return 0
    }

    private static func appTimeout(_ rest: [String], _ context: Context) throws -> Int32 {
        var config = try loadConfig(context)
        switch rest.count {
        case 0:
            if config.appTimeouts.isEmpty {
                print("No per-app timeouts; every app uses \(formatDuration(config.timeout)).")
            } else {
                let rows = config.appTimeouts.sorted { $0.key.lowercased() < $1.key.lowercased() }
                let width = rows.map(\.key.count).max() ?? 0
                for (app, seconds) in rows { print("\(pad(app, width))  \(formatDuration(seconds))") }
            }
            return 0
        case 2:
            let app = rest[0]
            // One override per app however it was spelled ("slack" replaces "Slack").
            let key = AppMatch.normalize(app)
            for existing in config.appTimeouts.keys where AppMatch.normalize(existing) == key {
                config.appTimeouts[existing] = nil
            }
            if rest[1] == "default" {
                try saveAndReload(config, context, "\(app) uses the default timeout (\(formatDuration(config.timeout))).")
            } else {
                let seconds = try duration(rest[1], minimum: Config.minimumTimeout)
                config.appTimeouts[app] = seconds
                try saveAndReload(config, context, "\(app) hides after \(formatDuration(seconds)).")
            }
            return 0
        default:
            throw CLIError.usage("expected `app-timeout APP DURATION` or `app-timeout APP default`")
        }
    }

    private static func only(_ rest: [String], _ context: Context) throws -> Int32 {
        var config = try loadConfig(context)
        if rest.isEmpty {
            print(config.onlyApps.isEmpty ? "All apps can hide (no allowlist)." : config.onlyApps.joined(separator: "\n"))
            return 0
        }
        if rest == ["--clear"] {
            config.onlyApps = []
            try saveAndReload(config, context, "Allowlist cleared: all apps can hide.")
            return 0
        }
        if let flag = rest.first(where: { $0.hasPrefix("--") }) {
            throw CLIError.usage("unknown option \(flag) for only")
        }
        var seen = Set<String>()
        config.onlyApps = rest.filter { seen.insert(AppMatch.normalize($0)).inserted }
        try saveAndReload(config, context,
                          "Only these apps can hide: \(config.onlyApps.joined(separator: ", ")). Undo with `autohide only --clear`.")
        return 0
    }

    private static func config(_ rest: [String], _ context: Context) throws -> Int32 {
        let file = context.paths.configFile
        switch rest {
        case ["path"]:
            print(file.path)
        case [], ["show"]:
            _ = try loadConfig(context)
            print(try String(contentsOf: file, encoding: .utf8), terminator: "")
        case ["edit"]:
            // Create a missing file first, but do not insist on a valid one:
            // fixing a broken config.json is what `edit` is for.
            if !FileManager.default.fileExists(atPath: file.path) { _ = try loadConfig(context) }
            try runEditor(on: file)
            do {
                _ = try ConfigStore.load(file)
            } catch {
                throw CLIError.failure("\(file.path) is not valid: \(error)\nrun `autohide config edit` again to fix it")
            }
            print("Saved \(file.path).")
            notifyReload(context)
        default:
            throw CLIError.usage("expected `config`, `config path`, `config show`, or `config edit`")
        }
        return 0
    }

    // MARK: - LaunchAgent

    private static func install(menubar: Bool, _ context: Context) throws -> Int32 {
        let executable = currentExecutable()
        if LaunchAgent.activeLabel == LaunchAgent.label && !executable.path.contains(".app/Contents/MacOS/") {
            printError("warning: \(executable.path) is not inside an app bundle; `make install` installs the signed /Applications/autohide.app")
        }
        try LaunchAgent.install(executable: executable, configDir: context.configOverride, menubar: menubar)
        print("Installed LaunchAgent \(LaunchAgent.activeLabel)")
        print("  plist   \(LaunchAgent.plistURL.path)")
        print("  binary  \(executable.path)")
        print("  log     \(context.paths.directory.appendingPathComponent("launchd.log").path)")

        // launchd has started it; give it a moment to open its socket.
        let deadline = Date().addingTimeInterval(5)
        while Date() < deadline {
            if let response = try? IPCClient.send(IPC.Request(command: .status), socket: context.paths.socket, timeout: 1),
               let status = response.status {
                print("autohide \(status.version) is running (pid \(status.pid)).")
                return 0
            }
            Thread.sleep(forTimeInterval: 0.2)
        }
        printError("the daemon has not answered yet; check `autohide status` and the log above")
        return 1
    }

    private static func uninstall(_ context: Context) throws -> Int32 {
        let wasInstalled = LaunchAgent.isLoaded() || FileManager.default.fileExists(atPath: LaunchAgent.plistURL.path)
        try LaunchAgent.uninstall()
        print(wasInstalled
              ? "Removed LaunchAgent \(LaunchAgent.activeLabel)."
              : "LaunchAgent \(LaunchAgent.activeLabel) was not installed.")
        print("Config and logs are kept in \(context.paths.directory.path).")
        return 0
    }

    // MARK: - Helpers

    private static func request(_ command: IPC.Command, seconds: Double? = nil, _ context: Context) throws -> IPC.Response {
        let response = try IPCClient.send(IPC.Request(command: command, seconds: seconds), socket: context.paths.socket)
        guard response.ok else {
            throw CLIError.failure(response.error ?? "the daemon refused `\(command.rawValue)`")
        }
        return response
    }

    private static func loadConfig(_ context: Context) throws -> Config {
        do {
            return try ConfigStore.loadOrCreate(context.paths)
        } catch {
            throw CLIError.failure("cannot read \(context.paths.configFile.path): \(error)\nfix it with `autohide config edit`")
        }
    }

    /// Saves first and reports success, because the edit is complete once
    /// config.json is written; the reload only makes it take effect sooner.
    private static func saveAndReload(_ config: Config, _ context: Context, _ message: String) throws {
        try ConfigStore.save(config, to: context.paths.configFile)
        print(message)
        notifyReload(context)
    }

    /// A daemon that is down, slow, or refusing is only worth a note: it reads
    /// config.json at start, and also notices the file's mtime change on its
    /// next evaluation.
    private static func notifyReload(_ context: Context) {
        do {
            _ = try request(.reload, context)
        } catch IPCClientError.notRunning {
            print("autohide is not running; the change applies when it starts.")
        } catch CLIError.failure(let message) {
            printError("saved, but the daemon refused to reload: \(message)")
        } catch {
            printError("saved, but the daemon did not confirm the reload: \(error)")
        }
    }

    private static func runEditor(on file: URL) throws {
        let environment = ProcessInfo.processInfo.environment
        let editor = [environment["VISUAL"], environment["EDITOR"]].compactMap { $0 }.first { !$0.isEmpty } ?? "vi"
        // Through sh so an EDITOR with arguments ("code -w") works.
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/bin/sh")
        process.arguments = ["-c", "\(editor) \"$1\"", "sh", file.path]
        try process.run()
        process.waitUntilExit()
        guard process.terminationStatus == 0 else {
            throw CLIError.failure("\(editor) exited with status \(process.terminationStatus)")
        }
    }

    /// The real path of this binary, following the ~/go/bin/autohide symlink,
    /// so the LaunchAgent always points into the app bundle.
    private static func currentExecutable() -> URL {
        let path = Bundle.main.executablePath ?? CommandLine.arguments[0]
        return URL(fileURLWithPath: path).resolvingSymlinksInPath()
    }

    private static func duration(_ text: String, minimum: TimeInterval? = nil) throws -> TimeInterval {
        guard let seconds = DurationFormat.parse(text) else {
            throw CLIError.usage("invalid duration \"\(text)\" (try 30s, 5m, or 1h30m)")
        }
        if let minimum, seconds < minimum {
            throw CLIError.usage("\(text) is too short; the minimum is \(formatDuration(minimum))")
        }
        return seconds
    }

    private static func parseFlags(_ rest: [String], allowed: Set<String>) throws -> Set<String> {
        for argument in rest where !allowed.contains(argument) {
            throw CLIError.usage("unexpected argument \"\(argument)\"")
        }
        return Set(rest)
    }

    private static func expectNoArguments(_ rest: [String]) throws {
        _ = try parseFlags(rest, allowed: [])
    }

    private static func printJSON(_ response: IPC.Response) -> Int32 {
        guard let line = try? IPC.encodeLine(response) else { return 1 }
        print(String(decoding: line, as: UTF8.self), terminator: "")
        return 0
    }

    private static func formatDuration(_ seconds: TimeInterval) -> String {
        seconds < 1 ? "0s" : DurationFormat.format(seconds.rounded())
    }

    private static func clock(_ date: Date) -> String {
        let formatter = DateFormatter()
        formatter.dateFormat = Calendar.current.isDateInToday(date) ? "HH:mm" : "EEE HH:mm"
        return formatter.string(from: date)
    }

    /// App names can carry invisible format characters (U+200E); they would
    /// throw off column alignment.
    private static func displayName(_ name: String) -> String {
        String(String.UnicodeScalarView(name.unicodeScalars.filter { $0.properties.generalCategory != .format }))
    }

    private static func pad(_ text: String, _ width: Int) -> String {
        text.count >= width ? text : text + String(repeating: " ", count: width - text.count)
    }

    private static func printError(_ message: String) {
        FileHandle.standardError.write(Data("autohide: \(message)\n".utf8))
    }

    private static let usage = """
    usage: autohide [--config-dir DIR] <command> [args]

    Hides apps you have not used for a while. The frontmost app is never hidden.

    Daemon:
      status [--json]              whether it is running, and with what settings
      list [--json]                each app's state and time until it hides
      pause [DURATION]             stop hiding, for DURATION or until resume
      resume                       hide again; every visible app starts a fresh timeout
      hide-others                  hide every visible, non-exempt app except the frontmost

    Settings (edit config.json, then ask the daemon to reload):
      timeout [DURATION]           show or set the default timeout
      exempt [add|remove APP...]   list or change the apps that never hide
      app-timeout [APP DURATION|default]
                                   list or set a per-app timeout
      only [APP...] | only --clear restrict hiding to these apps (test instances)
      config [path|show|edit]

    Setup:
      install [--no-menubar]       install and start the LaunchAgent
      uninstall                    stop and remove the LaunchAgent; config is kept
      restart                      restart the daemon through launchd
      daemon [--no-menubar]        run the agent in the foreground (launchd uses this)
      version

    DURATION is 45s, 5m, 1h30m, or bare seconds.
    --config-dir DIR keeps config, socket, log, and lock in DIR (for a test instance).

    """
}

/// How a command ends early. `usage` prints the message and the usage text
/// and exits 2; `failure` prints the message and exits 1.
private enum CLIError: Error {
    case usage(String)
    case failure(String)
}

/// Where this invocation's instance lives. `--config-dir` is exported as
/// AUTOHIDE_CONFIG_DIR, so ConfigPaths.standard() and anything launched from
/// here agree on the directory.
private struct Context {
    let paths: ConfigPaths
    /// The --config-dir / AUTOHIDE_CONFIG_DIR directory; nil for the real install.
    let configOverride: URL?

    /// Removes `--config-dir DIR` / `--config-dir=DIR` from wherever it appears.
    init(consumingGlobalFlagsFrom args: inout [String]) throws {
        var remaining: [String] = []
        var directory: String?
        var index = 0
        while index < args.count {
            let argument = args[index]
            if argument == "--config-dir" {
                guard index + 1 < args.count else { throw CLIError.usage("--config-dir needs a directory") }
                directory = args[index + 1]
                index += 2
                continue
            }
            if argument.hasPrefix("--config-dir=") {
                directory = String(argument.dropFirst("--config-dir=".count))
                index += 1
                continue
            }
            remaining.append(argument)
            index += 1
        }
        args = remaining

        if let directory {
            guard !directory.isEmpty else { throw CLIError.usage("--config-dir needs a directory") }
            let absolute = URL(fileURLWithPath: (directory as NSString).expandingTildeInPath)
                .absoluteURL.standardizedFileURL.path
            setenv("AUTOHIDE_CONFIG_DIR", absolute, 1)
        }
        paths = ConfigPaths.standard()
        let overridden = !(ProcessInfo.processInfo.environment["AUTOHIDE_CONFIG_DIR"] ?? "").isEmpty
        configOverride = overridden ? paths.directory : nil
    }
}
