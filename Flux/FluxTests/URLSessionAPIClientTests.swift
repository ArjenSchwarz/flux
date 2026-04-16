import Foundation
import Testing
@testable import Flux

// swiftlint:disable type_body_length
@MainActor @Suite(.serialized)
struct URLSessionAPIClientTests {
    @Test
    func fetchStatusReturnsDecodedResponseOn200() async throws {
        let session = makeSession()
        MockURLProtocol.requestHandler = { request in
            let response = HTTPURLResponse(
                url: try #require(request.url),
                statusCode: 200,
                httpVersion: nil,
                headerFields: nil
            )!
            let body = """
            {
              "live": {
                "ppv": 100,
                "pload": 400,
                "pbat": -200,
                "pgrid": 300,
                "pgridSustained": false,
                "soc": 73,
                "timestamp": "2026-04-15T10:00:00Z"
              }
            }
            """
            return (response, Data(body.utf8))
        }

        let client = URLSessionAPIClient(baseURL: URL(string: "https://example.com")!, token: "token", session: session)
        let status = try await client.fetchStatus()

        #expect(status.live?.soc == 73)
    }

    @Test
    func fetchHistoryBuildsQueryParameter() async throws {
        let session = makeSession()
        MockURLProtocol.requestHandler = { request in
            let response = HTTPURLResponse(
                url: try #require(request.url),
                statusCode: 200,
                httpVersion: nil,
                headerFields: nil
            )!
            let body = """
            {
              "days": []
            }
            """
            return (response, Data(body.utf8))
        }

        let client = URLSessionAPIClient(baseURL: URL(string: "https://example.com")!, token: "token", session: session)
        _ = try await client.fetchHistory(days: 7)

        let request = try #require(MockURLProtocol.lastRequest)
        let requestURL = try #require(request.url)
        let components = try #require(URLComponents(url: requestURL, resolvingAgainstBaseURL: false))
        #expect(components.path == "/history")
        #expect(components.queryItems?.contains(URLQueryItem(name: "days", value: "7")) == true)
    }

    @Test
    func fetchDayBuildsQueryParameter() async throws {
        let session = makeSession()
        MockURLProtocol.requestHandler = { request in
            let response = HTTPURLResponse(
                url: try #require(request.url),
                statusCode: 200,
                httpVersion: nil,
                headerFields: nil
            )!
            let body = """
            {
              "date": "2026-04-15",
              "readings": [],
              "summary": null
            }
            """
            return (response, Data(body.utf8))
        }

        let client = URLSessionAPIClient(baseURL: URL(string: "https://example.com")!, token: "token", session: session)
        _ = try await client.fetchDay(date: "2026-04-15")

        let request = try #require(MockURLProtocol.lastRequest)
        let requestURL = try #require(request.url)
        let components = try #require(URLComponents(url: requestURL, resolvingAgainstBaseURL: false))
        #expect(components.path == "/day")
        #expect(components.queryItems?.contains(URLQueryItem(name: "date", value: "2026-04-15")) == true)
    }

    @Test
    func includesBearerTokenInAuthorizationHeader() async throws {
        let session = makeSession()
        MockURLProtocol.requestHandler = { request in
            let response = HTTPURLResponse(
                url: try #require(request.url),
                statusCode: 200,
                httpVersion: nil,
                headerFields: nil
            )!
            let body = """
            {
              "live": null,
              "battery": null,
              "rolling15min": null,
              "offpeak": null,
              "todayEnergy": null
            }
            """
            return (response, Data(body.utf8))
        }

        let client = URLSessionAPIClient(
            baseURL: URL(string: "https://example.com")!,
            token: "abc123",
            session: session
        )
        _ = try await client.fetchStatus()

        let request = try #require(MockURLProtocol.lastRequest)
        #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer abc123")
    }

