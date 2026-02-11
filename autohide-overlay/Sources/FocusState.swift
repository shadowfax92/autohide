import Foundation

struct FocusInfo: Codable {
    let task: String
    let durationSeconds: Int
    let remainingSeconds: Int
    let paused: Bool

    enum CodingKeys: String, CodingKey {
        case task
        case durationSeconds = "duration_seconds"
        case remainingSeconds = "remaining_seconds"
        case paused
    }

    var progress: Double {
        guard durationSeconds > 0 else { return 0 }
        return 1.0 - (Double(remainingSeconds) / Double(durationSeconds))
    }

    var timeString: String {
        let h = remainingSeconds / 3600
        let m = (remainingSeconds % 3600 + 59) / 60
        if h > 0 { return "\(h)h \(m)m" }
        return "\(m)m"
    }

    static let placeholder = FocusInfo(
        task: "API docs",
        durationSeconds: 2700,
        remainingSeconds: 1421,
        paused: false
    )
}

class FocusStateReader: ObservableObject {
    @Published var info: FocusInfo?

    private var timer: Timer?
    private let path: String

    init(path: String) {
        self.path = path
        read()
        timer = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { [weak self] _ in
            self?.read()
        }
    }

    private func read() {
        guard let data = FileManager.default.contents(atPath: path) else {
            DispatchQueue.main.async { self.info = nil }
            return
        }
        if let decoded = try? JSONDecoder().decode(FocusInfo.self, from: data) {
            DispatchQueue.main.async { self.info = decoded }
        }
    }

    deinit {
        timer?.invalidate()
    }
}
