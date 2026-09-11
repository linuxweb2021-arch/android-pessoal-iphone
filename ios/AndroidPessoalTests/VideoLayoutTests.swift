import XCTest
@testable import AndroidPessoal

final class VideoLayoutTests: XCTestCase {
    func testPortraitVideoFitsWideContainerWithoutStretching() {
        let result = VideoTouchContainer.aspectFit(
            size: CGSize(width: 720, height: 1280),
            inside: CGRect(x: 0, y: 0, width: 430, height: 800)
        )
        XCTAssertEqual(result.width / result.height, 720.0 / 1280.0, accuracy: 0.0001)
        XCTAssertEqual(result.height, 800, accuracy: 0.001)
        XCTAssertEqual(result.midX, 215, accuracy: 0.001)
    }
}
