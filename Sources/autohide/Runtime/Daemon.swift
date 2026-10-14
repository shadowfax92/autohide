// Daemon.swift — process entry for `autohide daemon`.
// Wires the pieces together and parks the main thread in the AppKit run loop,
// where every event, timer, and IPC request is then handled.
//
//   launchd ─▶ runDaemon ─ flock ─ Log ─ config ─ NSApp(.accessory)
//                 ├─ Engine.start      (WorkspaceEvents, first evaluate, heartbeat)
//                 ├─ IPCServer.start   (socket queue ─main.sync─▶ Engine.handle)
//                 ├─ MenuBar.install   (unless --no-menubar)
//                 └─ NSApp.run()       (never returns)
//   SIGTERM/SIGINT, menu Quit, IPC quit ─▶ stopDaemon ─▶ exit(0)

import AppKit
import AutohideCore

/// The live pieces of a running daemon, kept so every exit path shuts down the
/// same way. Main thread only.
private struct DaemonContext {
    var engine: Engine?
    var server: IPCServer?
    var menuBar: MenuBar?
    var signalSources: [DispatchSourceSignal] = []
    /// Held open for the life of the process: closing it would release the lock.
    var lockFD: Int32 = -1
}

private var context = DaemonContext()

/// flock(paths.lockFile) or exit 0 ("already running"); Log.configure; ConfigStore.loadOrCreate;
/// NSApplication.shared.setActivationPolicy(.accessory); Engine.start; IPCServer.start;
/// MenuBar.install when menubar; SIGTERM/SIGINT → clean stop; NSApp.run().
func runDaemon(paths: ConfigPaths, menubar: Bool) -> Never {
    dispatchPrecondition(condition: .onQueue(.main))

    context.lockFD = acquireInstanceLock(paths)
    Log.configure(file: paths.logFile)
    Log.info("autohide \(AutohideVersion.current) daemon starting pid=\(getpid()) "
        + "dir=\(paths.directory.path) menubar=\(menubar)")

    // Signals first, so a SIGTERM during startup is queued for a clean stop
    // instead of killing the process mid-setup. The default handlers must be
    // ignored, or the signal still terminates the process before GCD sees it.
    // SIGPIPE is ignored for good: a CLI that hangs up early must never kill us.
    signal(SIGPIPE, SIG_IGN)
    for number in [SIGTERM, SIGINT] {
        signal(number, SIG_IGN)
        let source = DispatchSource.makeSignalSource(signal: number, queue: .main)
        source.setEventHandler { stopDaemon(reason: number == SIGTERM ? "SIGTERM" : "SIGINT") }
        source.resume()
        context.signalSources.append(source)
    }

    let config: Config
    do {
        config = try ConfigStore.loadOrCreate(paths)
    } catch {
        // Refuse to guess: running on defaults would hide apps the owner exempted.
        // launchd restarts us (KeepAlive SuccessfulExit=false), so fixing the file
        // is enough to recover.
        Log.error("cannot load \(paths.configFile.path): \(error). Fix or delete it; not starting.")
        exit(EX_CONFIG)
    }

    // An agent with no Dock icon and no app menu that may own a status item.
    // Everything AppKit below runs on this, the main, thread.
    NSApplication.shared.setActivationPolicy(.accessory)

    let engine = Engine(paths: paths, config: config)
    context.engine = engine
    engine.start()

    let server = IPCServer(socket: paths.socket) { request in
        engine.handle(request)
    }
    // Runs on the IPC queue once the reply is out. `quit` exits only then, so
    // the client reads ok:true rather than a dropped connection.
    server.onResponseSent = { request in
        guard request.command == .quit else { return }
        DispatchQueue.main.async { stopDaemon(reason: "quit requested over IPC") }
    }
    do {
        try server.start()
    } catch {
        // Without IPC the CLI would report "not running" while we hide apps.
        Log.error("cannot start IPC server: \(error)")
        exit(EXIT_FAILURE)
    }
    context.server = server

    if menubar {
        let menuBar = MenuBar(control: engine)
        menuBar.install()
        context.menuBar = menuBar
    }

    Log.info("daemon running")
    NSApplication.shared.run()
    stopDaemon(reason: "run loop exited")
}

/// The one shutdown path: stop hiding, remove the socket, exit 0. Exit status 0
/// matters: launchd's KeepAlive{SuccessfulExit=false} restarts only on failure,
/// so a deliberate stop stays stopped until the next login.
func stopDaemon(reason: String) -> Never {
    dispatchPrecondition(condition: .onQueue(.main))
    Log.info("daemon stopping: \(reason)")
    context.engine?.stop()
    context.server?.stop()
    exit(0)
}

/// One daemon per config directory. The kernel drops a flock when the process
/// dies, so a crash never leaves a stale lock behind (unlike a pid file).
private func acquireInstanceLock(_ paths: ConfigPaths) -> Int32 {
    do {
        try FileManager.default.createDirectory(at: paths.directory, withIntermediateDirectories: true)
    } catch {
        Log.error("cannot create \(paths.directory.path): \(error)")
        exit(EXIT_FAILURE)
    }
    let fd = open(paths.lockFile.path, O_RDWR | O_CREAT | O_CLOEXEC, 0o600)
    guard fd >= 0 else {
        Log.error("cannot open lock \(paths.lockFile.path): \(String(cString: strerror(errno)))")
        exit(EXIT_FAILURE)
    }
    guard flock(fd, LOCK_EX | LOCK_NB) == 0 else {
        if errno == EWOULDBLOCK {
            // Exit 0 so launchd does not keep restarting a duplicate. This runs
            // before Log.configure, so the line goes to stderr (launchd.log), and
            // a second process never rotates the running daemon's log under it.
            Log.info("autohide daemon already running for \(paths.directory.path); exiting")
            exit(0)
        }
        Log.error("cannot lock \(paths.lockFile.path): \(String(cString: strerror(errno)))")
        exit(EXIT_FAILURE)
    }
    // The holder's pid, for humans; the lock itself is what counts.
    let pid = Data("\(getpid())\n".utf8)
    ftruncate(fd, 0)
    _ = pid.withUnsafeBytes { pwrite(fd, $0.baseAddress, $0.count, 0) }
    return fd
}
