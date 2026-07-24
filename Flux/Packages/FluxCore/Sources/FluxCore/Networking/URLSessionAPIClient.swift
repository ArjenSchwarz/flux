import Foundation

public final class URLSessionAPIClient: FluxAPIClient, Sendable {
    private let session: URLSession
    private let baseURL: URL
    private let tokenProvider: @Sendable () -> String?
    let decoder: JSONDecoder  // internal: used by the pricing extension's own file
    let encoder: JSONEncoder  // internal: used by the simulation extension's own file

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
        self.decoder = Self.makeDecoder()
        self.encoder = Self.makeEncoder()
    }

    public init(baseURL: URL, token: String, session: URLSession? = nil) {
        self.session = session ?? Self.noCacheSession
        self.baseURL = baseURL
        self.tokenProvider = { token }
        self.decoder = Self.makeDecoder()
        self.encoder = Self.makeEncoder()
    }

    /// The Go backend serialises every Date as RFC 3339 (e.g.
    /// `"2026-05-19T10:00:00Z"`). The default `JSONDecoder` treats `Date`
    /// as a `TimeInterval`, which silently fails to parse those strings.
    /// Setting `.iso8601` once here so every endpoint that decodes a
    /// `Date` field (rule timestamps, future endpoints) lands on the
    /// success path.
    private static func makeDecoder() -> JSONDecoder {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return decoder
    }

    private static func makeEncoder() -> JSONEncoder {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        return encoder
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

    // MARK: - SoC Alerts

    public func registerDevice(_ registration: DeviceRegistration) async throws -> DeviceItemResponse {
        let body = try encoder.encode(registration)
        return try await performRequest(
            path: "devices",
            queryItems: [],
            method: "POST",
            body: body
        )
    }

    public func fetchRules(deviceId: String) async throws -> [SoCAlertRule] {
        let response: SoCAlertRulesResponse = try await performRequest(
            path: "devices/\(deviceId)/rules",
            queryItems: []
        )
        return response.rules
    }

    public func createRule(deviceId: String, rule: SoCAlertRuleDraft) async throws -> SoCAlertRule {
        let body = try encoder.encode(RulePayload(rule: rule))
        return try await performRequest(
            path: "devices/\(deviceId)/rules",
            queryItems: [],
            method: "POST",
            body: body
        )
    }

    public func updateRule(deviceId: String, rule: SoCAlertRule) async throws -> SoCAlertRule {
        let body = try encoder.encode(RulePayload(rule: rule))
        return try await performRequest(
            path: "devices/\(deviceId)/rules/\(rule.id)",
            queryItems: [],
            method: "PUT",
            body: body
        )
    }

    public func deleteRule(deviceId: String, ruleId: String) async throws {
        let _: EmptyResponse = try await performRequest(
            path: "devices/\(deviceId)/rules/\(ruleId)",
            queryItems: [],
            method: "DELETE"
        )
    }

    private struct RulePayload: Encodable {
        let thresholdPercent: Int
        let windowStart: String
        let windowEnd: String
        let enabled: Bool
        let label: String?

        init(rule: SoCAlertRuleDraft) {
            self.thresholdPercent = rule.thresholdPercent
            self.windowStart = rule.windowStart
            self.windowEnd = rule.windowEnd
            self.enabled = rule.enabled
            self.label = rule.label
        }

        init(rule: SoCAlertRule) {
            self.thresholdPercent = rule.thresholdPercent
            self.windowStart = rule.windowStart
            self.windowEnd = rule.windowEnd
            self.enabled = rule.enabled
            self.label = rule.label
        }
    }

    /// EmptyResponse handles 204 No Content responses where the decoder would
    /// otherwise reject an empty body.
    private struct EmptyResponse: Decodable {
        init(from _: Decoder) throws {}
    }

    func performRequest<T: Decodable>(  // internal: used by the simulation extension's file
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
        case 204:
            return try decodeResponse(emptyResponseData())
        case 200 ... 299:
            return try decodeResponse(data)
        case 400:
            throw mapBadRequest(data: data)
        case 401, 403:
            throw FluxAPIError.unauthorized
        case 404:
            throw FluxAPIError.notFound
        case 409:
            throw mapConflict(data: data)
        case 500 ... 599:
            throw FluxAPIError.serverError
        default:
            throw FluxAPIError.unexpectedStatus(httpResponse.statusCode)
        }
    }

    private func mapBadRequest(data: Data) -> FluxAPIError {
        if let reason = parsePricingValidationReason(from: data) {
            return .pricingValidation(reason)
        }
        return .badRequest(parseErrorMessage(from: data))
    }

    /// Maps an HTTP 409 response to a typed error. The only two known 409
    /// shapes today are the alerts rule-cap exceeded (legacy) and the
    /// pricing sentinel-race `concurrent_open_ended_write`. Anything else
    /// falls through to `.ruleCapReached` — if a future endpoint introduces
    /// a new 409 reason, add a branch here before adding the endpoint.
    private func mapConflict(data: Data) -> FluxAPIError {
        if let reason = parsePricingValidationReason(from: data),
           case .concurrentWrite = reason {
            return .pricingValidation(.concurrentWrite)
        }
        return .ruleCapReached
    }

    /// 204 No Content responses carry no JSON body; feed the decoder a valid
    /// empty-object literal so `EmptyResponse` can land on the success path.
    private func emptyResponseData() -> Data {
        Data("{}".utf8)
    }

    /// On macOS, URLSession requests issued right after launch can be cancelled
    /// by the system before they go out (window/`nehelper`/URLSession warm-up
    /// race). The backoff sequence covers ~3.7 s so the user sees data on the
    /// first dashboard appear instead of waiting for the next auto-refresh
    /// iteration. `Task.sleep` propagates `CancellationError` when the
    /// surrounding task is cancelled, which short-circuits the loop without
    /// an explicit (and racy) `Task.isCancelled` probe.
    ///
    /// iOS doesn't exhibit this warm-up race; on iOS, a `.cancelled` URLError
    /// is virtually always a real cancellation (view disappear, app
    /// background) that should propagate immediately rather than triggering
    /// an unwanted ~3.7 s retry.
    private func fetch(_ request: URLRequest) async throws -> (Data, URLResponse) {
        #if os(macOS)
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
        #else
        return try await session.data(for: request)
        #endif
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
