import Foundation
import Testing
@testable import FluxCore

// swiftlint:disable file_length type_body_length
@MainActor @Suite(.serialized)
struct URLSessionAPIClientSimulationTests {
    // MARK: - Simulated status request

    @Test
    func fetchStatusWithSimulateLoadAddsQueryItem() async throws {
        let session = makeSession()
        SimulationMockURLProtocol.requestHandler = { request in
            let url = try #require(request.url)
            let response = HTTPURLResponse(url: url, statusCode: 200, httpVersion: nil, headerFields: nil)!
            return (response, Data(Self.statusBody.utf8))
        }
        let client = makeClient(session: session)
        _ = try await client.fetchStatus(simulateLoadWatts: 1700)

        let request = try #require(SimulationMockURLProtocol.lastRequest)
        let requestURL = try #require(request.url)
        let components = try #require(URLComponents(url: requestURL, resolvingAgainstBaseURL: false))
        #expect(components.path == "/status")
        #expect(request.httpMethod == "GET")
        let item = components.queryItems?.first { $0.name == "simulateLoadWatts" }
        #expect(item?.value == "1700")
    }

    @Test
    func fetchStatusWithoutSimulateLoadStaysParamFree() async throws {
        let session = makeSession()
        SimulationMockURLProtocol.requestHandler = { request in
            let url = try #require(request.url)
            let response = HTTPURLResponse(url: url, statusCode: 200, httpVersion: nil, headerFields: nil)!
            return (response, Data(Self.statusBody.utf8))
        }
        let client = makeClient(session: session)
        _ = try await client.fetchStatus()

        let request = try #require(SimulationMockURLProtocol.lastRequest)
        let requestURL = try #require(request.url)
        let components = try #require(URLComponents(url: requestURL, resolvingAgainstBaseURL: false))
        #expect(components.path == "/status")
        // The unsimulated call must carry no query items at all.
        #expect(components.queryItems == nil)
    }

    @Test
    func fetchStatusWithSimulateLoadDecodesStatus() async throws {
        let session = makeSession()
        SimulationMockURLProtocol.requestHandler = { request in
            let url = try #require(request.url)
            let response = HTTPURLResponse(url: url, statusCode: 200, httpVersion: nil, headerFields: nil)!
            return (response, Data(Self.statusBody.utf8))
        }
        let client = makeClient(session: session)
        let status = try await client.fetchStatus(simulateLoadWatts: 1700)
        #expect(status.live?.soc == 62)
    }

    // MARK: - Preset list

    @Test
    func fetchPresetsDecodesArray() async throws {
        let session = makeSession()
        SimulationMockURLProtocol.requestHandler = { request in
            let url = try #require(request.url)
            let response = HTTPURLResponse(url: url, statusCode: 200, httpVersion: nil, headerFields: nil)!
            let body = """
            {
              "presets": [
                {"id": "p1", "label": "Charge car", "watts": 1700,
                 "createdAt": "2026-06-09T08:00:00Z", "updatedAt": "2026-06-09T08:00:00Z"},
                {"id": "p2", "label": "Heat pump", "watts": 3200,
                 "createdAt": "2026-06-09T09:00:00Z", "updatedAt": "2026-06-09T09:00:00Z"}
              ]
            }
            """
            return (response, Data(body.utf8))
        }
        let client = makeClient(session: session)
        let presets = try await client.fetchPresets()

        #expect(presets.count == 2)
        #expect(presets[0].id == "p1")
        #expect(presets[1].watts == 3200)
        let request = try #require(SimulationMockURLProtocol.lastRequest)
        let requestURL = try #require(request.url)
        let components = try #require(URLComponents(url: requestURL, resolvingAgainstBaseURL: false))
        #expect(components.path == "/simulation-presets")
        #expect(request.httpMethod == "GET")
        #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer token")
    }

    // MARK: - Create

    @Test
    func createPresetPostsDraftAndDecodesResponse() async throws {
        let session = makeSession()
        SimulationMockURLProtocol.requestHandler = { request in
            let url = try #require(request.url)
            let response = HTTPURLResponse(url: url, statusCode: 201, httpVersion: nil, headerFields: nil)!
            let body = """
            {"id": "p-new", "label": "Charge car", "watts": 1700,
             "createdAt": "2026-06-09T08:00:00Z", "updatedAt": "2026-06-09T08:00:00Z"}
            """
            return (response, Data(body.utf8))
        }
        let client = makeClient(session: session)
        let created = try await client.createPreset(SimulationPresetDraft(label: "Charge car", watts: 1700))

        #expect(created.id == "p-new")
        #expect(created.watts == 1700)
        let request = try #require(SimulationMockURLProtocol.lastRequest)
        #expect(request.httpMethod == "POST")
        let requestURL = try #require(request.url)
        let components = try #require(URLComponents(url: requestURL, resolvingAgainstBaseURL: false))
        #expect(components.path == "/simulation-presets")
        #expect(request.value(forHTTPHeaderField: "Content-Type") == "application/json")
        let bodyData = try #require(SimulationMockURLProtocol.lastRequestBody)
        let json = try #require(try JSONSerialization.jsonObject(with: bodyData) as? [String: Any])
        #expect(json["label"] as? String == "Charge car")
        #expect((json["watts"] as? NSNumber)?.intValue == 1700)
    }

