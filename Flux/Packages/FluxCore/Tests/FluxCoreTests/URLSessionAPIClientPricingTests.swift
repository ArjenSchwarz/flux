import Foundation
import Testing
@testable import FluxCore

// swiftlint:disable file_length type_body_length
@MainActor @Suite(.serialized)
struct URLSessionAPIClientPricingTests {
    // MARK: - List

    @Test
    func fetchPricingDecodesArray() async throws {
        let session = makeSession()
        PricingMockURLProtocol.requestHandler = { request in
            let url = try #require(request.url)
            let response = HTTPURLResponse(
                url: url, statusCode: 200, httpVersion: nil, headerFields: nil
            )!
            let body = """
            {
              "pricing": [
                {
                  "id": "pp-1",
                  "startDate": "2026-01-01",
                  "endDate": "2026-06-30",
                  "peakRate": 0.2873,
                  "feedInRate": 0.05,
                  "offPeakSavingsRate": 0.12,
                  "createdAt": "2026-01-01T00:00:00Z",
                  "updatedAt": "2026-01-01T00:00:00Z"
                },
                {
                  "id": "pp-2",
                  "startDate": "2026-07-01",
                  "peakRate": 0.30,
                  "feedInRate": 0.06,
                  "offPeakSavingsRate": 0.12,
                  "createdAt": "2026-07-01T00:00:00Z",
                  "updatedAt": "2026-07-01T00:00:00Z"
                }
              ]
            }
            """
            return (response, Data(body.utf8))
        }
        let client = makeClient(session: session)
        let periods = try await client.fetchPricing()

        #expect(periods.count == 2)
        #expect(periods[0].id == "pp-1")
        #expect(periods[1].endDate == nil)
        let request = try #require(PricingMockURLProtocol.lastRequest)
        let requestURL = try #require(request.url)
        let components = try #require(URLComponents(url: requestURL, resolvingAgainstBaseURL: false))
        #expect(components.path == "/pricing")
        #expect(request.httpMethod == "GET")
        #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer token")
    }

    @Test
    func fetchPricingReturnsEmptyArrayOnEmptyResponse() async throws {
        let session = makeSession()
        PricingMockURLProtocol.requestHandler = { request in
            let url = try #require(request.url)
            let response = HTTPURLResponse(
                url: url, statusCode: 200, httpVersion: nil, headerFields: nil
            )!
            return (response, Data("{\"pricing\": []}".utf8))
        }
        let client = makeClient(session: session)
        let periods = try await client.fetchPricing()
        #expect(periods.isEmpty)
    }

    // MARK: - Create

    @Test
    func createPricingPostsDraftAndDecodesResponse() async throws {
        let session = makeSession()
        PricingMockURLProtocol.requestHandler = { request in
            let url = try #require(request.url)
            let response = HTTPURLResponse(
                url: url, statusCode: 200, httpVersion: nil, headerFields: nil
            )!
            let body = """
            {
              "id": "pp-new",
              "startDate": "2026-08-01",
              "peakRate": 0.30,
              "feedInRate": 0.06,
              "offPeakSavingsRate": 0.12,
              "createdAt": "2026-08-01T00:00:00Z",
              "updatedAt": "2026-08-01T00:00:00Z"
            }
            """
            return (response, Data(body.utf8))
        }
        let draft = PricingPeriodDraft(
            startDate: "2026-08-01",
            endDate: nil,
            peakRate: 0.30,
            feedInRate: 0.06,
            offPeakSavingsRate: 0.12
        )
        let client = makeClient(session: session)
        let created = try await client.createPricing(draft)

        #expect(created.id == "pp-new")
        let request = try #require(PricingMockURLProtocol.lastRequest)
        #expect(request.httpMethod == "POST")
        let requestURL = try #require(request.url)
        let components = try #require(URLComponents(url: requestURL, resolvingAgainstBaseURL: false))
        #expect(components.path == "/pricing")
        #expect(request.value(forHTTPHeaderField: "Content-Type") == "application/json")
        let bodyData = try #require(PricingMockURLProtocol.lastRequestBody)
        let json = try #require(try JSONSerialization.jsonObject(with: bodyData) as? [String: Any])
        #expect(json["startDate"] as? String == "2026-08-01")
        #expect((json["peakRate"] as? NSNumber)?.doubleValue == 0.30)
    }

