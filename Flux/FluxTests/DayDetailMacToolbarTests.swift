#if os(macOS)
import FluxCore
import Foundation
import Testing
@testable import Flux

/// Verifies the macOS Day Detail toolbar disabled-state predicate and the
/// view-model navigation hooks it invokes. SwiftUI toolbar buttons are not
/// directly inspectable from a unit test, so this exercise covers the
/// predicate (`viewModel.isToday`) and the navigation methods the toolbar
/// buttons call — see design.md "Testing Strategy" / AC 7.2 for the
/// acknowledged toolbar-wiring gap.
@MainActor @Suite(.serialized)
struct DayDetailMacToolbarTests {
    @Test
    func nextDayDisabledPredicateIsTrueWhenViewModelIsToday() {
        let apiClient = StubAPIClient()
        let now = Self.makeUTCDate(year: 2026, month: 4, day: 15, hour: 14, minute: 30)
        // Sydney is +10/+11 from UTC; 14:30 UTC on Apr 15 is Apr 16 Sydney.
        let viewModel = DayDetailViewModel(date: "2026-04-16", apiClient: apiClient, nowProvider: { now })

        #expect(viewModel.isToday == true)
    }

    @Test
    func nextDayDisabledPredicateIsFalseForPastDay() {
        let apiClient = StubAPIClient()
        let now = Self.makeUTCDate(year: 2026, month: 4, day: 15, hour: 14, minute: 30)
        let viewModel = DayDetailViewModel(date: "2026-04-10", apiClient: apiClient, nowProvider: { now })

        #expect(viewModel.isToday == false)
    }

    @Test
    func navigatePreviousMovesDateBackOneSydneyDay() {
        let apiClient = StubAPIClient()
        let fixedNow = Self.makeUTCDate(year: 2026, month: 4, day: 13, hour: 0, minute: 0)
        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient, nowProvider: { fixedNow })

        viewModel.navigatePrevious()

        #expect(viewModel.date == "2026-04-14")
    }

    @Test
    func navigateNextMovesDateForwardOneSydneyDay() {
        let apiClient = StubAPIClient()
        let fixedNow = Self.makeUTCDate(year: 2026, month: 4, day: 13, hour: 0, minute: 0)
        let viewModel = DayDetailViewModel(date: "2026-04-14", apiClient: apiClient, nowProvider: { fixedNow })

        viewModel.navigateNext()

        #expect(viewModel.date == "2026-04-15")
    }

    @Test
    func navigateNextIsNoopWhenAlreadyToday() {
        let apiClient = StubAPIClient()
        let now = Self.makeUTCDate(year: 2026, month: 4, day: 15, hour: 14, minute: 30)
        let viewModel = DayDetailViewModel(date: "2026-04-16", apiClient: apiClient, nowProvider: { now })

        #expect(viewModel.isToday == true)
        viewModel.navigateNext()
        #expect(viewModel.date == "2026-04-16")
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
