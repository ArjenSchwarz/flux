enum FluxAPIError: Error, Sendable {
    case notConfigured
    case unauthorized
    case badRequest(String)
    case serverError
    case networkError(String)
    case decodingError(String)
    case unexpectedStatus(Int)
}
