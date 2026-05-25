#if os(macOS)
import FluxCore
import Foundation
import Testing
@testable import Flux

/// Verifies the macOS Day Detail toolbar disabled-state predicate and the
/// view-model navigation hooks it invokes. SwiftUI toolbar buttons are not
/// directly inspectable from a unit test, so this exercise covers the
/// predicate (`viewModel.isToday`) and the navigation methods the toolbar
/// buttons call — see design.md "Testing Strategy" for the acknowledged
/// toolbar-wiring gap.
@MainActor @Suite(.serialized)
struct DayDetailMacToolbarTests {
    @Test
    func nextDayDisabledPredicateIsTrueWhenViewModelIsToday() {
        let apiClient = StubAPIClient()
        let now = TestDates.utc(year: 2026, month: 4, day: 15, hour: 14, minute: 30)
        // Sydney is +10/+11 from UTC; 14:30 UTC on Apr 15 is Apr 16 Sydney.
        let viewModel = DayDetailViewModel(date: "2026-04-16", apiClient: apiClient, nowProvider: { now })

        #expect(viewModel.isToday == true)
    }

    @Test
    func nextDayDisabledPredicateIsFalseForPastDay() {
        let apiClient = StubAPIClient()
        let now = TestDates.utc(year: 2026, month: 4, day: 15, hour: 14, minute: 30)
        let viewModel = DayDetailViewModel(date: "2026-04-10", apiClient: apiClient, nowProvider: { now })

        #expect(viewModel.isToday == false)
    }

    @Test
    func navigatePreviousMovesDateBackOneSydneyDay() {
        let apiClient = StubAPIClient()
        let fixedNow = TestDates.utc(year: 2026, month: 4, day: 13, hour: 0, minute: 0)
        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient, nowProvider: { fixedNow })

        viewModel.navigatePrevious()

        #expect(viewModel.date == "2026-04-14")
    }

    @Test
    func navigateNextMovesDateForwardOneSydneyDay() {
        let apiClient = StubAPIClient()
        let fixedNow = TestDates.utc(year: 2026, month: 4, day: 13, hour: 0, minute: 0)
        let viewModel = DayDetailViewModel(date: "2026-04-14", apiClient: apiClient, nowProvider: { fixedNow })

        viewModel.navigateNext()

        #expect(viewModel.date == "2026-04-15")
    }

    @Test
    func navigateNextIsNoopWhenAlreadyToday() {
        let apiClient = StubAPIClient()
        let now = TestDates.utc(year: 2026, month: 4, day: 15, hour: 14, minute: 30)
        let viewModel = DayDetailViewModel(date: "2026-04-16", apiClient: apiClient, nowProvider: { now })

        #expect(viewModel.isToday == true)
        viewModel.navigateNext()
        #expect(viewModel.date == "2026-04-16")
    }
}
#endif
