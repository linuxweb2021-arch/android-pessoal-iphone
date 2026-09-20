import AVFoundation
import Foundation
import WebRTC

struct SignalMessage: Codable {
    let type: String
    var sdp: String?
    var candidate: String?
    var sdpMid: String?
    var sdpMLineIndex: Int32?
}

final class WebRTCClient: NSObject {
    let session: RemoteSession
    var onState: ((String) -> Void)?
    var onFailure: ((String) -> Void)?

    private let api: APIClient
    private let factory: RTCPeerConnectionFactory
    private var peerConnection: RTCPeerConnection?
    private var dataChannel: RTCDataChannel?
    private var touchDataChannel: RTCDataChannel?
    private var socket: URLSessionWebSocketTask?
    private var remoteVideoTrack: RTCVideoTrack?
    private var remoteAudioTrack: RTCAudioTrack?
    private weak var renderer: RTCVideoRenderer?
    private var pendingRemoteCandidates: [RTCIceCandidate] = []
    private var remoteDescriptionReady = false
    private var interruptionObserver: NSObjectProtocol?

    init(api: APIClient, session: RemoteSession) {
        self.api = api
        self.session = session
        RTCInitializeSSL()
        factory = RTCPeerConnectionFactory(
            encoderFactory: RTCDefaultVideoEncoderFactory(),
            decoderFactory: RTCDefaultVideoDecoderFactory()
        )
        super.init()
    }

    func connect() async throws {
        configurePlaybackAudioSession()
        let request = try await api.signalRequest(sessionID: session.id)
        let socket = URLSession.shared.webSocketTask(with: request)
        self.socket = socket
        socket.resume()

        let configuration = RTCConfiguration()
        configuration.sdpSemantics = .unifiedPlan
        configuration.continualGatheringPolicy = .gatherContinually
        configuration.iceServers = [RTCIceServer(urlStrings: ["stun:stun.cloudflare.com:3478"])]
        let constraints = RTCMediaConstraints(mandatoryConstraints: nil, optionalConstraints: nil)
        guard let connection = factory.peerConnection(with: configuration, constraints: constraints, delegate: self) else {
            throw AppError.server("Não foi possível iniciar o WebRTC.")
        }
        peerConnection = connection

        let videoInit = RTCRtpTransceiverInit()
        videoInit.direction = .recvOnly
        connection.addTransceiver(of: .video, init: videoInit)
        let audioInit = RTCRtpTransceiverInit()
        audioInit.direction = .recvOnly
        connection.addTransceiver(of: .audio, init: audioInit)

        let channelConfig = RTCDataChannelConfiguration()
        channelConfig.isOrdered = true
        channelConfig.maxRetransmits = -1
        dataChannel = connection.dataChannel(forLabel: "control.v1", configuration: channelConfig)
        dataChannel?.delegate = self

        let touchChannelConfig = RTCDataChannelConfiguration()
        touchChannelConfig.isOrdered = false
        touchChannelConfig.maxRetransmits = 0
        touchDataChannel = connection.dataChannel(forLabel: "touch.v1", configuration: touchChannelConfig)
        touchDataChannel?.delegate = self

        Task { await receiveSignals() }
        onState?("Negociando mídia…")
        let rawOffer = try await createOffer(connection)
        let offer = RTCSessionDescription(
            type: rawOffer.type,
            sdp: Self.preferStereoOpus(in: rawOffer.sdp)
        )
        try await setLocalDescription(offer, on: connection)
        try await sendSignal(SignalMessage(type: "offer", sdp: offer.sdp))
    }

    func disconnect() async {
        sendCancelAll()
        touchDataChannel?.close()
        dataChannel?.close()
        peerConnection?.close()
        socket?.cancel(with: .normalClosure, reason: nil)
        if let token = interruptionObserver {
            NotificationCenter.default.removeObserver(token)
            interruptionObserver = nil
        }
        try? AVAudioSession.sharedInstance().setActive(false)
        await api.deleteSession(id: session.id)
    }

