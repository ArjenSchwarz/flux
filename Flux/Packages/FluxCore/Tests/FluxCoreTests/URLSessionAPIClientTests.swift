import Foundation
import Testing
@testable import FluxCore

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
    func saveNoteSendsPutRequestWithBearerAndJSONBody() async throws {
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
              "text": "off-grid afternoon",
              "updatedAt": "2026-04-15T03:30:00Z"
            }
            """
            return (response, Data(body.utf8))
        }

        let client = URLSessionAPIClient(
            baseURL: URL(string: "https://example.com")!,
            token: "abc123",
            session: session
        )
        let response = try await client.saveNote(date: "2026-04-15", text: "off-grid afternoon")

        let request = try #require(MockURLProtocol.lastRequest)
        let requestURL = try #require(request.url)
        let components = try #require(URLComponents(url: requestURL, resolvingAgainstBaseURL: false))
        #expect(request.httpMethod == "PUT")
        #expect(components.path == "/note")
        #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer abc123")
        #expect(request.value(forHTTPHeaderField: "Content-Type") == "application/json")

        let bodyData = try #require(MockURLProtocol.lastRequestBody)
        let payload = try #require(try JSONSerialization.jsonObject(with: bodyData) as? [String: String])
        #expect(payload["date"] == "2026-04-15")
        #expect(payload["text"] == "off-grid afternoon")

        #expect(response.date == "2026-04-15")
        #expect(response.text == "off-grid afternoon")
        #expect(response.updatedAt == "2026-04-15T03:30:00Z")
    }

    @Test
    func saveNoteMapsServerErrorsToFluxAPIError() async throws {
        let session = makeSession()
        let client = URLSessionAPIClient(
            baseURL: URL(string: "https://example.com")!,
            token: "token",
            session: session
        )

        MockURLProtocol.requestHandler = { request in
            let response = HTTPURLResponse(
                url: try #require(request.url),
                statusCode: 400,
                httpVersion: nil,
                headerFields: nil
            )!
            return (response, Data(#"{"error":"note must be 200 characters or fewer"}"#.utf8))
        }
        do {
            _ = try await client.saveNote(date: "2026-04-15", text: "x")
            Issue.record("Expected badRequest error")
        } catch let error as FluxAPIError {
            #expect(error == .badRequest("note must be 200 characters or fewer"))
        }

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
            _ = try await client.saveNote(date: "2026-04-15", text: "x")
            Issue.record("Expected unauthorized error")
        } catch let error as FluxAPIError {
            #expect(error == .unauthorized)
        }

        MockURLProtocol.requestHandler = { request in
            let response = HTTPURLResponse(
                url: try #require(request.url),
                statusCode: 413,
                httpVersion: nil,
                headerFields: nil
            )!
            return (response, Data())
        }
        do {
            _ = try await client.saveNote(date: "2026-04-15", text: "x")
            Issue.record("Expected unexpectedStatus 413")
        } catch let error as FluxAPIError {
            #expect(error == .unexpectedStatus(413))
        }

        MockURLProtocol.requestHandler = { request in
            let response = HTTPURLResponse(
                url: try #require(request.url),
                statusCode: 415,
                httpVersion: nil,
                headerFields: nil
            )!
            return (response, Data())
        }
        do {
            _ = try await client.saveNote(date: "2026-04-15", text: "x")
            Issue.record("Expected unexpectedStatus 415")
        } catch let error as FluxAPIError {
            #expect(error == .unexpectedStatus(415))
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
    private static var _lastRequestBody: Data?

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
            _lastRequestBody = nil
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

    static var lastRequestBody: Data? {
        lock.lock()
        defer { lock.unlock() }
        return _lastRequestBody
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
        // URLProtocol captures the body on httpBodyStream rather than httpBody;
        // drain the stream so tests can assert payload contents.
        if let stream = request.httpBodyStream {
            Self.lock.lock()
            Self._lastRequestBody = MockURLProtocol.readAll(from: stream)
            Self.lock.unlock()
        } else if let body = request.httpBody {
            Self.lock.lock()
            Self._lastRequestBody = body
            Self.lock.unlock()
        }

        do {
            let (response, data) = try handler(request)
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: data)
            client?.urlProtocolDidFinishLoading(self)
        } catch {
            client?.urlProtocol(self, didFailWithError: error)
        }
    }

    private static func readAll(from stream: InputStream) -> Data {
        stream.open()
        defer { stream.close() }
        var buffer = [UInt8](repeating: 0, count: 4096)
        var data = Data()
        while stream.hasBytesAvailable {
            let read = stream.read(&buffer, maxLength: buffer.count)
            if read <= 0 { break }
            data.append(buffer, count: read)
        }
        return data
    }

    override func stopLoading() {}
}
