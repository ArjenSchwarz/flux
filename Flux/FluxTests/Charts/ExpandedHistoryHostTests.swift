import Foundation
import Testing
@testable import Flux

@MainActor
@Suite
struct ExpandedHistoryHostTests {
    @Test("Adopting a snapshot before any drag updates displayed immediately")
    func adoptBeforeDragAppliesImmediately() {
        let controller = ExpandedHistoryHostController(initial: makeSnapshot(solarTotal: 10))
        let newer = makeSnapshot(solarTotal: 25)

        controller.adopt(newer)

        #expect(controller.displayed.summary.solarTotalKwh == 25)
    }

    @Test("Refresh during a drag is deferred until drag ends")
    func refreshDuringDragIsDeferred() {
        let initial = makeSnapshot(solarTotal: 10)
        let controller = ExpandedHistoryHostController(initial: initial)

        controller.beginDrag()
        let refreshed = makeSnapshot(solarTotal: 42)
        controller.adopt(refreshed)

        #expect(controller.displayed.summary.solarTotalKwh == 10)

        controller.endDrag()

        #expect(controller.displayed.summary.solarTotalKwh == 42)
    }

    @Test("Only the most recent snapshot is applied on drag end")
    func onlyLatestSnapshotApplied() {
        let controller = ExpandedHistoryHostController(initial: makeSnapshot(solarTotal: 0))

        controller.beginDrag()
        controller.adopt(makeSnapshot(solarTotal: 1))
        controller.adopt(makeSnapshot(solarTotal: 2))
        controller.adopt(makeSnapshot(solarTotal: 3))

        #expect(controller.displayed.summary.solarTotalKwh == 0)

        controller.endDrag()

        #expect(controller.displayed.summary.solarTotalKwh == 3)
    }

    @Test("Ending a drag with no pending snapshot keeps the displayed snapshot")
    func endDragWithoutRefreshIsNoOp() {
        let controller = ExpandedHistoryHostController(initial: makeSnapshot(solarTotal: 7))

        controller.beginDrag()
        controller.endDrag()

        #expect(controller.displayed.summary.solarTotalKwh == 7)
    }

    @Test("After a drag ends, a fresh adopt is applied immediately")
    func adoptAfterDragEndsAppliesImmediately() {
        let controller = ExpandedHistoryHostController(initial: makeSnapshot(solarTotal: 1))

        controller.beginDrag()
        controller.adopt(makeSnapshot(solarTotal: 2))
        controller.endDrag()

        controller.adopt(makeSnapshot(solarTotal: 9))

        #expect(controller.displayed.summary.solarTotalKwh == 9)
    }

    @Test("Pending snapshot does not leak into a subsequent drag")
    func pendingSnapshotDoesNotLeakAcrossDrags() {
        let controller = ExpandedHistoryHostController(initial: makeSnapshot(solarTotal: 1))

        controller.beginDrag()
        controller.adopt(makeSnapshot(solarTotal: 2))
        controller.endDrag()
        #expect(controller.displayed.summary.solarTotalKwh == 2)

        controller.beginDrag()
        controller.endDrag()

        #expect(controller.displayed.summary.solarTotalKwh == 2)
    }

    private func makeSnapshot(solarTotal: Double) -> ExpandedHistoryHostSnapshot {
        ExpandedHistoryHostSnapshot(
            solar: [],
            grid: [],
            dailyUsage: [],
            summary: HistoryViewModel.PeriodSummary(
                solarTotalKwh: solarTotal,
                solarDayCount: solarTotal > 0 ? 1 : 0,
                peakImportTotalKwh: 0,
                offpeakImportTotalKwh: 0,
                exportTotalKwh: 0,
                gridDayCount: 0,
                chargeTotalKwh: 0,
                dischargeTotalKwh: 0,
                batteryDayCount: 0,
                dailyUsageTotalKwh: 0,
                dailyUsageDayCount: 0,
                dailyUsageLargestKind: nil,
                dailyUsageLargestKindTotalKwh: 0,
                nightTotalKwh: 0,
                nightBlockDayCount: 0,
                mostUsageDay: nil,
                mostSolarDay: nil,
                lowestSocDay: nil
            )
        )
    }
}
