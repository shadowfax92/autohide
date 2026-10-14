import XCTest
import AutohideCore

final class SkeletonTests: XCTestCase {
    func testDefaultTimeoutIsSixtySeconds() {
        XCTAssertEqual(Config.default.timeout, 60)
    }
}
