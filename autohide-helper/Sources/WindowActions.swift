import AppKit

// Hides one app by pid — a whole-app hide (clean Cmd-Tab restore). Returns an
// error reason, or nil on success.
func hideApp(pid: pid_t) -> String? {
    guard let app = NSRunningApplication(processIdentifier: pid) else {
        return "no running application with pid \(pid)"
    }
    // hide() returns false when the app is already hidden (e.g. another
    // autohide tick won the race) — already-hidden is success here.
    if app.isHidden { return nil }
    if app.hide() { return nil }
    if app.isHidden { return nil }
    return "hide request for pid \(pid) was refused"
}
