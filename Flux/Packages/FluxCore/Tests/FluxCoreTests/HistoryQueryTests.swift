import Foundation
import Testing
@testable import FluxCore

// MARK: - HistoryQuery value semantics

@Suite struct HistoryQueryTests {
    /// Codable is required, not optional: `ChartScope` (which carries this
    /// type) is encoded for scene/expansion restoration.
    @Test
    func daysCaseRoundTripsThroughJSON() throws {
        let query = HistoryQuery.days(14)
        let data = try JSONEncoder().encode(query)
        let decoded = try JSONDecoder().decode(HistoryQuery.self, from: data)
        #expect(decoded == query)
    }

    @Test
    func dateRangeCaseRoundTripsThroughJSON() throws {
        let query = HistoryQuery.dateRange(start: "2026-05-01", end: "2026-05-31")
        let data = try JSONEncoder().encode(query)
        let decoded = try JSONDecoder().decode(HistoryQuery.self, from: data)
        #expect(decoded == query)
    }

    @Test
    func hashableDistinguishesCasesAndPayloads() {
        let queries: Set<HistoryQuery> = [
            .days(7),
            .days(14),
            .dateRange(start: "2026-05-01", end: "2026-05-31"),
            .dateRange(start: "2026-05-01", end: "2026-05-07")
        ]
        #expect(queries.count == 4)
    }
}

// MARK: - URLSessionAPIClient encoding

@MainActor @Suite(.serialized)
struct URLSessionAPIClientHistoryQueryTests {
    @Test
    func daysQueryEncodesAsDaysParameter() async throws {
        let session = makeSession()
        HistoryQueryMockURLProtocol.requestHandler = { request in
            let response = HTTPURLResponse(
                url: try #require(request.url),
                statusCode: 200,
                httpVersion: nil,
                headerFields: nil
            )!
            return (response, Data(#"{"days": []}"#.utf8))
        }

        let client = URLSessionAPIClient(baseURL: URL(string: "https://example.com")!, token: "token", session: session)
        _ = try await client.fetchHistory(query: .days(14))

        let request = try #require(HistoryQueryMockURLProtocol.lastRequest)
        let requestURL = try #require(request.url)
        let components = try #require(URLComponents(url: requestURL, resolvingAgainstBaseURL: false))
        #expect(components.path == "/history")
        #expect(components.queryItems?.contains(URLQueryItem(name: "days", value: "14")) == true)
        #expect(components.queryItems?.contains(where: { $0.name == "start" }) == false)
        #expect(components.queryItems?.contains(where: { $0.name == "end" }) == false)
    }

    @Test
    func dateRangeQueryEncodesAsStartAndEndParameters() async throws {
        let session = makeSession()
        HistoryQueryMockURLProtocol.requestHandler = { request in
            let response = HTTPURLResponse(
                url: try #require(request.url),
                statusCode: 200,
                httpVersion: nil,
                headerFields: nil
            )!
            return (response, Data(#"{"days": []}"#.utf8))
        }

        let client = URLSessionAPIClient(baseURL: URL(string: "https://example.com")!, token: "token", session: session)
        _ = try await client.fetchHistory(query: .dateRange(start: "2026-05-01", end: "2026-05-31"))

        let request = try #require(HistoryQueryMockURLProtocol.lastRequest)
        let requestURL = try #require(request.url)
        let components = try #require(URLComponents(url: requestURL, resolvingAgainstBaseURL: false))
        #expect(components.path == "/history")
        #expect(components.queryItems?.contains(URLQueryItem(name: "start", value: "2026-05-01")) == true)
        #expect(components.queryItems?.contains(URLQueryItem(name: "end", value: "2026-05-31")) == true)
        #expect(components.queryItems?.contains(where: { $0.name == "days" }) == false)
    }

    private func makeSession() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [HistoryQueryMockURLProtocol.self]
        return URLSession(configuration: configuration)
    }
}

// MARK: - Protocol-extension default

/// The default keeps the ~30 existing mocks compiling: `.days` delegates to
/// the required `fetchHistory(days:)`; `.dateRange` throws `notConfigured`
/// (the `fetchStatus(simulateLoadWatts:)` evolution pattern).
@Suite struct FluxAPIClientHistoryQueryDefaultTests {
    @Test
    func defaultDelegatesDaysCaseToRequiredMethod() async throws {
        let client = MinimalHistoryClient()
        let response = try await client.fetchHistory(query: .days(9))

        let requested = await client.requestedDays
        #expect(requested == [9])
        #expect(response.days.isEmpty)
    }

    @Test
    func defaultThrowsNotConfiguredForDateRange() async throws {
        let client = MinimalHistoryClient()
        do {
            _ = try await client.fetchHistory(query: .dateRange(start: "2026-05-01", end: "2026-05-31"))
            Issue.record("Expected notConfigured")
        } catch let error as FluxAPIError {
            #expect(error == .notConfigured)
        }
        let requested = await client.requestedDays
        #expect(requested.isEmpty)
    }
}

/// Implements only the protocol's required methods, so `fetchHistory(query:)`
/// resolves to the protocol-extension default under test.
private actor MinimalHistoryClient: FluxAPIClient {
    private(set) var requestedDays: [Int] = []

    func fetchStatus() async throws -> StatusResponse {
        throw FluxAPIError.notConfigured
    }

    func fetchHistory(days: Int) async throws -> HistoryResponse {
        requestedDays.append(days)
        return HistoryResponse(days: [])
    }

    func fetchDay(date _: String) async throws -> DayDetailResponse {
        throw FluxAPIError.notConfigured
    }

    func saveNote(date _: String, text _: String) async throws -> NoteResponse {
        throw FluxAPIError.notConfigured
    }
}

private final class HistoryQueryMockURLProtocol: URLProtocol {
    private static let lock = NSLock()
    private static var _requestHandler: (@Sendable (URLRequest) throws -> (HTTPURLResponse, Data))?
    private static var _lastRequest: URLRequest?

    static var requestHandler: (@Sendable (URLRequest) throws -> (HTTPURLResponse, Data))? {
        get {
            lock.lock()
            defer { lock.unlock() }
            return _requestHandler
        }
        set {
            lock.lock()
            _requestHandler = newValue
            _lastRequest = nil
            lock.unlock()
        }
    }

    static var lastRequest: URLRequest? {
        get {
            lock.lock()
            defer { lock.unlock() }
            return _lastRequest
        }
        set {
            lock.lock()
            _lastRequest = newValue
            lock.unlock()
        }
    }

    // swiftlint:disable:next static_over_final_class
    override class func canInit(with request: URLRequest) -> Bool {
        true
    }

    // swiftlint:disable:next static_over_final_class
    override class func canonicalRequest(for request: URLRequest) -> URLRequest {
        request
    }

    override func startLoading() {
        guard let handler = Self.requestHandler else {
            client?.urlProtocol(self, didFailWithError: URLError(.badServerResponse))
            return
        }
        Self.lastRequest = request
        do {
            let (response, data) = try handler(request)
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: data)
            client?.urlProtocolDidFinishLoading(self)
        } catch {
            client?.urlProtocol(self, didFailWithError: error)
        }
    }

    override func stopLoading() {}
}
