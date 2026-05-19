import Foundation
import Testing
@testable import FluxCore

@MainActor @Suite(.serialized)
struct URLSessionAPIClientNotificationsTests {
    @Test
    func registerDeviceSendsPostWithBearerAndBody() async throws {
        let session = makeSession()
        NotificationsMockURLProtocol.requestHandler = { request in
            let response = HTTPURLResponse(
                url: try #require(request.url),
                statusCode: 200,
                httpVersion: nil,
                headerFields: nil
            )!
            return (response, Data(canonicalDeviceResponse().utf8))
        }

        let client = URLSessionAPIClient(baseURL: URL(string: "https://example.com")!, token: "t-1", session: session)
        let registration = DeviceRegistration(
            deviceId: "dev-1",
            platform: "ios",
            apnsToken: "deadbeef",
            tzIdentifier: "Australia/Sydney",
            tzUpdatedAt: 1_700_000_000
        )
        _ = try await client.registerDevice(registration)

        let request = try #require(NotificationsMockURLProtocol.lastRequest)
        let url = try #require(request.url)
        #expect(url.path == "/devices")
        #expect(request.httpMethod == "POST")
        #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer t-1")
        #expect(request.value(forHTTPHeaderField: "Content-Type") == "application/json")
        let body = try #require(NotificationsMockURLProtocol.lastRequestBody)
        let decoded = try JSONSerialization.jsonObject(with: body) as? [String: Any]
        #expect(decoded?["deviceId"] as? String == "dev-1")
        #expect(decoded?["platform"] as? String == "ios")
    }

    @Test
    func fetchRulesGetsListSortedByCreatedAt() async throws {
        let session = makeSession()
        NotificationsMockURLProtocol.requestHandler = { request in
            let response = HTTPURLResponse(
                url: try #require(request.url),
                statusCode: 200,
                httpVersion: nil,
                headerFields: nil
            )!
            let body = """
            {
              "rules": [
                {"id":"a","thresholdPercent":30,"windowStart":"17:00","windowEnd":"19:00","enabled":true,"createdAt":"2026-05-19T10:00:00Z","updatedAt":"2026-05-19T10:00:00Z"},
                {"id":"b","thresholdPercent":40,"windowStart":"20:00","windowEnd":"22:00","enabled":true,"createdAt":"2026-05-19T11:00:00Z","updatedAt":"2026-05-19T11:00:00Z"}
              ]
            }
            """
            return (response, Data(body.utf8))
        }

        let client = URLSessionAPIClient(baseURL: URL(string: "https://example.com")!, token: "t-1", session: session)
        let rules = try await client.fetchRules(deviceId: "dev-1")
        #expect(rules.count == 2)
        #expect(rules[0].id == "a")
        let url = try #require(NotificationsMockURLProtocol.lastRequest?.url)
        #expect(url.path == "/devices/dev-1/rules")
    }

    @Test
    func createRuleSendsPostAndReturns201() async throws {
        let session = makeSession()
        NotificationsMockURLProtocol.requestHandler = { request in
            let response = HTTPURLResponse(
                url: try #require(request.url),
                statusCode: 201,
                httpVersion: nil,
                headerFields: nil
            )!
            let body = """
            {"deviceId":"dev-1","ruleId":"new-rule","id":"new-rule","thresholdPercent":40,"windowStart":"17:00","windowEnd":"00:00","enabled":true,"createdAt":"2026-05-19T10:00:00Z","updatedAt":"2026-05-19T10:00:00Z"}
            """
            return (response, Data(body.utf8))
        }

        let client = URLSessionAPIClient(baseURL: URL(string: "https://example.com")!, token: "t-1", session: session)
        let draft = SoCAlertRuleDraft(thresholdPercent: 40, windowStart: "17:00", windowEnd: "00:00", enabled: true, label: nil)
        let created = try await client.createRule(deviceId: "dev-1", rule: draft)
        #expect(created.id == "new-rule")

        let request = try #require(NotificationsMockURLProtocol.lastRequest)
        #expect(request.httpMethod == "POST")
        #expect(request.url?.path == "/devices/dev-1/rules")
    }

    @Test
    func updateRuleSendsPutAtRulePath() async throws {
        let session = makeSession()
        NotificationsMockURLProtocol.requestHandler = { request in
            let response = HTTPURLResponse(
                url: try #require(request.url),
                statusCode: 200,
                httpVersion: nil,
                headerFields: nil
            )!
            let body = """
            {"id":"rule-1","thresholdPercent":35,"windowStart":"17:00","windowEnd":"00:00","enabled":true,"createdAt":"2026-05-19T08:00:00Z","updatedAt":"2026-05-19T10:00:00Z"}
            """
            return (response, Data(body.utf8))
        }

        let client = URLSessionAPIClient(baseURL: URL(string: "https://example.com")!, token: "t-1", session: session)
        let rule = SoCAlertRule(id: "rule-1", thresholdPercent: 35, windowStart: "17:00", windowEnd: "00:00", enabled: true, label: nil, createdAt: Date(), updatedAt: Date())
        _ = try await client.updateRule(deviceId: "dev-1", rule: rule)

        let request = try #require(NotificationsMockURLProtocol.lastRequest)
        #expect(request.httpMethod == "PUT")
        #expect(request.url?.path == "/devices/dev-1/rules/rule-1")
    }

