import FluxCore
import Foundation
@testable import Flux

/// Throw-on-everything `FluxAPIClient` stub for tests that exercise
/// `DayDetailViewModel` / `DashboardViewModel` / etc. without hitting the
/// network. Tests that need specific responses should keep their own
/// per-file mocks; this stub is for the common case where the test only
/// needs the type to satisfy a constructor and never invokes any method.
final class StubAPIClient: FluxAPIClient, @unchecked Sendable {
    func fetchStatus() async throws -> StatusResponse { throw FluxAPIError.notConfigured }
    func fetchHistory(days _: Int) async throws -> HistoryResponse { throw FluxAPIError.notConfigured }
    func fetchDay(date _: String) async throws -> DayDetailResponse { throw FluxAPIError.notConfigured }
    func saveNote(date _: String, text _: String) async throws -> NoteResponse { throw FluxAPIError.notConfigured }
}

/// Date factory for tests that pin `nowProvider()` to a deterministic UTC
/// instant. UTC is the right anchor because `DayDetailViewModel.isToday`
/// derives the Sydney-local "today" string from the wall-clock date, and
/// the conversion is unambiguous from a UTC source.
enum TestDates {
    static func utc(year: Int, month: Int, day: Int, hour: Int, minute: Int) -> Date {
        let calendar = Calendar(identifier: .gregorian)
        return calendar.date(from: DateComponents(
            timeZone: TimeZone(secondsFromGMT: 0),
            year: year,
            month: month,
            day: day,
            hour: hour,
            minute: minute
        ))!
    }
}