    @Test
    // swiftlint:disable:next function_body_length
    func mapsHttpErrorsToFluxAPIErrorCases() async throws {
        let session = makeSession()

        MockURLProtocol.requestHandler = { request in
            let response = HTTPURLResponse(
                url: try #require(request.url),
                statusCode: 401,
                httpVersion: nil,
                headerFields: nil
            )!
            return (response, Data())
        }
        do {
            let client = URLSessionAPIClient(
                baseURL: URL(string: "https://example.com")!,
                token: "token",
                session: session
            )
            _ = try await client.fetchStatus()
            Issue.record("Expected unauthorized error")
        } catch let error as FluxAPIError {
            #expect(error == .unauthorized)
        }

        MockURLProtocol.requestHandler = { request in
            let response = HTTPURLResponse(
                url: try #require(request.url),
                statusCode: 400,
                httpVersion: nil,
                headerFields: nil
            )!
            let body = #"{"error":"bad input"}"#
            return (response, Data(body.utf8))
        }
        do {
            let client = URLSessionAPIClient(
                baseURL: URL(string: "https://example.com")!,
                token: "token",
                session: session
            )
            _ = try await client.fetchStatus()
            Issue.record("Expected badRequest error")
        } catch let error as FluxAPIError {
            #expect(error == .badRequest("bad input"))
        }

        MockURLProtocol.requestHandler = { request in
            let response = HTTPURLResponse(
                url: try #require(request.url),
                statusCode: 500,
                httpVersion: nil,
                headerFields: nil
            )!
            return (response, Data())
        }
        do {
            let client = URLSessionAPIClient(
                baseURL: URL(string: "https://example.com")!,
                token: "token",
                session: session
            )
            _ = try await client.fetchStatus()
            Issue.record("Expected serverError")
        } catch let error as FluxAPIError {
            #expect(error == .serverError)
        }
    }

    @Test
    func mapsNetworkAndDecodingFailures() async throws {
        let session = makeSession()

        MockURLProtocol.requestHandler = { _ in
            throw URLError(.notConnectedToInternet)
        }
        do {
            let client = URLSessionAPIClient(
                baseURL: URL(string: "https://example.com")!,
                token: "token",
                session: session
            )
            _ = try await client.fetchStatus()
            Issue.record("Expected networkError")
        } catch let error as FluxAPIError {
            guard case .networkError = error else {
                Issue.record("Expected networkError, got \(error)")
                return
            }
        }

        MockURLProtocol.requestHandler = { request in
            let response = HTTPURLResponse(
                url: try #require(request.url),
                statusCode: 200,
                httpVersion: nil,
                headerFields: nil
            )!
            return (response, Data(#"{"live":"bad"}"#.utf8))
        }
        do {
            let client = URLSessionAPIClient(
                baseURL: URL(string: "https://example.com")!,
                token: "token",
                session: session
            )
            _ = try await client.fetchStatus()
            Issue.record("Expected decodingError")
        } catch let error as FluxAPIError {
            guard case .decodingError = error else {
                Issue.record("Expected decodingError, got \(error)")
                return
            }
        }
    }

    @Test
    func missingTokenThrowsNotConfigured() async throws {
        let keychain = KeychainService(
            service: "me.nore.ig.flux.tests.\(UUID().uuidString)",
            account: "api-token",
            accessGroup: nil
        )
        let session = makeSession()
        MockURLProtocol.requestHandler = { request in
            let response = HTTPURLResponse(
                url: try #require(request.url),
                statusCode: 200,
                httpVersion: nil,
                headerFields: nil
            )!
            return (response, Data())
        }

        do {
            let client = URLSessionAPIClient(
                baseURL: URL(string: "https://example.com")!,
                keychainService: keychain,
                session: session
            )
            _ = try await client.fetchStatus()
            Issue.record("Expected notConfigured")
        } catch let error as FluxAPIError {
            #expect(error == .notConfigured)
        }
    }

    @Test
    func validationInitializerUsesExplicitToken() async throws {
        let session = makeSession()
        let keychain = KeychainService(
            service: "me.nore.ig.flux.tests.\(UUID().uuidString)",
            account: "api-token",
            accessGroup: nil
        )
        defer { try? keychain.deleteToken() }
        try keychain.saveToken("stored-token")

        MockURLProtocol.requestHandler = { request in
            let response = HTTPURLResponse(
                url: try #require(request.url),
                statusCode: 200,
                httpVersion: nil,
                headerFields: nil
            )!
            let body = """
            {
              "live": null,
              "battery": null,
              "rolling15min": null,
              "offpeak": null,
              "todayEnergy": null
            }
            """
            return (response, Data(body.utf8))
        }

        let client = URLSessionAPIClient(
            baseURL: URL(string: "https://example.com")!,
            token: "validation-token",
            session: session
        )
        _ = try await client.fetchStatus()

        let request = try #require(MockURLProtocol.lastRequest)
        #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer validation-token")
    }

    private func makeSession() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [MockURLProtocol.self]
        return URLSession(configuration: configuration)
    }
}
// swiftlint:enable type_body_length

private final class MockURLProtocol: URLProtocol {
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
