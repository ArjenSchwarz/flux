import Foundation
import Testing
@testable import FluxCore

// swiftlint:disable file_length type_body_length
@MainActor @Suite(.serialized)
struct URLSessionAPIClientPricingTests {
    // MARK: - List

    @Test
    func fetchPricingDecodesTheBandShape() async throws {
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
                  "endDate": "2026-08-01",
                  "defaultRate": 0.2873,
                  "windows": [{ "start": "11:00", "end": "14:00", "free": true }],
                  "feedInRate": 0.05,
                  "savingsReferenceRate": 0.2873,
                  "createdAt": "2026-01-01T00:00:00Z",
                  "updatedAt": "2026-01-01T00:00:00Z"
                },
                {
                  "id": "pp-2",
                  "startDate": "2026-08-01",
                  "defaultRate": 0.35,
                  "windows": [
                    { "start": "10:00", "end": "15:00", "free": true },
                    { "start": "01:00", "end": "06:00", "free": false, "rate": 0.28 }
                  ],
                  "feedInRate": 0.06,
                  "savingsReferenceRate": 0.35,
                  "createdAt": "2026-07-01T00:00:00Z",
                  "updatedAt": "2026-07-01T00:00:00Z"
                }
              ]
            }
            """
            return (response, Data(body.utf8))
        }
        let client = makeClient(session: session)
        let plans = try await client.fetchPricing()

        #expect(plans.count == 2)
        #expect(plans[0].id == "pp-1")
        // The exclusive end and the successor's start are the same literal date.
        #expect(plans[0].endDate == "2026-08-01")
        #expect(plans[1].startDate == "2026-08-01")
        #expect(plans[1].endDate == nil)
        #expect(plans[1].windows.count == 2)
        #expect(plans[1].windows[1].rate == 0.28)

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
        #expect(try await client.fetchPricing().isEmpty)
    }

    // MARK: - Create

    @Test
    func createPricingPostsTheBandPayload() async throws {
        let session = makeSession()
        PricingMockURLProtocol.requestHandler = { request in
            let url = try #require(request.url)
            let response = HTTPURLResponse(
                url: url, statusCode: 200, httpVersion: nil, headerFields: nil
            )!
            return (response, Data(Self.newPlanBody.utf8))
        }
        let client = makeClient(session: session)
        let created = try await client.createPricing(makeDraft())

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
        #expect((json["defaultRate"] as? NSNumber)?.doubleValue == 0.35)
        #expect((json["savingsReferenceRate"] as? NSNumber)?.doubleValue == 0.35)
        // The legacy three-rate shape is rejected by the server (AC 7.3), so
        // the client must never emit it.
        #expect(json["peakRate"] == nil)

        let windows = try #require(json["windows"] as? [[String: Any]])
        #expect(windows.count == 2)
        #expect(windows[0]["start"] as? String == "10:00")
        #expect(windows[0]["free"] as? Bool == true)
        #expect((windows[1]["rate"] as? NSNumber)?.doubleValue == 0.28)
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
            {"error": "overlap", "openEndedId": "pp-open-123", "conflictingPricingId": "pp-open-123"}
            """
            return (response, Data(body.utf8))
        }
        let client = makeClient(session: session)
        do {
            _ = try await client.createPricing(makeDraft())
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
    func createPricingMapsAnOverlapWithANonOpenEndedPlan() async throws {
        // Only an overlap with the unique open-ended plan can be remediated in
        // one tap, so `openEndedId` is absent for any other conflict.
        let session = makeSession()
        PricingMockURLProtocol.requestHandler = { request in
            let url = try #require(request.url)
            let response = HTTPURLResponse(
                url: url, statusCode: 400, httpVersion: nil, headerFields: nil
            )!
            return (response, Data("{\"error\":\"overlap\",\"conflictingPricingId\":\"pp-closed\"}".utf8))
        }
        let client = makeClient(session: session)
        do {
            _ = try await client.createPricing(makeDraft())
            Issue.record("expected overlap error")
        } catch let error as FluxAPIError {
            #expect(error == .pricingValidation(.overlap(openEndedId: nil)))
        }
    }

    @Test
    func createPricingMapsAllValidationCodes() async throws {
        let cases: [(String, PricingValidationReason)] = [
            ("inverted_dates", .invertedDates),
            ("rate_precision", .ratePrecision),
            ("rate_out_of_range", .rateOutOfRange),
            ("second_open_ended", .secondOpenEnded),
            ("band_window_invalid", .bandWindowInvalid),
            ("band_overlap", .bandOverlap),
            ("multiple_free_bands", .multipleFreeBands),
            ("savings_rate_missing", .savingsRateMissing),
            ("no_rated_band", .noRatedBand),
            ("legacy_shape", .legacyShape)
        ]
        for (code, expectedReason) in cases {
            let session = makeSession()
            PricingMockURLProtocol.requestHandler = { request in
                let url = try #require(request.url)
                let response = HTTPURLResponse(
                    url: url, statusCode: 400, httpVersion: nil, headerFields: nil
                )!
                return (response, Data("{\"error\": \"\(code)\"}".utf8))
            }
            let client = makeClient(session: session)
            do {
                _ = try await client.createPricing(makeDraft())
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
    func everyValidationReasonHasAMessage() {
        let reasons: [PricingValidationReason] = [
            .invertedDates, .overlap(openEndedId: nil), .ratePrecision, .rateOutOfRange,
            .secondOpenEnded, .concurrentWrite, .bandWindowInvalid, .bandOverlap,
            .multipleFreeBands, .savingsRateMissing, .noRatedBand, .legacyShape
        ]
        for reason in reasons {
            #expect(!reason.message.isEmpty, "\(reason)")
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
            return (response, Data("{\"error\": \"concurrent_open_ended_write\"}".utf8))
        }
        let client = makeClient(session: session)
        do {
            _ = try await client.createPricing(makeDraft())
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
        let client = makeClient(session: session)
        do {
            _ = try await client.createPricing(makeDraft())
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
            return (response, Data(Self.newPlanBody.utf8))
        }
        let client = makeClient(session: session)
        let updated = try await client.updatePricing(id: "pp-new", makeDraft())
        #expect(updated.defaultRate == 0.35)
        let request = try #require(PricingMockURLProtocol.lastRequest)
        #expect(request.httpMethod == "PUT")
        let requestURL = try #require(request.url)
        let components = try #require(URLComponents(url: requestURL, resolvingAgainstBaseURL: false))
        #expect(components.path == "/pricing/pp-new")
    }

    @Test
    func updatePricingMaps404ToNotFound() async throws {
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
            _ = try await client.updatePricing(id: "pp-missing", makeDraft())
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
    func replaceOpenEndedClosesThePredecessorOnTheSuccessorsStartDate() async throws {
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
                  "endDate": "2026-08-01",
                  "defaultRate": 0.2873,
                  "windows": [{ "start": "11:00", "end": "14:00", "free": true }],
                  "feedInRate": 0.05,
                  "savingsReferenceRate": 0.2873,
                  "createdAt": "2026-01-01T00:00:00Z",
                  "updatedAt": "2026-08-01T00:00:00Z"
                },
                \(Self.newPlanBody)
              ]
            }
            """
            return (response, Data(body.utf8))
        }
        let client = makeClient(session: session)
        let result = try await client.replaceOpenEndedPricing(closingId: "pp-open", with: makeDraft())

        #expect(result.closing.id == "pp-open")
        // Exclusive end: the closing plan's end date IS the successor's start
        // date (AC 2.2), with no ±1 arithmetic anywhere.
        #expect(result.closing.endDate == "2026-08-01")
        #expect(result.newPlan.id == "pp-new")
        #expect(result.newPlan.startDate == "2026-08-01")
        #expect(result.newPlan.endDate == nil)

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
        #expect(newPeriod["windows"] != nil)
    }

    @Test
    func replaceOpenEndedMapsLegacyShapeRejection() async throws {
        // The closing row is still the pre-migration three-rate shape (Q32).
        let session = makeSession()
        PricingMockURLProtocol.requestHandler = { request in
            let url = try #require(request.url)
            let response = HTTPURLResponse(
                url: url, statusCode: 400, httpVersion: nil, headerFields: nil
            )!
            return (response, Data("{\"error\":\"legacy_shape\"}".utf8))
        }
        let client = makeClient(session: session)
        do {
            _ = try await client.replaceOpenEndedPricing(closingId: "pp-open", with: makeDraft())
            Issue.record("expected legacyShape")
        } catch let error as FluxAPIError {
            #expect(error == .pricingValidation(.legacyShape))
        }
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
        let client = makeClient(session: session)
        do {
            _ = try await client.replaceOpenEndedPricing(closingId: "pp-open", with: makeDraft())
            Issue.record("expected concurrent")
        } catch let error as FluxAPIError {
            #expect(error == .pricingValidation(.concurrentWrite))
        }
    }

    // MARK: - Helpers

    private static let newPlanBody = """
    {
      "id": "pp-new",
      "startDate": "2026-08-01",
      "defaultRate": 0.35,
      "windows": [
        { "start": "10:00", "end": "15:00", "free": true },
        { "start": "01:00", "end": "06:00", "free": false, "rate": 0.28 }
      ],
      "feedInRate": 0.06,
      "savingsReferenceRate": 0.35,
      "createdAt": "2026-08-01T00:00:00Z",
      "updatedAt": "2026-08-01T00:00:00Z"
    }
    """

    private func makeDraft() -> PricingPlanDraft {
        PricingPlanDraft(
            startDate: "2026-08-01",
            endDate: nil,
            defaultRate: 0.35,
            windows: [
                PlanWindow(start: "10:00", end: "15:00", free: true, rate: nil),
                PlanWindow(start: "01:00", end: "06:00", free: false, rate: 0.28)
            ],
            feedInRate: 0.06,
            savingsReferenceRate: 0.35
        )
    }

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
