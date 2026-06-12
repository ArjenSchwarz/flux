import Foundation

// MARK: - History period navigation (history-period-navigation spec)

extension URLSessionAPIClient {
    /// Real encoding for both query forms: `.days` reuses the existing
    /// `?days=N` request; `.dateRange` sends the inclusive past-only
    /// `?start=YYYY-MM-DD&end=YYYY-MM-DD` form.
    public func fetchHistory(query: HistoryQuery) async throws -> HistoryResponse {
        switch query {
        case let .days(days):
            return try await fetchHistory(days: days)
        case let .dateRange(start, end):
            return try await performRequest(
                path: "history",
                queryItems: [
                    URLQueryItem(name: "start", value: start),
                    URLQueryItem(name: "end", value: end)
                ]
            )
        }
    }
}