    @Test
    func createPricingMapsOverlapErrorToValidation() async throws {
        let session = makeSession()
        PricingMockURLProtocol.requestHandler = { request in
            let url = try #require(request.url)
            let response = HTTPURLResponse(
                url: url, statusCode: 400, httpVersion: nil, headerFields: nil
            )!
            let body = """
            {"error": "overlap", "openEndedId": "pp-open-123"}
            """
            return (response, Data(body.utf8))
        }
        let draft = PricingPeriodDraft(
            startDate: "2026-08-01",
            endDate: nil,
            peakRate: 0.30,
            feedInRate: 0.06,
            offPeakSavingsRate: 0.12
        )
        let client = makeClient(session: session)
        do {
            _ = try await client.createPricing(draft)
            Issue.record("expected overlap error")
        } catch let error as FluxAPIError {
            guard case let .pricingValidation(reason) = error,
                  case let .overlap(openEndedId) = reason else {
                Issue.record("got \(error)")
                return
            }
            #expect(openEndedId == "pp-open-123")
        }
    }

    @Test
    func createPricingMapsAllValidationCodes() async throws {
        let cases: [(String, PricingValidationReason)] = [
            ("inverted_dates", .invertedDates),
            ("rate_precision", .ratePrecision),
            ("rate_out_of_range", .rateOutOfRange),
            ("second_open_ended", .secondOpenEnded)
        ]
        for (code, expectedReason) in cases {
            let session = makeSession()
            PricingMockURLProtocol.requestHandler = { request in
                let url = try #require(request.url)
                let response = HTTPURLResponse(
                    url: url, statusCode: 400, httpVersion: nil, headerFields: nil
                )!
                let body = "{\"error\": \"\(code)\"}"
                return (response, Data(body.utf8))
            }
            let draft = PricingPeriodDraft(
                startDate: "2026-08-01",
                peakRate: 0.3,
                feedInRate: 0.05,
                offPeakSavingsRate: 0.12
            )
            let client = makeClient(session: session)
            do {
                _ = try await client.createPricing(draft)
                Issue.record("expected \(code)")
            } catch let error as FluxAPIError {
                guard case let .pricingValidation(reason) = error else {
                    Issue.record("got \(error) for \(code)")
                    continue
                }
                #expect(reason == expectedReason, "code \(code)")
            }
        }
    }

    @Test
    func createPricingMaps409ToConcurrentWrite() async throws {
        let session = makeSession()
        PricingMockURLProtocol.requestHandler = { request in
            let url = try #require(request.url)
            let response = HTTPURLResponse(
                url: url, statusCode: 409, httpVersion: nil, headerFields: nil
            )!
            let body = "{\"error\": \"concurrent_open_ended_write\"}"
            return (response, Data(body.utf8))
        }
        let draft = PricingPeriodDraft(
            startDate: "2026-08-01",
            peakRate: 0.3,
            feedInRate: 0.05,
            offPeakSavingsRate: 0.12
        )
        let client = makeClient(session: session)
        do {
            _ = try await client.createPricing(draft)
            Issue.record("expected concurrent write error")
        } catch let error as FluxAPIError {
            #expect(error == .pricingValidation(.concurrentWrite))
        }
    }

