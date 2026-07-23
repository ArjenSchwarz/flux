import Foundation

public enum FluxAPIError: Error, Sendable, Equatable {
    case notConfigured
    case unauthorized
    case badRequest(String)
    case serverError
    case networkError(String)
    case decodingError(String)
    case unexpectedStatus(Int)
    case ruleCapReached
    case notFound
    case pricingValidation(PricingValidationReason)
}

/// Validation failure codes returned by the pricing endpoints (AC 7.2).
/// Mirrors the server-side error codes verbatim so the editor can map each one
/// to inline field-level feedback.
public enum PricingValidationReason: Error, Sendable, Equatable {
    case invertedDates
    case overlap(openEndedId: String?)
    case ratePrecision
    case rateOutOfRange
    case secondOpenEnded
    /// Returned as HTTP 409 when a concurrent writer raced this one; the
    /// editor refetches the list and retries.
    case concurrentWrite
    /// Band rules (time-of-use-pricing).
    case bandWindowInvalid
    case bandOverlap
    case multipleFreeBands
    case savingsRateMissing
    case noRatedBand
    /// A pre-migration three-rate payload, or a `replace-open-ended` whose
    /// closing row is still the legacy shape (AC 7.3 / Q32).
    case legacyShape
}

extension FluxAPIError {
    public static func from(_ error: Error) -> FluxAPIError {
        if let apiError = error as? FluxAPIError {
            return apiError
        }
        return .networkError(error.localizedDescription)
    }

    public var message: String {
        switch self {
        case .notConfigured:
            return "Set your API URL and token in Settings."
        case .unauthorized:
            return "Your API token is invalid or expired. Update it in Settings."
        case let .badRequest(details):
            return details
        case .serverError:
            return "The backend is temporarily unavailable. Please try again."
        case let .networkError(details):
            return details
        case let .decodingError(details):
            return "The app could not read backend data: \(details)"
        case let .unexpectedStatus(status):
            return "The backend returned an unexpected status (\(status))."
        case .ruleCapReached:
            return "You can have at most 10 alert rules per device."
        case .notFound:
            return "The item could not be found."
        case let .pricingValidation(reason):
            return reason.message
        }
    }

    public var suggestsSettings: Bool {
        switch self {
        case .notConfigured, .unauthorized:
            return true
        default:
            return false
        }
    }
}

extension PricingValidationReason {
    public var message: String {
        switch self {
        case .invertedDates:
            return "The end date must be after the start date."
        case .overlap:
            return "This plan overlaps an existing one. Close the previous plan first."
        case .ratePrecision:
            return "Rates must use at most four decimal places."
        case .rateOutOfRange:
            return "Each rate must be between $0.00 and $10.00 per kWh."
        case .secondOpenEnded:
            return "Only one open-ended pricing plan is allowed at a time."
        case .concurrentWrite:
            return "Another change was just applied. Try again."
        case .bandWindowInvalid:
            return "Each window needs a start before its end, between 00:00 and 24:00."
        case .bandOverlap:
            return "Windows must not overlap each other."
        case .multipleFreeBands:
            return "A plan can have at most one free window."
        case .savingsRateMissing:
            return "A plan with a free window needs a savings reference rate."
        case .noRatedBand:
            return "A free window covering the whole day leaves nothing to price."
        case .legacyShape:
            return "This plan is still in the old three-rate format. Run the pricing migration first."
        }
    }
}
