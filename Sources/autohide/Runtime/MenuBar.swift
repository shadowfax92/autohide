// MenuBar.swift — NSStatusItem (SF Symbol "eye.slash"; "pause.circle" while paused).
// The menu is rebuilt in menuNeedsUpdate (no timers). Items: status line; Pause 30 min /
// Pause 1 hour / Pause until resumed | Resume; Hide others now; Timeout ▸ 30s 1m 2m 5m 10m (✓ current);
// Never hide "<protected app>" (toggle); Exempt apps ▸ (✓ each, click removes); Open config;
// Quit autohide (exit 0 → launchd will not restart).
// Main thread only: AppKit status items created off main crashed the old daemon (diagnosis R2).

import AppKit
import AutohideCore

/// The daemon's only UI. It holds no state of its own: every item is built from
/// EngineControl when the menu opens, and every click calls straight back into it.
/// The icon follows pause state through `.engineStateDidChange`, not a timer.
final class MenuBar: NSObject, NSMenuDelegate {
    static let timeoutChoices: [TimeInterval] = [30, 60, 120, 300, 600]

    private let control: EngineControl
    private var statusItem: NSStatusItem?
    private var stateObserver: NSObjectProtocol?

    init(control: EngineControl) {
        self.control = control
        super.init()
    }

    deinit {
        if let stateObserver { NotificationCenter.default.removeObserver(stateObserver) }
    }

    func install() {
        dispatchPrecondition(condition: .onQueue(.main))
        guard statusItem == nil else { return }
        let item = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
        let menu = NSMenu()
        menu.autoenablesItems = false
        menu.delegate = self
        item.menu = menu
        statusItem = item
        stateObserver = NotificationCenter.default.addObserver(
            forName: .engineStateDidChange, object: nil, queue: .main) { [weak self] _ in
            self?.refreshIcon()
        }
        refreshIcon()
    }

    private func refreshIcon() {
        let paused = control.statusPayload().paused
        let image = NSImage(systemSymbolName: paused ? "pause.circle" : "eye.slash",
                            accessibilityDescription: paused ? "autohide (paused)" : "autohide")
        image?.isTemplate = true
        statusItem?.button?.image = image
        statusItem?.button?.toolTip = paused ? "autohide is paused" : "autohide"
    }

    // MARK: NSMenuDelegate

    func menuNeedsUpdate(_ menu: NSMenu) {
        menu.removeAllItems()
        let status = control.statusPayload()
        let config = control.config

        menu.addItem(disabled(statusLine(status, config)))
        menu.addItem(.separator())

        if status.paused {
            menu.addItem(action("Resume", #selector(resume)))
        } else {
            menu.addItem(action("Pause 30 min", #selector(pause(_:)), value: 1800.0))
            menu.addItem(action("Pause 1 hour", #selector(pause(_:)), value: 3600.0))
            menu.addItem(action("Pause until resumed", #selector(pause(_:))))
        }
        menu.addItem(action("Hide others now", #selector(hideOthers)))
        menu.addItem(.separator())

        menu.addItem(submenu("Timeout", timeoutItems(current: config.timeout)))
        if let app = control.protectedApp {
            let toggle = action("Never hide \u{201C}\(app.name)\u{201D}", #selector(toggleProtectedExempt(_:)),
                                value: app.name)
            toggle.state = config.isExempt(app) ? .on : .off
            menu.addItem(toggle)
        }
        menu.addItem(submenu("Exempt apps", exemptItems(config.exempt)))
        menu.addItem(.separator())

        menu.addItem(action("Open config", #selector(openConfig)))
        menu.addItem(action("Quit autohide", #selector(quit)))
        refreshIcon()
    }

    private func statusLine(_ status: IPC.StatusPayload, _ config: Config) -> String {
        if status.paused {
            guard let until = status.pausedUntil else { return "Paused until resumed" }
            return "Paused until \(DateFormatter.localizedString(from: until, dateStyle: .none, timeStyle: .short))"
        }
        if status.away { return "Away \u{2014} hiding suspended" }
        if status.fullscreenSpace { return "Fullscreen Space \u{2014} hiding suspended" }
        return "Hiding apps idle for \(DurationFormat.format(config.timeout))"
    }

    private func timeoutItems(current: TimeInterval) -> [NSMenuItem] {
        var items: [NSMenuItem] = []
        // A timeout set from the CLI or by hand may not be one of the presets;
        // show it checked so the submenu never claims nothing is selected.
        if !Self.timeoutChoices.contains(current) {
            let custom = disabled(DurationFormat.format(current))
            custom.state = .on
            items.append(custom)
        }
        for seconds in Self.timeoutChoices {
            let item = action(DurationFormat.format(seconds), #selector(setTimeout(_:)), value: seconds)
            item.state = seconds == current ? .on : .off
            items.append(item)
        }
        return items
    }

    private func exemptItems(_ exempt: [String]) -> [NSMenuItem] {
        guard !exempt.isEmpty else { return [disabled("None")] }
        return exempt.map { entry in
            let item = action(entry, #selector(removeExempt(_:)), value: entry)
            item.state = .on
            item.toolTip = "Click to let autohide hide \(entry) again"
            return item
        }
    }

    // MARK: Actions (main thread: AppKit delivers menu actions there)

    @objc private func pause(_ sender: NSMenuItem) {
        control.pause(seconds: sender.representedObject as? TimeInterval)
    }

    @objc private func resume() {
        control.resume()
    }

    @objc private func hideOthers() {
        _ = control.hideOthersNow()
    }

    @objc private func setTimeout(_ sender: NSMenuItem) {
        guard let seconds = sender.representedObject as? TimeInterval else { return }
        control.setTimeout(seconds)
    }

    @objc private func toggleProtectedExempt(_ sender: NSMenuItem) {
        guard let name = sender.representedObject as? String else { return }
        let app = control.protectedApp.flatMap { $0.name == name ? $0 : nil } ?? AppIdentity(name: name)
        if control.config.isExempt(app) {
            // The app may be exempt through its bundle ID or a differently cased
            // name, so remove every entry that matches it, not just its display name.
            for entry in control.config.exempt where AppMatch.matches(entry, app) {
                control.setExempt(entry, false)
            }
        } else {
            control.setExempt(name, true)
        }
    }

    @objc private func removeExempt(_ sender: NSMenuItem) {
        guard let entry = sender.representedObject as? String else { return }
        control.setExempt(entry, false)
    }

    @objc private func openConfig() {
        let url = URL(fileURLWithPath: control.statusPayload().configPath)
        if !NSWorkspace.shared.open(url) {
            NSWorkspace.shared.activateFileViewerSelecting([url])
        }
    }

    @objc private func quit() {
        stopDaemon(reason: "Quit from the menu bar")
    }

    // MARK: Item builders

    private func action(_ title: String, _ selector: Selector, value: Any? = nil) -> NSMenuItem {
        let item = NSMenuItem(title: title, action: selector, keyEquivalent: "")
        item.target = self
        item.representedObject = value
        return item
    }

    private func disabled(_ title: String) -> NSMenuItem {
        let item = NSMenuItem(title: title, action: nil, keyEquivalent: "")
        item.isEnabled = false
        return item
    }

    private func submenu(_ title: String, _ items: [NSMenuItem]) -> NSMenuItem {
        let item = NSMenuItem(title: title, action: nil, keyEquivalent: "")
        let menu = NSMenu(title: title)
        menu.autoenablesItems = false
        items.forEach(menu.addItem)
        item.submenu = menu
        return item
    }
}