    @Test
    func createPricingMaps401ToUnauthorized() async throws {
        let session = makeSession()
        PricingMockURLProtocol.requestHandler = { request in
            let url = try #require(request.url)
            let response = HTTPURLResponse(
                url: url, statusCode: 401, httpVersion: nil, headerFields: nil
            )!
            return (response, Data())
        }
        let draft = PricingPeriodDraft(
            startDate: "2026-08-01",
            peakRate: 0.3,
            feedInRate: 0.05,
            offPeakSavingsRate: 0.12
        )
        let client = makeClient(session: session)
        do {
            _ = try await client.createPricing(draft)
            Issue.record("expected unauthorized")
        } catch let error as FluxAPIError {
            #expect(error == .unauthorized)
        }
    }

    // MARK: - Update

    @Test
    func updatePricingPutsDraftAndDecodesResponse() async throws {
        let session = makeSession()
        PricingMockURLProtocol.requestHandler = { request in
            let url = try #require(request.url)
            let response = HTTPURLResponse(
                url: url, statusCode: 200, httpVersion: nil, headerFields: nil
            )!
            let body = """
            {
              "id": "pp-1",
              "startDate": "2026-01-01",
              "endDate": "2026-06-30",
              "peakRate": 0.31,
              "feedInRate": 0.06,
              "offPeakSavingsRate": 0.12,
              "createdAt": "2026-01-01T00:00:00Z",
              "updatedAt": "2026-05-23T00:00:00Z"
            }
            """
            return (response, Data(body.utf8))
        }
        let draft = PricingPeriodDraft(
            startDate: "2026-01-01",
            endDate: "2026-06-30",
            peakRate: 0.31,
            feedInRate: 0.06,
            offPeakSavingsRate: 0.12
        )
        let client = makeClient(session: session)
        let updated = try await client.updatePricing(id: "pp-1", draft)
        #expect(updated.id == "pp-1")
        #expect(updated.peakRate == 0.31)
        let request = try #require(PricingMockURLProtocol.lastRequest)
        #expect(request.httpMethod == "PUT")
        let requestURL = try #require(request.url)
        let components = try #require(URLComponents(url: requestURL, resolvingAgainstBaseURL: false))
        #expect(components.path == "/pricing/pp-1")
    }

    @Test
    func updatePricingMaps404ToNotFoundAsBadRequest() async throws {
        let session = makeSession()
        PricingMockURLProtocol.requestHandler = { request in
            let url = try #require(request.url)
            let response = HTTPURLResponse(
                url: url, statusCode: 404, httpVersion: nil, headerFields: nil
            )!
            return (response, Data())
        }
        let draft = PricingPeriodDraft(
            startDate: "2026-01-01",
            peakRate: 0.3,
            feedInRate: 0.05,
            offPeakSavingsRate: 0.12
        )
        let client = makeClient(session: session)
        do {
            _ = try await client.updatePricing(id: "pp-missing", draft)
            Issue.record("expected notFound")
        } catch let error as FluxAPIError {
            #expect(error == .notFound)
        }
    }

    // MARK: - Delete

    @Test
    func deletePricingHits204AndReturns() async throws {
        let session = makeSession()
        PricingMockURLProtocol.requestHandler = { request in
            let url = try #require(request.url)
            let response = HTTPURLResponse(
                url: url, statusCode: 204, httpVersion: nil, headerFields: nil
            )!
            return (response, Data())
        }
        let client = makeClient(session: session)
        try await client.deletePricing(id: "pp-1")
        let request = try #require(PricingMockURLProtocol.lastRequest)
        #expect(request.httpMethod == "DELETE")
        let requestURL = try #require(request.url)
        let components = try #require(URLComponents(url: requestURL, resolvingAgainstBaseURL: false))
        #expect(components.path == "/pricing/pp-1")
    }

    @Test
    func deletePricing404MapsToNotFound() async throws {
        let session = makeSession()
        PricingMockURLProtocol.requestHandler = { request in
            let url = try #require(request.url)
            let response = HTTPURLResponse(
                url: url, statusCode: 404, httpVersion: nil, headerFields: nil
            )!
            return (response, Data())
        }
        let client = makeClient(session: session)
        do {
            try await client.deletePricing(id: "pp-missing")
            Issue.record("expected notFound")
        } catch let error as FluxAPIError {
            #expect(error == .notFound)
        }
    }

