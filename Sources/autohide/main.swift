// One binary, three roles: launchd runs `autohide daemon` (agent + menu bar);
// every other invocation is a short-lived CLI command that talks to it over IPC.

import Foundation

exit(CLI.run(Array(CommandLine.arguments.dropFirst())))
