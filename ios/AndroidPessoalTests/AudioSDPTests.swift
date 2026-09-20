import XCTest
@testable import AndroidPessoal

final class AudioSDPTests: XCTestCase {
    func testRequestsStereoOpusAndPreservesOtherCodecs() {
        let input = [
            "v=0",
            "m=audio 9 UDP/TLS/RTP/SAVPF 111 63",
            "a=rtpmap:111 opus/48000/2",
            "a=fmtp:111 minptime=10;useinbandfec=1;stereo=0;sprop-stereo=0",
            "a=rtpmap:63 red/48000/2",
            "a=fmtp:63 111/111"
        ].joined(separator: "\r\n") + "\r\n"

        let output = WebRTCClient.preferStereoOpus(in: input)

        XCTAssertTrue(output.contains("a=fmtp:111 minptime=10;useinbandfec=1;stereo=1;sprop-stereo=1;maxplaybackrate=48000;maxaveragebitrate=192000"))
        XCTAssertTrue(output.contains("a=fmtp:63 111/111"))
        XCTAssertFalse(output.contains("stereo=0"))
        XCTAssertTrue(output.hasSuffix("\r\n"))
    }

    func testAddsFmtpWhenOfferOmitsIt() {
        let input = "v=0\nm=audio 9 UDP/TLS/RTP/SAVPF 109\na=rtpmap:109 opus/48000/2\n"

        let output = WebRTCClient.preferStereoOpus(in: input)

        XCTAssertTrue(output.contains("a=rtpmap:109 opus/48000/2\na=fmtp:109 stereo=1;sprop-stereo=1;maxplaybackrate=48000;maxaveragebitrate=192000\n"))
    }
}
