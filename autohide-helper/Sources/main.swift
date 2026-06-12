import CoreGraphics
import Foundation

// One-shot CLI the Go daemon spawns each tick: `snapshot` reports apps +
// on-screen windows + focus as JSON; `minimize`/`hide` perform one action.
// Exit 0 with JSON/empty stdout, or exit 1 with a reason on stderr.

func fail(_ message: String) -> Never {
    FileHandle.standardError.write(Data((message + "\n").utf8))
    exit(1)
}

let args = Array(CommandLine.arguments.dropFirst())

guard let command = args.first else {
    fail("usage: autohide-helper snapshot | minimize <pid> <window-id> | hide <pid> | check [--prompt]")
}

switch command {
case "snapshot":
    print(makeSnapshotJSON())
case "minimize":
    guard args.count == 3, let pid = pid_t(args[1]), let windowID = UInt32(args[2]) else {
        fail("usage: autohide-helper minimize <pid> <window-id>")
    }
    if let err = minimizeWindow(pid: pid, windowID: CGWindowID(windowID)) {
        fail("minimize: \(err)")
    }
case "hide":
    guard args.count == 2, let pid = pid_t(args[1]) else {
        fail("usage: autohide-helper hide <pid>")
    }
    if let err = hideApp(pid: pid) {
        fail("hide: \(err)")
    }
case "check":
    print(checkJSON(prompt: args.contains("--prompt")))
default:
    fail("unknown command: \(command)")
}
