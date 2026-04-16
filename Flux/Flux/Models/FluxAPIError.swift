import Foundation

enum FluxAPIError: Error, Sendable, Equatable {
    case notConfigured
    case unauthorized
    case badRequest(String)
    case serverError
    case networkError(String)
    case decodingError(String)
    case unexpectedStatus(Int)
}

extension FluxAPIError {
    static func from(_ error: Error) -> FluxAPIError {
        if let apiError = error as? FluxAPIError {
            return apiError
        }
        return .networkError(error.localizedDescription)
    }

    var message: String {
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
        }
    }

    var suggestsSettings: Bool {
        switch self {
        case .notConfigured, .unauthorized:
            return true
        default:
            return false
        }
    }
}
