import Foundation

// MARK: - Pricing (daily-costs, time-of-use-pricing specs)

extension URLSessionAPIClient {
    public func fetchPricing() async throws -> [PricingPlan] {
        let response: PricingListResponse = try await performRequest(path: "pricing", queryItems: [])
        return response.pricing
    }

    public func createPricing(_ draft: PricingPlanDraft) async throws -> PricingPlan {
        let body = try encoder.encode(draft)
        return try await performRequest(path: "pricing", queryItems: [], method: "POST", body: body)
    }

    public func updatePricing(id: String, _ draft: PricingPlanDraft) async throws -> PricingPlan {
        let body = try encoder.encode(draft)
        return try await performRequest(path: "pricing/\(id)", queryItems: [], method: "PUT", body: body)
    }

    public func deletePricing(id: String) async throws {
        let _: EmptyPricingResponse = try await performRequest(
            path: "pricing/\(id)",
            queryItems: [],
            method: "DELETE"
        )
    }

    public func replaceOpenEndedPricing(
        closingId: String,
        with draft: PricingPlanDraft
    ) async throws -> ReplaceOpenEndedResult {
        let payload = ReplaceOpenEndedPayload(closingPricingId: closingId, newPeriod: draft)
        let body = try encoder.encode(payload)
        let response: PricingListResponse = try await performRequest(
            path: "pricing/replace-open-ended",
            queryItems: [],
            method: "POST",
            body: body
        )
        guard response.pricing.count == 2 else {
            throw FluxAPIError.decodingError("replace-open-ended expected 2 rows, got \(response.pricing.count)")
        }
        // Match by id rather than position so a server-side reorder
        // (e.g. start-date sort) can't swap closing and new on the wire.
        guard let closing = response.pricing.first(where: { $0.id == closingId }) else {
            throw FluxAPIError.decodingError("replace-open-ended response missing row with id \(closingId)")
        }
        guard let newPlan = response.pricing.first(where: { $0.id != closingId }) else {
            throw FluxAPIError.decodingError("replace-open-ended response: both rows share id \(closingId)")
        }
        return ReplaceOpenEndedResult(closing: closing, newPlan: newPlan)
    }

    /// The server's `error` codes, mirrored one-for-one. `overlap` is the only
    /// one carrying a payload, so it is handled separately from the table.
    private static let validationReasonsByCode: [String: PricingValidationReason] = [
        "inverted_dates": .invertedDates,
        "rate_precision": .ratePrecision,
        "rate_out_of_range": .rateOutOfRange,
        "second_open_ended": .secondOpenEnded,
        "concurrent_open_ended_write": .concurrentWrite,
        "band_window_invalid": .bandWindowInvalid,
        "band_overlap": .bandOverlap,
        "multiple_free_bands": .multipleFreeBands,
        "savings_rate_missing": .savingsRateMissing,
        "no_rated_band": .noRatedBand,
        "legacy_shape": .legacyShape
    ]

    func parsePricingValidationReason(from data: Data) -> PricingValidationReason? {
        guard let payload = try? decoder.decode(PricingErrorResponse.self, from: data) else {
            return nil
        }
        if payload.error == "overlap" {
            return .overlap(openEndedId: payload.openEndedId)
        }
        return Self.validationReasonsByCode[payload.error]
    }

    struct PricingErrorResponse: Decodable {
        let error: String
        /// Populated only when the overlap offender is the unique open-ended
        /// plan — the id that powers the editor's one-tap remediation.
        let openEndedId: String?
    }

    struct PricingListResponse: Decodable {
        let pricing: [PricingPlan]
    }

    /// The request field name stays `newPeriod` — it is the server's wire
    /// contract, unchanged by the band rework.
    struct ReplaceOpenEndedPayload: Encodable {
        let closingPricingId: String
        let newPeriod: PricingPlanDraft
    }

    struct EmptyPricingResponse: Decodable {
        init(from _: Decoder) throws {}
    }
}
