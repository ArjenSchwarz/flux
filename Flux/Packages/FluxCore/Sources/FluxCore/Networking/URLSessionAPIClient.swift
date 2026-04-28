import Foundation

public final class URLSessionAPIClient: FluxAPIClient, Sendable {
    private let session: URLSession
    private let baseURL: URL
    private let tokenProvider: @Sendable () -> String?
    private let decoder: JSONDecoder

    private static let noCacheSession: URLSession = {
        let config = URLSessionConfiguration.default
        config.requestCachePolicy = .reloadIgnoringLocalCacheData
        config.urlCache = nil
        return URLSession(configuration: config)
    }()

    public init(baseURL: URL, keychainService: KeychainService, session: URLSession? = nil) {
        self.session = session ?? Self.noCacheSession
        self.baseURL = baseURL
        self.tokenProvider = { keychainService.loadToken() }
        self.decoder = JSONDecoder()
    }

    public init(baseURL: URL, token: String, session: URLSession? = nil) {
        self.session = session ?? Self.noCacheSession
        self.baseURL = baseURL
        self.tokenProvider = { token }
        self.decoder = JSONDecoder()
    }

    public func fetchStatus() async throws -> StatusResponse {
        try await performRequest(path: "status", queryItems: [])
    }

    public func fetchHistory(days: Int) async throws -> HistoryResponse {
        try await performRequest(
            path: "history",
            queryItems: [URLQueryItem(name: "days", value: String(days))]
        )
    }

    public func fetchDay(date: String) async throws -> DayDetailResponse {
        try await performRequest(
            path: "day",
            queryItems: [URLQueryItem(name: "date", value: date)]
        )
    }

    public func saveNote(date: String, text: String) async throws -> NoteResponse {
        let body = try JSONEncoder().encode(NotePayload(date: date, text: text))
        return try await performRequest(
            path: "note",
            queryItems: [],
            method: "PUT",
            body: body
        )
    }

    private struct NotePayload: Encodable {
        let date: String
        let text: String
    }

    private func performRequest<T: Decodable>(
        path: String,
        queryItems: [URLQueryItem],
        method: String = "GET",
        body: Data? = nil
    ) async throws -> T {
        guard let token = tokenProvider(), token.isEmpty == false else {
            throw FluxAPIError.notConfigured
        }

        var components = URLComponents(url: baseURL.appendingPathComponent(path), resolvingAgainstBaseURL: false)
        if queryItems.isEmpty == false {
            components?.queryItems = queryItems
        }

        guard let url = components?.url else {
            throw FluxAPIError.badRequest("Invalid URL")
        }

        var request = URLRequest(url: url)
        request.httpMethod = method
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        if let body {
            request.httpBody = body
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }

        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await session.data(for: request)
        } catch {
            throw FluxAPIError.networkError(error.localizedDescription)
        }

        guard let httpResponse = response as? HTTPURLResponse else {
            throw FluxAPIError.networkError("Invalid HTTP response")
        }

        switch httpResponse.statusCode {
        case 200 ... 299:
            return try decodeResponse(data)
        case 400:
            throw FluxAPIError.badRequest(parseErrorMessage(from: data))
        case 401, 403:
            throw FluxAPIError.unauthorized
        case 500 ... 599:
            throw FluxAPIError.serverError
        default:
            throw FluxAPIError.unexpectedStatus(httpResponse.statusCode)
        }
    }

    private func decodeResponse<T: Decodable>(_ data: Data) throws -> T {
        do {
            return try decoder.decode(T.self, from: data)
        } catch {
            throw FluxAPIError.decodingError(error.localizedDescription)
        }
    }

    private func parseErrorMessage(from data: Data) -> String {
        guard let response = try? decoder.decode(APIErrorResponse.self, from: data) else {
            return "Bad request"
        }
        return response.error
    }
}
