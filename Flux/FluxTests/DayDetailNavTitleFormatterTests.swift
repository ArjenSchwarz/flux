#if os(macOS)
import FluxCore
import Foundation
import Testing
@testable import Flux

/// Verifies `DayDetailView.macPageTitle` resolves to "Today" when the
/// view-model reports today, otherwise to a `DayDetailEyebrow.full`
/// formatted string for the parsed date (AC 4.2 / 7.4). The fallback to
/// raw `viewModel.date` for an unparsable date string mirrors
/// `DayNavigationHeader.formattedDate`.
@MainActor @Suite(.serialized)
struct DayDetailNavTitleFormatterTests {
    @Test
    func returnsTodayWhenViewModelIsToday() {
        let apiClient = StubAPIClient()
        let now = Self.makeUTCDate(year: 2026, month: 4, day: 15, hour: 14, minute: 30)
        // Sydney is ahead of UTC; 14:30 UTC on Apr 15 is Apr 16 Sydney.
        let viewModel = DayDetailViewModel(date: "2026-04-16", apiClient: apiClient, nowProvider: { now })

        let view = DayDetailView(viewModel: viewModel)

        #expect(view.macPageTitle == "Today")
    }

    @Test
    func returnsFullFormattedDateForPastDay() {
        let apiClient = StubAPIClient()
        let now = Self.makeUTCDate(year: 2026, month: 4, day: 15, hour: 14, minute: 30)
        let viewModel = DayDetailViewModel(date: "2026-04-10", apiClient: apiClient, nowProvider: { now })

        let view = DayDetailView(viewModel: viewModel)
        let parsedDate = DateFormatting.parseDayDate("2026-04-10")!
        let expected = DayDetailEyebrow.full.string(from: parsedDate)

        #expect(view.macPageTitle == expected)
    }

    @Test
    func fallsBackToRawDateStringWhenUnparseable() {
        let apiClient = StubAPIClient()
        let now = Self.makeUTCDate(year: 2026, month: 4, day: 15, hour: 14, minute: 30)
        let viewModel = DayDetailViewModel(date: "not-a-date", apiClient: apiClient, nowProvider: { now })

        let view = DayDetailView(viewModel: viewModel)

        #expect(view.macPageTitle == "not-a-date")
    }

    private static func makeUTCDate(year: Int, month: Int, day: Int, hour: Int, minute: Int) -> Date {
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

private final class StubAPIClient: FluxAPIClient, @unchecked Sendable {
    func fetchStatus() async throws -> StatusResponse { throw FluxAPIError.notConfigured }
    func fetchHistory(days _: Int) async throws -> HistoryResponse { throw FluxAPIError.notConfigured }
    func fetchDay(date _: String) async throws -> DayDetailResponse { throw FluxAPIError.notConfigured }
    func saveNote(date _: String, text _: String) async throws -> NoteResponse { throw FluxAPIError.notConfigured }
}
#endif