    func attach(renderer: RTCVideoRenderer) {
        if let current = self.renderer { remoteVideoTrack?.remove(current) }
        self.renderer = renderer
        remoteVideoTrack?.add(renderer)
    }

    func detach(renderer: RTCVideoRenderer) {
        remoteVideoTrack?.remove(renderer)
        self.renderer = nil
    }

    func sendControl(_ message: ControlEnvelope) {
        guard let data = try? JSONEncoder().encode(message) else { return }
        let buffer = RTCDataBuffer(data: data, isBinary: false)
        if message.action == "down" || message.action == "up" || message.action == "cancel" {
            _ = dataChannel?.sendData(buffer)
            return
        }
        if touchDataChannel?.readyState == .open {
            _ = touchDataChannel?.sendData(buffer)
        } else {
            _ = dataChannel?.sendData(buffer)
        }
    }

    func sendAndroidKey(_ key: String) {
        guard let data = try? JSONSerialization.data(withJSONObject: ["version": "control.v1", "action": "key", "key": key, "sessionId": session.id, "generation": session.generation]) else { return }
        _ = dataChannel?.sendData(RTCDataBuffer(data: data, isBinary: false))
    }

    func sendText(_ text: String) {
        guard !text.isEmpty,
              let data = try? JSONSerialization.data(withJSONObject: ["version": "control.v1", "action": "text", "text": text, "sessionId": session.id, "generation": session.generation]) else { return }
        _ = dataChannel?.sendData(RTCDataBuffer(data: data, isBinary: false))
    }

    func setMuted(_ muted: Bool) { remoteAudioTrack?.isEnabled = !muted }

    /// libwebrtc offers Opus as mono unless stereo is explicitly requested in
    /// fmtp. The Android stream already contains stereo music, so leaving the
    /// default in place makes the receiver downmix it and can sound hollow.
    static func preferStereoOpus(in sdp: String) -> String {
        let separator = sdp.contains("\r\n") ? "\r\n" : "\n"
        var lines = sdp.components(separatedBy: separator)
        let opusPayloads = Set(lines.compactMap { line -> String? in
            guard line.lowercased().hasPrefix("a=rtpmap:"),
                  let space = line.firstIndex(of: " ") else { return nil }
            let codec = line[line.index(after: space)...].lowercased()
            guard codec.hasPrefix("opus/48000/") else { return nil }
            return String(line[line.index(line.startIndex, offsetBy: 9)..<space])
        })
        guard !opusPayloads.isEmpty else { return sdp }

        let preferred = [
            "stereo": "1",
            "sprop-stereo": "1",
            "maxplaybackrate": "48000",
            "maxaveragebitrate": "192000"
        ]
        var updatedPayloads = Set<String>()
        for index in lines.indices {
            guard lines[index].lowercased().hasPrefix("a=fmtp:"),
                  let space = lines[index].firstIndex(of: " ") else { continue }
            let payload = String(lines[index][lines[index].index(lines[index].startIndex, offsetBy: 7)..<space])
            guard opusPayloads.contains(payload) else { continue }

            var parameters: [String] = lines[index][lines[index].index(after: space)...]
                .split(separator: ";", omittingEmptySubsequences: true)
                .map { $0.trimmingCharacters(in: .whitespaces) }
            parameters.removeAll { parameter in
                guard let equals = parameter.firstIndex(of: "=") else { return false }
                return preferred.keys.contains(parameter[..<equals].lowercased())
            }
            for key in ["stereo", "sprop-stereo", "maxplaybackrate", "maxaveragebitrate"] {
                parameters.append("\(key)=\(preferred[key]!)")
            }
            lines[index] = "a=fmtp:\(payload) \(parameters.joined(separator: ";"))"
            updatedPayloads.insert(payload)
        }

        for payload in opusPayloads.subtracting(updatedPayloads) {
            guard let rtpmapIndex = lines.firstIndex(where: {
                $0.lowercased().hasPrefix("a=rtpmap:\(payload.lowercased()) ")
            }) else { continue }
            lines.insert(
                "a=fmtp:\(payload) stereo=1;sprop-stereo=1;maxplaybackrate=48000;maxaveragebitrate=192000",
                at: lines.index(after: rtpmapIndex)
            )
        }
        return lines.joined(separator: separator)
    }

