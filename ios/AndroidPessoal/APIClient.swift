import Foundation

struct TokenResponse: Codable {
    let accessToken: String
    let accessTokenExpiresAt: String
    let refreshToken: String
    let refreshTokenExpiresAt: String
}

struct RemoteSession: Codable {
    let id: String
    let username: String
    let generation: Int64
    let quality: String
    let createdAt: Date
    let expiresAt: Date
}

actor APIClient {
    let baseURL: URL
    private var accessToken: String?
    private var refreshToken: String?
    private var accessTokenExpiresAt: Date?
    private let decoder: JSONDecoder

    init(baseURL: URL) {
        self.baseURL = baseURL
        decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
    }

    func login(username: String, password: String) async throws {
        struct Input: Codable { let username: String; let password: String }
        var request = URLRequest(url: baseURL.appending(path: "/v1/login"))
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(Input(username: username, password: password))
        let (data, response) = try await URLSession.shared.data(for: request)
        try validate(response: response, data: data)
        let tokens = try decoder.decode(TokenResponse.self, from: data)
        try apply(tokens)
    }

    func createSession(quality: String) async throws -> RemoteSession {
        struct Input: Codable { let quality: String }
        var request = try await authorizedRequest(path: "/v1/sessions")
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(Input(quality: quality))
        let (data, response) = try await URLSession.shared.data(for: request)
        try validate(response: response, data: data)
        return try decoder.decode(RemoteSession.self, from: data)
    }

    func deleteSession(id: String) async {
        guard var request = try? await authorizedRequest(path: "/v1/sessions/\(id)") else { return }
        request.httpMethod = "DELETE"
        _ = try? await URLSession.shared.data(for: request)
    }

    func signalRequest(sessionID: String) async throws -> URLRequest {
        try await refreshIfNeeded()
        var components = URLComponents(url: baseURL, resolvingAgainstBaseURL: false)
        components?.scheme = baseURL.scheme == "https" ? "wss" : "ws"
        components?.path = "/v1/sessions/\(sessionID)/signal/client"
        guard let url = components?.url, let accessToken else { throw AppError.notAuthenticated }
        var request = URLRequest(url: url)
        request.setValue("Bearer \(accessToken)", forHTTPHeaderField: "Authorization")
        return request
    }

    func logout() async {
        guard let refreshToken, var request = try? await authorizedRequest(path: "/v1/logout") else { return }
        struct Input: Codable { let refreshToken: String }
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try? JSONEncoder().encode(Input(refreshToken: refreshToken))
        _ = try? await URLSession.shared.data(for: request)
        accessToken = nil
        self.refreshToken = nil
        accessTokenExpiresAt = nil
        KeychainStore.delete(account: "refresh-token")
    }

    private func authorizedRequest(path: String) async throws -> URLRequest {
        try await refreshIfNeeded()
        guard let accessToken else { throw AppError.notAuthenticated }
        var request = URLRequest(url: baseURL.appending(path: path))
        request.setValue("Bearer \(accessToken)", forHTTPHeaderField: "Authorization")
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        return request
    }

    private func refreshIfNeeded() async throws {
        guard let expiry = accessTokenExpiresAt, expiry <= Date().addingTimeInterval(60) else { return }
        guard let refreshToken else { throw AppError.notAuthenticated }
        struct Input: Codable { let refreshToken: String }
        var request = URLRequest(url: baseURL.appending(path: "/v1/refresh"))
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(Input(refreshToken: refreshToken))
        let (data, response) = try await URLSession.shared.data(for: request)
        try validate(response: response, data: data)
        try apply(decoder.decode(TokenResponse.self, from: data))
    }

    private func apply(_ tokens: TokenResponse) throws {
        guard let expiry = ISO8601DateFormatter().date(from: tokens.accessTokenExpiresAt) else {
            throw AppError.invalidResponse
        }
        accessToken = tokens.accessToken
        refreshToken = tokens.refreshToken
        accessTokenExpiresAt = expiry
        try KeychainStore.save(tokens.refreshToken, account: "refresh-token")
    }

    private func validate(response: URLResponse, data: Data) throws {
        guard let http = response as? HTTPURLResponse else { throw AppError.invalidResponse }
        guard (200..<300).contains(http.statusCode) else {
            let value = (try? JSONDecoder().decode([String: String].self, from: data)["error"])
            throw AppError.server(value ?? "O servidor respondeu com erro \(http.statusCode).")
        }
    }
}