    @Test
    func deleteRuleSendsDeleteOn204() async throws {
        let session = makeSession()
        NotificationsMockURLProtocol.requestHandler = { request in
            let response = HTTPURLResponse(
                url: try #require(request.url),
                statusCode: 204,
                httpVersion: nil,
                headerFields: nil
            )!
            return (response, Data())
        }

        let client = URLSessionAPIClient(baseURL: URL(string: "https://example.com")!, token: "t-1", session: session)
        try await client.deleteRule(deviceId: "dev-1", ruleId: "rule-1")

        let request = try #require(NotificationsMockURLProtocol.lastRequest)
        #expect(request.httpMethod == "DELETE")
        #expect(request.url?.path == "/devices/dev-1/rules/rule-1")
    }

    @Test
    func createRuleMaps409ToRuleCapReached() async throws {
        let session = makeSession()
        NotificationsMockURLProtocol.requestHandler = { request in
            let response = HTTPURLResponse(
                url: try #require(request.url),
                statusCode: 409,
                httpVersion: nil,
                headerFields: ["Content-Type": "application/json"]
            )!
            return (response, Data(#"{"error":"rule cap reached"}"#.utf8))
        }

        let client = URLSessionAPIClient(baseURL: URL(string: "https://example.com")!, token: "t-1", session: session)
        let draft = SoCAlertRuleDraft(thresholdPercent: 40, windowStart: "17:00", windowEnd: "00:00", enabled: true)
        do {
            _ = try await client.createRule(deviceId: "dev-1", rule: draft)
            Issue.record("expected createRule to throw on 409")
        } catch FluxAPIError.ruleCapReached {
            // expected
        } catch {
            Issue.record("unexpected error: \(error)")
        }
    }

    @Test
    func updateRuleMaps401ToUnauthorized() async throws {
        let session = makeSession()
        NotificationsMockURLProtocol.requestHandler = { request in
            let response = HTTPURLResponse(
                url: try #require(request.url),
                statusCode: 401,
                httpVersion: nil,
                headerFields: nil
            )!
            return (response, Data(#"{"error":"unauthorized"}"#.utf8))
        }
        let client = URLSessionAPIClient(baseURL: URL(string: "https://example.com")!, token: "t-1", session: session)
        let rule = SoCAlertRule(id: "rule-1", thresholdPercent: 30, windowStart: "17:00", windowEnd: "18:00", enabled: true, label: nil, createdAt: Date(), updatedAt: Date())
        do {
            _ = try await client.updateRule(deviceId: "dev-1", rule: rule)
            Issue.record("expected unauthorized")
        } catch FluxAPIError.unauthorized {
            // expected
        } catch {
            Issue.record("unexpected: \(error)")
        }
    }

    // MARK: - helpers

    private func canonicalDeviceResponse() -> String {
        """
        {"deviceId":"dev-1","platform":"ios","tzIdentifier":"Australia/Sydney","tzUpdatedAt":1700000000,"tokenStatus":"active"}
        """
    }

    private func makeSession() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [NotificationsMockURLProtocol.self]
        return URLSession(configuration: configuration)
    }
}

private final class NotificationsMockURLProtocol: URLProtocol {
    private static let lock = NSLock()
    private static var _requestHandler: (@Sendable (URLRequest) throws -> (HTTPURLResponse, Data))?
    private static var _lastRequest: URLRequest?
    private static var _lastRequestBody: Data?

    static var requestHandler: (@Sendable (URLRequest) throws -> (HTTPURLResponse, Data))? {
        get { lock.lock(); defer { lock.unlock() }; return _requestHandler }
        set {
            lock.lock()
            _requestHandler = newValue
            _lastRequest = nil
            _lastRequestBody = nil
            lock.unlock()
        }
    }

    static var lastRequest: URLRequest? {
        lock.lock(); defer { lock.unlock() }
        return _lastRequest
    }

    static var lastRequestBody: Data? {
        lock.lock(); defer { lock.unlock() }
        return _lastRequestBody
    }

    // swiftlint:disable:next static_over_final_class
    override class func canInit(with _: URLRequest) -> Bool { true }
    // swiftlint:disable:next static_over_final_class
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        guard let handler = Self.requestHandler else {
            client?.urlProtocol(self, didFailWithError: URLError(.badServerResponse))
            return
        }
        Self.lock.lock()
        Self._lastRequest = request
        if let stream = request.httpBodyStream {
            stream.open()
            defer { stream.close() }
            var data = Data()
            var buffer = [UInt8](repeating: 0, count: 4096)
            while stream.hasBytesAvailable {
                let read = stream.read(&buffer, maxLength: buffer.count)
                if read <= 0 { break }
                data.append(buffer, count: read)
            }
            Self._lastRequestBody = data
        } else if let body = request.httpBody {
            Self._lastRequestBody = body
        }
        Self.lock.unlock()

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