    /// Routes remote audio to the speaker. Uses only long-stable
    /// AVAudioSession APIs: WebRTC reconfigures the session behind our
    /// back, so plain playback category alone ends up on the earpiece.
    /// Also subscribes to interruptions (calls, alarms) so playback
    /// resumes without dropping the session.
    func configurePlaybackAudioSession() {
        let session = AVAudioSession.sharedInstance()
        try? session.setCategory(.playback, mode: .default, options: [])
        try? session.overrideOutputAudioPort(.speaker)
        try? session.setActive(true)
        if interruptionObserver == nil {
            interruptionObserver = NotificationCenter.default.addObserver(
                forName: AVAudioSession.interruptionNotification,
                object: session,
                queue: .main
            ) { [weak self] note in
                self?.handleInterruption(note)
            }
        }
    }

    private func handleInterruption(_ note: Notification) {
        guard let info = note.userInfo,
              let raw = info[AVAudioSessionInterruptionTypeKey] as? UInt,
              let type = AVAudioSession.InterruptionType(rawValue: raw) else { return }
        if case .ended = type {
            // A call or alarm paused us; resume playback, keep the session.
            configurePlaybackAudioSession()
        }
    }

    private func sendCancelAll() {
        guard let data = try? JSONSerialization.data(withJSONObject: ["version": "control.v1", "action": "cancelAll", "sessionId": session.id, "generation": session.generation]) else { return }
        _ = dataChannel?.sendData(RTCDataBuffer(data: data, isBinary: false))
    }

    private func receiveSignals() async {
        guard let socket else { return }
        while true {
            do {
                let value = try await socket.receive()
                let data: Data
                switch value {
                case .data(let received): data = received
                case .string(let text): data = Data(text.utf8)
                @unknown default: continue
                }
                let signal = try JSONDecoder().decode(SignalMessage.self, from: data)
                try await handle(signal)
            } catch {
                onFailure?("Sinalização interrompida: \(error.localizedDescription)")
                return
            }
        }
    }

    private func handle(_ signal: SignalMessage) async throws {
        guard let connection = peerConnection else { throw AppError.invalidResponse }
        switch signal.type {
        case "answer":
            guard let sdp = signal.sdp else { throw AppError.invalidResponse }
            try await setRemoteDescription(RTCSessionDescription(type: .answer, sdp: sdp), on: connection)
            remoteDescriptionReady = true
            let pending = pendingRemoteCandidates
            pendingRemoteCandidates.removeAll()
            for candidate in pending {
                try await add(candidate: candidate, to: connection)
            }
        case "ice":
            guard let candidate = signal.candidate else { throw AppError.invalidResponse }
            let value = RTCIceCandidate(sdp: candidate, sdpMLineIndex: signal.sdpMLineIndex ?? 0, sdpMid: signal.sdpMid)
            if remoteDescriptionReady {
                try await add(candidate: value, to: connection)
            } else {
                pendingRemoteCandidates.append(value)
            }
        case "error":
            throw AppError.server(signal.sdp ?? "O executor recusou a sessão.")
        default:
            break
        }
    }

    private func sendSignal(_ value: SignalMessage) async throws {
        guard let socket else { throw AppError.invalidResponse }
        let data = try JSONEncoder().encode(value)
        try await socket.send(.data(data))
    }

