import Foundation

// Helper CLI for one-shot snapshots/actions and the daemon-owned watch stream.

func fail(_ message: String) -> Never {
    FileHandle.standardError.write(Data((message + "\n").utf8))
    exit(1)
}

let args = Array(CommandLine.arguments.dropFirst())

guard let command = args.first else {
    fail("usage: autohide-helper snapshot | watch | hide <pid> | check [--prompt]")
}

switch command {
case "snapshot":
    print(makeSnapshotJSON())
case "watch":
    runWatcher()
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
