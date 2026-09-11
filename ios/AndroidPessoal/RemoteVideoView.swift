import SwiftUI
import UIKit
import WebRTC

struct RemoteVideoView: UIViewRepresentable {
    let client: WebRTCClient

    func makeUIView(context: Context) -> VideoTouchContainer {
        let view = VideoTouchContainer()
        view.onControl = { client.sendControl($0) }
        view.sessionID = client.session.id
        view.generation = client.session.generation
        view.client = client
        client.attach(renderer: view)
        return view
    }

    func updateUIView(_ uiView: VideoTouchContainer, context: Context) {}

    static func dismantleUIView(_ uiView: VideoTouchContainer, coordinator: ()) {
        uiView.cancelAllTouches()
        uiView.client?.detach(renderer: uiView)
    }
}

final class VideoTouchContainer: UIView, RTCVideoRenderer {
    private let videoView = RTCMTLVideoView(frame: .zero)
    private let touchView = TouchSurfaceView(frame: .zero)
    private var videoSize = CGSize(width: 720, height: 1280)
    weak var client: WebRTCClient?
    var onControl: ((ControlEnvelope) -> Void)? { didSet { touchView.onControl = onControl } }
    var sessionID = "" { didSet { touchView.sessionID = sessionID } }
    var generation: Int64 = 0 { didSet { touchView.generation = generation } }

    override init(frame: CGRect) {
        super.init(frame: frame)
        backgroundColor = .black
        videoView.videoContentMode = .scaleAspectFit
        addSubview(videoView)
        addSubview(touchView)
        isMultipleTouchEnabled = true
        touchView.isMultipleTouchEnabled = true
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }

    override func layoutSubviews() {
        super.layoutSubviews()
        videoView.frame = bounds
        touchView.frame = bounds
        touchView.videoRect = Self.aspectFit(size: videoSize, inside: bounds)
    }

    func setSize(_ size: CGSize) {
        guard size.width > 0, size.height > 0 else { return }
        guard size != videoSize else {
            videoView.setSize(size)
            return
        }
        touchView.cancelAll()
        videoSize = size
        touchView.screenSize = size
        touchView.screenRevision += 1
        setNeedsLayout()
        videoView.setSize(size)
    }

    func renderFrame(_ frame: RTCVideoFrame?) { videoView.renderFrame(frame) }
    func cancelAllTouches() { touchView.cancelAll() }

    static func aspectFit(size: CGSize, inside bounds: CGRect) -> CGRect {
        guard size.width > 0, size.height > 0, bounds.width > 0, bounds.height > 0 else { return .zero }
        let scale = min(bounds.width / size.width, bounds.height / size.height)
        let fitted = CGSize(width: size.width * scale, height: size.height * scale)
        return CGRect(x: bounds.midX - fitted.width / 2, y: bounds.midY - fitted.height / 2, width: fitted.width, height: fitted.height)
    }
}
