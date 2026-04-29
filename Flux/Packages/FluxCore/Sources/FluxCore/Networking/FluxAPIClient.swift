public protocol FluxAPIClient: Sendable {
    func fetchStatus() async throws -> StatusResponse
    func fetchHistory(days: Int) async throws -> HistoryResponse
    func fetchDay(date: String) async throws -> DayDetailResponse
    func saveNote(date: String, text: String) async throws -> NoteResponse
}
