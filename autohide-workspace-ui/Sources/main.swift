import AppKit
import Foundation

struct PickerPayload: Decodable {
    let title: String
    let items: [PickerItem]
}

struct PickerItem: Decodable {
    let workspace: Int
    let title: String
    let subtitle: String?
    let current: Bool

    var displayText: String {
        var parts = [title]
        if let subtitle, !subtitle.isEmpty {
            parts.append(subtitle)
        }
        if current {
            parts.append("current")
        }
        return parts.joined(separator: " · ")
    }

    var searchText: String {
        displayText.lowercased()
    }
}

final class PickerWindowController: NSWindowController, NSSearchFieldDelegate, NSTableViewDataSource, NSTableViewDelegate, NSWindowDelegate {
    private let payload: PickerPayload
    private var filteredItems: [PickerItem]
    private let searchField = NSSearchField(frame: .zero)
    private let tableView = NSTableView(frame: .zero)
    private var eventMonitor: Any?
    private var shouldCancelOnClose = true

    init(payload: PickerPayload) {
        self.payload = payload
        self.filteredItems = payload.items

        let rect = NSRect(x: 0, y: 0, width: 620, height: 420)
        let style: NSWindow.StyleMask = [.titled, .closable, .fullSizeContentView]
        let window = NSWindow(contentRect: rect, styleMask: style, backing: .buffered, defer: false)
        window.title = payload.title
        window.titlebarAppearsTransparent = true
        window.isReleasedWhenClosed = false
        window.center()
        super.init(window: window)
        window.delegate = self
        buildUI()
        installKeyMonitor()
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) has not been implemented")
    }

    func windowWillClose(_ notification: Notification) {
        if shouldCancelOnClose {
            cancelAndExit()
        }
    }

    func numberOfRows(in tableView: NSTableView) -> Int {
        filteredItems.count
    }

    func tableView(_ tableView: NSTableView, viewFor tableColumn: NSTableColumn?, row: Int) -> NSView? {
        let identifier = NSUserInterfaceItemIdentifier("WorkspaceCell")
        let cell = tableView.makeView(withIdentifier: identifier, owner: self) as? NSTableCellView ?? makeCell(identifier: identifier)
        cell.textField?.stringValue = filteredItems[row].displayText
        return cell
    }

    func controlTextDidChange(_ obj: Notification) {
        reloadResults()
    }

    @objc func chooseSelectedWorkspace() {
        guard !filteredItems.isEmpty else {
            NSSound.beep()
            return
        }
        let row = selectedRow()
        shouldCancelOnClose = false
        print(filteredItems[row].workspace)
        fflush(stdout)
        NSApp.terminate(nil)
    }

    @objc func cancelAndExit() {
        exit(1)
    }

    func activate() {
        guard let window else {
            return
        }
        NSApp.activate(ignoringOtherApps: true)
        window.makeKeyAndOrderFront(nil)
        window.makeFirstResponder(searchField)
        selectRow(0)
    }

    private func buildUI() {
        guard let window else {
            return
        }

        let contentView = NSView(frame: window.contentView?.bounds ?? .zero)
        contentView.translatesAutoresizingMaskIntoConstraints = false
        window.contentView = contentView

        let headerLabel = NSTextField(labelWithString: payload.title)
        headerLabel.font = NSFont.systemFont(ofSize: 20, weight: .semibold)
        headerLabel.translatesAutoresizingMaskIntoConstraints = false

        searchField.delegate = self
        searchField.placeholderString = "Search by workspace number or label"
        searchField.translatesAutoresizingMaskIntoConstraints = false

        let scrollView = NSScrollView(frame: .zero)
        scrollView.translatesAutoresizingMaskIntoConstraints = false
        scrollView.hasVerticalScroller = true
        scrollView.borderType = .bezelBorder

        let column = NSTableColumn(identifier: NSUserInterfaceItemIdentifier("Workspace"))
        column.width = 560
        tableView.addTableColumn(column)
        tableView.headerView = nil
        tableView.rowHeight = 30
        tableView.delegate = self
        tableView.dataSource = self
        tableView.target = self
        tableView.doubleAction = #selector(chooseSelectedWorkspace)
        scrollView.documentView = tableView

        let footerLabel = NSTextField(labelWithString: "Enter to switch, Esc to cancel, arrows to move")
        footerLabel.textColor = .secondaryLabelColor
        footerLabel.translatesAutoresizingMaskIntoConstraints = false

        contentView.addSubview(headerLabel)
        contentView.addSubview(searchField)
        contentView.addSubview(scrollView)
        contentView.addSubview(footerLabel)

        NSLayoutConstraint.activate([
            headerLabel.topAnchor.constraint(equalTo: contentView.topAnchor, constant: 20),
            headerLabel.leadingAnchor.constraint(equalTo: contentView.leadingAnchor, constant: 20),
            headerLabel.trailingAnchor.constraint(equalTo: contentView.trailingAnchor, constant: -20),

            searchField.topAnchor.constraint(equalTo: headerLabel.bottomAnchor, constant: 14),
            searchField.leadingAnchor.constraint(equalTo: contentView.leadingAnchor, constant: 20),
            searchField.trailingAnchor.constraint(equalTo: contentView.trailingAnchor, constant: -20),

            scrollView.topAnchor.constraint(equalTo: searchField.bottomAnchor, constant: 14),
            scrollView.leadingAnchor.constraint(equalTo: contentView.leadingAnchor, constant: 20),
            scrollView.trailingAnchor.constraint(equalTo: contentView.trailingAnchor, constant: -20),
            scrollView.bottomAnchor.constraint(equalTo: footerLabel.topAnchor, constant: -12),

            footerLabel.leadingAnchor.constraint(equalTo: contentView.leadingAnchor, constant: 20),
            footerLabel.trailingAnchor.constraint(equalTo: contentView.trailingAnchor, constant: -20),
            footerLabel.bottomAnchor.constraint(equalTo: contentView.bottomAnchor, constant: -18)
        ])
    }

    private func installKeyMonitor() {
        eventMonitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self] event in
            self?.handle(event: event) ?? event
        }
    }

    private func handle(event: NSEvent) -> NSEvent? {
        switch event.keyCode {
        case 36:
            chooseSelectedWorkspace()
            return nil
        case 53:
            cancelAndExit()
            return nil
        case 125:
            selectRow(selectedRow() + 1)
            return nil
        case 126:
            selectRow(selectedRow() - 1)
            return nil
        default:
            return event
        }
    }

    private func reloadResults() {
        let query = searchField.stringValue.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        if query.isEmpty {
            filteredItems = payload.items
        } else {
            filteredItems = payload.items
                .compactMap { item -> (PickerItem, Int)? in
                    guard let score = fuzzyScore(query: query, candidate: item.searchText) else {
                        return nil
                    }
                    return (item, score)
                }
                .sorted { lhs, rhs in
                    if lhs.1 == rhs.1 {
                        return lhs.0.workspace < rhs.0.workspace
                    }
                    return lhs.1 > rhs.1
                }
                .map(\.0)
        }

        tableView.reloadData()
        selectRow(0)
    }

    private func selectedRow() -> Int {
        let row = tableView.selectedRow
        if row >= 0 && row < filteredItems.count {
            return row
        }
        return 0
    }

    private func selectRow(_ row: Int) {
        guard !filteredItems.isEmpty else {
            tableView.deselectAll(nil)
            return
        }
        let nextRow = max(0, min(filteredItems.count - 1, row))
        tableView.selectRowIndexes(IndexSet(integer: nextRow), byExtendingSelection: false)
        tableView.scrollRowToVisible(nextRow)
    }

    private func makeCell(identifier: NSUserInterfaceItemIdentifier) -> NSTableCellView {
        let cell = NSTableCellView(frame: .zero)
        cell.identifier = identifier

        let textField = NSTextField(labelWithString: "")
        textField.translatesAutoresizingMaskIntoConstraints = false
        cell.addSubview(textField)
        cell.textField = textField

        NSLayoutConstraint.activate([
            textField.leadingAnchor.constraint(equalTo: cell.leadingAnchor, constant: 10),
            textField.trailingAnchor.constraint(equalTo: cell.trailingAnchor, constant: -10),
            textField.centerYAnchor.constraint(equalTo: cell.centerYAnchor)
        ])

        return cell
    }

    // Favor ordered subsequence matches, with bonuses for prefix and word starts.
    private func fuzzyScore(query: String, candidate: String) -> Int? {
        let queryChars = Array(query)
        let candidateChars = Array(candidate)
        var queryIndex = 0
        var score = 0
        var streak = 0

        for index in candidateChars.indices {
            guard queryIndex < queryChars.count else {
                break
            }
            if candidateChars[index] == queryChars[queryIndex] {
                score += 10
                if queryIndex == 0 && index == 0 {
                    score += 15
                }
                if index == 0 || "-_. /·".contains(candidateChars[index - 1]) {
                    score += 8
                }
                if streak > 0 {
                    score += streak * 4
                }
                streak += 1
                queryIndex += 1
            } else {
                streak = 0
            }
        }

        guard queryIndex == queryChars.count else {
            return nil
        }

        return score - max(0, candidateChars.count - queryChars.count)
    }
}

final class AppDelegate: NSObject, NSApplicationDelegate {
    private let payload: PickerPayload
    private var controller: PickerWindowController?

    init(payload: PickerPayload) {
        self.payload = payload
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        controller = PickerWindowController(payload: payload)
        controller?.activate()
    }
}

func loadPayload() throws -> PickerPayload {
    let data = FileHandle.standardInput.readDataToEndOfFile()
    return try JSONDecoder().decode(PickerPayload.self, from: data)
}

do {
    let payload = try loadPayload()
    let app = NSApplication.shared
    app.setActivationPolicy(.accessory)
    let delegate = AppDelegate(payload: payload)
    app.delegate = delegate
    app.run()
} catch {
    fputs("workspace picker input error: \(error)\n", stderr)
    exit(2)
}