    private func createOffer(_ connection: RTCPeerConnection) async throws -> RTCSessionDescription {
        try await withCheckedThrowingContinuation { continuation in
            let constraints = RTCMediaConstraints(mandatoryConstraints: ["OfferToReceiveVideo": "true", "OfferToReceiveAudio": "true"], optionalConstraints: nil)
            connection.offer(for: constraints) { description, error in
                if let description { continuation.resume(returning: description) }
                else { continuation.resume(throwing: error ?? AppError.invalidResponse) }
            }
        }
    }

    private func setLocalDescription(_ description: RTCSessionDescription, on connection: RTCPeerConnection) async throws {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            connection.setLocalDescription(description) { error in
                if let error { continuation.resume(throwing: error) }
                else { continuation.resume(returning: ()) }
            }
        }
    }

    private func setRemoteDescription(_ description: RTCSessionDescription, on connection: RTCPeerConnection) async throws {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            connection.setRemoteDescription(description) { error in
                if let error { continuation.resume(throwing: error) }
                else { continuation.resume(returning: ()) }
            }
        }
    }

    private func add(candidate: RTCIceCandidate, to connection: RTCPeerConnection) async throws {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            connection.add(candidate) { error in
                if let error { continuation.resume(throwing: error) }
                else { continuation.resume(returning: ()) }
            }
        }
    }
}

extension WebRTCClient: RTCPeerConnectionDelegate {
    func peerConnection(_ peerConnection: RTCPeerConnection, didChange stateChanged: RTCSignalingState) {}
    func peerConnection(_ peerConnection: RTCPeerConnection, didAdd stream: RTCMediaStream) {}
    func peerConnection(_ peerConnection: RTCPeerConnection, didRemove stream: RTCMediaStream) {}
    func peerConnectionShouldNegotiate(_ peerConnection: RTCPeerConnection) {}
    func peerConnection(_ peerConnection: RTCPeerConnection, didChange newState: RTCIceConnectionState) {
        switch newState {
        case .connected, .completed: onState?("Conectado")
        case .disconnected: onState?("Reconectando…")
        case .failed: onFailure?("A conexão WebRTC falhou.")
        default: break
        }
    }
    func peerConnection(_ peerConnection: RTCPeerConnection, didChange newState: RTCIceGatheringState) {}
    func peerConnection(_ peerConnection: RTCPeerConnection, didGenerate candidate: RTCIceCandidate) {
        Task { try? await sendSignal(SignalMessage(type: "ice", candidate: candidate.sdp, sdpMid: candidate.sdpMid, sdpMLineIndex: candidate.sdpMLineIndex)) }
    }
    func peerConnection(_ peerConnection: RTCPeerConnection, didRemove candidates: [RTCIceCandidate]) {}
    func peerConnection(_ peerConnection: RTCPeerConnection, didOpen dataChannel: RTCDataChannel) {
        if dataChannel.label == "touch.v1" {
            self.touchDataChannel = dataChannel
        } else {
            self.dataChannel = dataChannel
        }
        dataChannel.delegate = self
    }
    func peerConnection(_ peerConnection: RTCPeerConnection, didAdd rtpReceiver: RTCRtpReceiver, streams: [RTCMediaStream]) {
        if let video = rtpReceiver.track as? RTCVideoTrack {
            remoteVideoTrack = video
            if let renderer { video.add(renderer) }
        } else if let audio = rtpReceiver.track as? RTCAudioTrack {
            remoteAudioTrack = audio
            // Re-assert playback routing once the track exists; the audio
            // device (re)configures the session when streaming starts.
            configurePlaybackAudioSession()
        }
    }
}

extension WebRTCClient: RTCDataChannelDelegate {
    func dataChannelDidChangeState(_ dataChannel: RTCDataChannel) {
        if dataChannel.readyState == .open { onState?("Conectado") }
    }
    func dataChannel(_ dataChannel: RTCDataChannel, didReceiveMessageWith buffer: RTCDataBuffer) {}
}