    @Test
    func createPresetMaps409ToRuleCapReached() async throws {
        let session = makeSession()
        SimulationMockURLProtocol.requestHandler = { request in
            let url = try #require(request.url)
            let response = HTTPURLResponse(url: url, statusCode: 409, httpVersion: nil, headerFields: nil)!
            return (response, Data("{\"error\":\"preset cap reached\"}".utf8))
        }
        let client = makeClient(session: session)
        do {
            _ = try await client.createPreset(SimulationPresetDraft(label: "x", watts: 100))
            Issue.record("expected cap error")
        } catch let error as FluxAPIError {
            #expect(error == .ruleCapReached)
        }
    }

    @Test
    func createPresetMaps400ToBadRequest() async throws {
        let session = makeSession()
        SimulationMockURLProtocol.requestHandler = { request in
            let url = try #require(request.url)
            let response = HTTPURLResponse(url: url, statusCode: 400, httpVersion: nil, headerFields: nil)!
            return (response, Data("{\"error\":\"label must not be empty\"}".utf8))
        }
        let client = makeClient(session: session)
        do {
            _ = try await client.createPreset(SimulationPresetDraft(label: "", watts: 100))
            Issue.record("expected bad request")
        } catch let error as FluxAPIError {
            #expect(error == .badRequest("label must not be empty"))
        }
    }

    // MARK: - Update

    @Test
    func updatePresetPutsDraftAndDecodesResponse() async throws {
        let session = makeSession()
        SimulationMockURLProtocol.requestHandler = { request in
            let url = try #require(request.url)
            let response = HTTPURLResponse(url: url, statusCode: 200, httpVersion: nil, headerFields: nil)!
            let body = """
            {"id": "p1", "label": "Charge car fast", "watts": 7000,
             "createdAt": "2026-06-09T08:00:00Z", "updatedAt": "2026-06-09T10:00:00Z"}
            """
            return (response, Data(body.utf8))
        }
        let preset = SimulationPreset(
            id: "p1", label: "Charge car fast", watts: 7000,
            createdAt: Date(timeIntervalSince1970: 1), updatedAt: Date(timeIntervalSince1970: 1)
        )
        let client = makeClient(session: session)
        let updated = try await client.updatePreset(preset)
        #expect(updated.id == "p1")
        #expect(updated.watts == 7000)
        let request = try #require(SimulationMockURLProtocol.lastRequest)
        #expect(request.httpMethod == "PUT")
        let requestURL = try #require(request.url)
        let components = try #require(URLComponents(url: requestURL, resolvingAgainstBaseURL: false))
        #expect(components.path == "/simulation-presets/p1")
        let bodyData = try #require(SimulationMockURLProtocol.lastRequestBody)
        let json = try #require(try JSONSerialization.jsonObject(with: bodyData) as? [String: Any])
        #expect(json["label"] as? String == "Charge car fast")
        #expect((json["watts"] as? NSNumber)?.intValue == 7000)
    }

    @Test
    func updatePresetMaps404ToNotFound() async throws {
        let session = makeSession()
        SimulationMockURLProtocol.requestHandler = { request in
            let url = try #require(request.url)
            let response = HTTPURLResponse(url: url, statusCode: 404, httpVersion: nil, headerFields: nil)!
            return (response, Data())
        }
        let preset = SimulationPreset(
            id: "missing", label: "x", watts: 100,
            createdAt: Date(timeIntervalSince1970: 1), updatedAt: Date(timeIntervalSince1970: 1)
        )
        let client = makeClient(session: session)
        do {
            _ = try await client.updatePreset(preset)
            Issue.record("expected notFound")
        } catch let error as FluxAPIError {
            #expect(error == .notFound)
        }
    }

    // MARK: - Delete

    @Test
    func deletePresetHits204AndReturns() async throws {
        let session = makeSession()
        SimulationMockURLProtocol.requestHandler = { request in
            let url = try #require(request.url)
            let response = HTTPURLResponse(url: url, statusCode: 204, httpVersion: nil, headerFields: nil)!
            return (response, Data())
        }
        let client = makeClient(session: session)
        try await client.deletePreset(id: "p1")
        let request = try #require(SimulationMockURLProtocol.lastRequest)
        #expect(request.httpMethod == "DELETE")
        let requestURL = try #require(request.url)
        let components = try #require(URLComponents(url: requestURL, resolvingAgainstBaseURL: false))
        #expect(components.path == "/simulation-presets/p1")
    }

    // MARK: - Helpers

    private static let statusBody = """
    {"live": {"ppv": 1000, "pload": 700, "pbat": 250, "pgrid": 0,
              "pgridSustained": false, "soc": 62, "timestamp": "2026-06-09T10:00:00Z"}}
    """

    private func makeClient(session: URLSession) -> URLSessionAPIClient {
        URLSessionAPIClient(baseURL: URL(string: "https://example.com")!, token: "token", session: session)
    }

    private func makeSession() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [SimulationMockURLProtocol.self]
        return URLSession(configuration: configuration)
    }
}
// swiftlint:enable file_length type_body_length
