import Foundation

@MainActor
final class AppModel: ObservableObject {
    enum Phase: Equatable {
        case signedOut
        case connecting
        case connected
        case failed(String)
    }

    @Published var phase: Phase = .signedOut
    @Published var serverURL: String {
        didSet { UserDefaults.standard.set(serverURL, forKey: "serverURL") }
    }
    @Published var username = "vitorfulll"
    @Published var quality = StreamQuality.balanced
    @Published var muted = false
    @Published var diagnostic = "Desconectado"

    private(set) var api: APIClient?
    private(set) var webRTC: WebRTCClient?

    init() {
        serverURL = UserDefaults.standard.string(forKey: "serverURL") ?? "https://android.80-241-216-204.sslip.io"
    }

    func signIn(password: String) async {
        guard let baseURL = URL(string: serverURL), baseURL.scheme == "https" else {
            phase = .failed("Informe um endereço HTTPS válido.")
            return
        }
        phase = .connecting
        diagnostic = "Autenticando…"
        do {
            let api = APIClient(baseURL: baseURL)
            try await api.login(username: username, password: password)
            self.api = api
            try await connect()
        } catch {
            phase = .failed(error.localizedDescription)
            diagnostic = "Falha ao conectar"
        }
    }

    func connect() async throws {
        guard let api else { throw AppError.notAuthenticated }
        phase = .connecting
        diagnostic = "Criando sessão…"
        let session = try await api.createSession(quality: quality.apiValue)
        let client = WebRTCClient(api: api, session: session)
        client.onState = { [weak self] state in
            Task { @MainActor in
                self?.diagnostic = state
                if state == "Conectado" { self?.phase = .connected }
            }
        }
        client.onFailure = { [weak self] message in
            Task { @MainActor in self?.phase = .failed(message) }
        }
        webRTC = client
        try await client.connect()
    }

    func disconnect() async {
        await webRTC?.disconnect()
        await api?.logout()
        webRTC = nil
        diagnostic = "Desconectado; o Android continua ligado"
        phase = .signedOut
    }
}

enum AppError: LocalizedError {
    case notAuthenticated
    case invalidResponse
    case server(String)

    var errorDescription: String? {
        switch self {
        case .notAuthenticated: return "Sessão local ausente."
        case .invalidResponse: return "Resposta inválida do servidor."
        case .server(let message): return message
        }
    }
}

enum StreamQuality: String, CaseIterable, Identifiable {
    case economy = "Economia"
    case balanced = "Equilibrado"
    case quality = "Qualidade"
    var id: String { rawValue }
    var apiValue: String {
        switch self {
        case .economy: return "economy"
        case .balanced: return "balanced"
        case .quality: return "quality"
        }
    }
}