    // MARK: - Replace open-ended

    @Test
    func replaceOpenEndedPricingPostsCombinedPayload() async throws {
        let session = makeSession()
        PricingMockURLProtocol.requestHandler = { request in
            let url = try #require(request.url)
            let response = HTTPURLResponse(
                url: url, statusCode: 200, httpVersion: nil, headerFields: nil
            )!
            let body = """
            {
              "pricing": [
                {
                  "id": "pp-open",
                  "startDate": "2026-01-01",
                  "endDate": "2026-07-31",
                  "peakRate": 0.2873,
                  "feedInRate": 0.05,
                  "offPeakSavingsRate": 0.12,
                  "createdAt": "2026-01-01T00:00:00Z",
                  "updatedAt": "2026-08-01T00:00:00Z"
                },
                {
                  "id": "pp-new",
                  "startDate": "2026-08-01",
                  "peakRate": 0.30,
                  "feedInRate": 0.06,
                  "offPeakSavingsRate": 0.12,
                  "createdAt": "2026-08-01T00:00:00Z",
                  "updatedAt": "2026-08-01T00:00:00Z"
                }
              ]
            }
            """
            return (response, Data(body.utf8))
        }
        let draft = PricingPeriodDraft(
            startDate: "2026-08-01",
            endDate: nil,
            peakRate: 0.30,
            feedInRate: 0.06,
            offPeakSavingsRate: 0.12
        )
        let client = makeClient(session: session)
        let result = try await client.replaceOpenEndedPricing(closingId: "pp-open", with: draft)
        #expect(result.closing.id == "pp-open")
        #expect(result.closing.endDate == "2026-07-31")
        #expect(result.newPeriod.id == "pp-new")
        #expect(result.newPeriod.endDate == nil)
        let request = try #require(PricingMockURLProtocol.lastRequest)
        #expect(request.httpMethod == "POST")
        let requestURL = try #require(request.url)
        let components = try #require(URLComponents(url: requestURL, resolvingAgainstBaseURL: false))
        #expect(components.path == "/pricing/replace-open-ended")
        let bodyData = try #require(PricingMockURLProtocol.lastRequestBody)
        let json = try #require(try JSONSerialization.jsonObject(with: bodyData) as? [String: Any])
        #expect(json["closingPricingId"] as? String == "pp-open")
        let newPeriod = try #require(json["newPeriod"] as? [String: Any])
        #expect(newPeriod["startDate"] as? String == "2026-08-01")
    }

    @Test
    func replaceOpenEndedMaps409ToConcurrentWrite() async throws {
        let session = makeSession()
        PricingMockURLProtocol.requestHandler = { request in
            let url = try #require(request.url)
            let response = HTTPURLResponse(
                url: url, statusCode: 409, httpVersion: nil, headerFields: nil
            )!
            return (response, Data("{\"error\":\"concurrent_open_ended_write\"}".utf8))
        }
        let draft = PricingPeriodDraft(
            startDate: "2026-08-01",
            peakRate: 0.30,
            feedInRate: 0.06,
            offPeakSavingsRate: 0.12
        )
        let client = makeClient(session: session)
        do {
            _ = try await client.replaceOpenEndedPricing(closingId: "pp-open", with: draft)
            Issue.record("expected concurrent")
        } catch let error as FluxAPIError {
            #expect(error == .pricingValidation(.concurrentWrite))
        }
    }

    // MARK: - Helpers
    private func makeClient(session: URLSession) -> URLSessionAPIClient {
        URLSessionAPIClient(baseURL: URL(string: "https://example.com")!, token: "token", session: session)
    }
    private func makeSession() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [PricingMockURLProtocol.self]
        return URLSession(configuration: configuration)
    }
}
// swiftlint:enable file_length type_body_length
