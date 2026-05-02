import Foundation

public final class URLSessionAPIClient: FluxAPIClient, Sendable {
    private let session: URLSession
    private let baseURL: URL
    private let tokenProvider: @Sendable () -> String?
    private let decoder: JSONDecoder
    private let encoder: JSONEncoder

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
        self.encoder = JSONEncoder()
    }

    public init(baseURL: URL, token: String, session: URLSession? = nil) {
        self.session = session ?? Self.noCacheSession
        self.baseURL = baseURL
        self.tokenProvider = { token }
        self.decoder = JSONDecoder()
        self.encoder = JSONEncoder()
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
        let body = try encoder.encode(NotePayload(date: date, text: text))
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
            (data, response) = try await fetch(request)
        } catch {
            throw mapTransportError(error)
        }

        return try interpret(data: data, response: response)
    }

    /// Cancellation propagates as-is so callers (refresh loops, view-driven
    /// requests) can silence it; everything else is surfaced as a network error.
    private func mapTransportError(_ error: Error) -> Error {
        if error is CancellationError { return error }
        if let urlError = error as? URLError, urlError.code == .cancelled { return urlError }
        return FluxAPIError.networkError(error.localizedDescription)
    }

    private func interpret<T: Decodable>(data: Data, response: URLResponse) throws -> T {
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

    /// On macOS, URLSession requests issued right after launch can be cancelled
    /// by the system before they go out (window/`nehelper`/URLSession warm-up
    /// race). The backoff sequence covers ~3.7 s so the user sees data on the
    /// first dashboard appear instead of waiting for the next auto-refresh
    /// iteration. `Task.sleep` propagates `CancellationError` when the
    /// surrounding task is cancelled, which short-circuits the loop without
    /// an explicit (and racy) `Task.isCancelled` probe.
    private func fetch(_ request: URLRequest) async throws -> (Data, URLResponse) {
        let backoffs: [Duration] = [
            .milliseconds(200),
            .milliseconds(500),
            .seconds(1),
            .seconds(2)
        ]
        for backoff in backoffs {
            do {
                return try await session.data(for: request)
            } catch let error as URLError where error.code == .cancelled {
                try await Task.sleep(for: backoff)
            }
        }
        return try await session.data(for: request)
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
