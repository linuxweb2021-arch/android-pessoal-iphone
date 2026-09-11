import UIKit

struct ControlEnvelope: Codable {
    let version = "control.v1"
    let sessionId: String
    let generation: Int64
    let screenRevision: Int64
    let screenWidth: UInt32
    let screenHeight: UInt32
    let sequence: UInt64
    let gestureId: UInt64
    let pointerId: UInt32
    let action: String
    let x: Double
    let y: Double
    let pressure: Double
    let monotonicNanos: UInt64
}

final class TouchSurfaceView: UIView {
    var videoRect: CGRect = .zero
    var sessionID = ""
    var generation: Int64 = 0
    var screenRevision: Int64 = 1
    var screenSize = CGSize(width: 720, height: 1280)
    var onControl: ((ControlEnvelope) -> Void)?

    private var pointerIDs: [ObjectIdentifier: UInt32] = [:]
    private var nextPointerID: UInt32 = 1
    private var gestureID: UInt64 = 0
    private var sequence: UInt64 = 0
    private var heartbeat: Timer?

    override init(frame: CGRect) {
        super.init(frame: frame)
        isMultipleTouchEnabled = true
        isExclusiveTouch = true
        backgroundColor = .clear
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }

    override func point(inside point: CGPoint, with event: UIEvent?) -> Bool {
        videoRect.contains(point)
    }

    override func touchesBegan(_ touches: Set<UITouch>, with event: UIEvent?) {
        if pointerIDs.isEmpty { gestureID &+= 1 }
        for touch in touches.sorted(by: stableTouchOrder) {
            let identifier = ObjectIdentifier(touch)
            let pointerID = nextPointerID
            nextPointerID &+= 1
            pointerIDs[identifier] = pointerID
            emit(touch: touch, pointerID: pointerID, action: "down")
        }
        startHeartbeat()
    }

    override func touchesMoved(_ touches: Set<UITouch>, with event: UIEvent?) {
        for touch in touches.sorted(by: stableTouchOrder) {
            guard let pointerID = pointerIDs[ObjectIdentifier(touch)] else { continue }
            emit(touch: touch, pointerID: pointerID, action: "move")
        }
    }

    override func touchesEnded(_ touches: Set<UITouch>, with event: UIEvent?) {
        finish(touches, action: "up")
    }

    override func touchesCancelled(_ touches: Set<UITouch>, with event: UIEvent?) {
        finish(touches, action: "cancel")
    }

    func cancelAll() {
        for pointerID in pointerIDs.values {
            emitNormalized(pointerID: pointerID, action: "cancel", x: 0, y: 0, pressure: 0)
        }
        pointerIDs.removeAll()
        heartbeat?.invalidate()
        heartbeat = nil
    }

    private func finish(_ touches: Set<UITouch>, action: String) {
        for touch in touches.sorted(by: stableTouchOrder) {
            let key = ObjectIdentifier(touch)
            guard let pointerID = pointerIDs.removeValue(forKey: key) else { continue }
            emit(touch: touch, pointerID: pointerID, action: action)
        }
        if pointerIDs.isEmpty {
            heartbeat?.invalidate()
            heartbeat = nil
        }
    }

    private func emit(touch: UITouch, pointerID: UInt32, action: String) {
        guard videoRect.width > 0, videoRect.height > 0 else { return }
        let point = touch.location(in: self)
        let x = min(1, max(0, (point.x - videoRect.minX) / videoRect.width))
        let y = min(1, max(0, (point.y - videoRect.minY) / videoRect.height))
        let pressure = touch.maximumPossibleForce > 0 ? min(1, max(0, touch.force / touch.maximumPossibleForce)) : 0
        emitNormalized(
            pointerID: pointerID,
            action: action,
            x: Double(x),
            y: Double(y),
            pressure: Double(pressure)
        )
    }

    private func emitNormalized(pointerID: UInt32, action: String, x: Double, y: Double, pressure: Double) {
        sequence &+= 1
        onControl?(ControlEnvelope(
            sessionId: sessionID,
            generation: generation,
            screenRevision: screenRevision,
            screenWidth: UInt32(max(1, min(8192, screenSize.width.rounded()))),
            screenHeight: UInt32(max(1, min(8192, screenSize.height.rounded()))),
            sequence: sequence,
            gestureId: gestureID,
            pointerId: pointerID,
            action: action,
            x: x,
            y: y,
            pressure: pressure,
            monotonicNanos: DispatchTime.now().uptimeNanoseconds
        ))
    }

    private func startHeartbeat() {
        guard heartbeat == nil else { return }
        heartbeat = Timer.scheduledTimer(withTimeInterval: 0.5, repeats: true) { [weak self] _ in
            guard let self, !self.pointerIDs.isEmpty else { return }
            self.emitNormalized(pointerID: 0, action: "heartbeat", x: 0, y: 0, pressure: 0)
        }
    }

    private func stableTouchOrder(_ lhs: UITouch, _ rhs: UITouch) -> Bool {
        UInt(bitPattern: ObjectIdentifier(lhs)) < UInt(bitPattern: ObjectIdentifier(rhs))
    }
}
