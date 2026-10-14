// MenuBar.swift — NSStatusItem (SF Symbol "eye.slash"; "pause.circle" while paused).
// The menu is rebuilt in menuNeedsUpdate (no timers). Items: status line; Pause 30 min /
// Pause 1 hour / Pause until resumed | Resume; Hide others now; Timeout ▸ 30s 1m 2m 5m 10m (✓ current);
// Never hide "<protected app>" (toggle); Exempt apps ▸ (✓ each, click removes); Open config;
// Quit autohide (exit 0 → launchd will not restart).
// Main thread only: AppKit status items created off main crashed the old daemon (diagnosis R2).

import AppKit
import AutohideCore

final class MenuBar: NSObject, NSMenuDelegate {
    private let control: EngineControl

    init(control: EngineControl) {
        self.control = control
        super.init()
    }

    func install() {
        preconditionFailure("unimplemented")
    }
}
