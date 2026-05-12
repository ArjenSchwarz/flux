import Foundation
import FluxCore
import Testing
@testable import Flux

@MainActor
@Suite
struct ExpandedDayHostTests {
    @Test("Adopting a snapshot before any selection updates displayed immediately")
    func adoptWithoutSelectionAppliesImmediately() {
        let now = Date(timeIntervalSince1970: 1_700_000_000)
        let controller = ExpandedDayHostController(
            initial: makeSnapshot(date: "2026-05-12"),
            quietWindow: 0.4,
            clock: { now }
        )

        controller.adopt(makeSnapshot(date: "2026-05-13"))

        #expect(controller.displayed.date == "2026-05-13")
    }

    @Test("Refresh arriving inside the quiet window after a selection change is deferred")
    func refreshInQuietWindowIsDeferred() {
        var now = Date(timeIntervalSince1970: 1_700_000_000)
        let controller = ExpandedDayHostController(
            initial: makeSnapshot(date: "2026-05-12"),
            quietWindow: 0.4,
            clock: { now }
        )

        controller.noteSelectionChange(to: Date(timeIntervalSince1970: 1_700_000_010))

        now = now.addingTimeInterval(0.1)
        controller.adopt(makeSnapshot(date: "2026-05-13"))

        #expect(controller.displayed.date == "2026-05-12")
    }

    @Test("Pending refresh applies once the quiet window has fully elapsed")
    func pendingRefreshAppliesAfterQuietWindow() {
        var now = Date(timeIntervalSince1970: 1_700_000_000)
        let controller = ExpandedDayHostController(
            initial: makeSnapshot(date: "2026-05-12"),
            quietWindow: 0.4,
            clock: { now }
        )

        controller.noteSelectionChange(to: Date(timeIntervalSince1970: 1_700_000_010))
        now = now.addingTimeInterval(0.1)
        controller.adopt(makeSnapshot(date: "2026-05-13"))

        #expect(controller.displayed.date == "2026-05-12")

        now = now.addingTimeInterval(0.5)
        controller.tick()

        #expect(controller.displayed.date == "2026-05-13")
    }

    @Test("Clearing the selection flushes any pending refresh immediately")
    func clearingSelectionFlushesPending() {
        var now = Date(timeIntervalSince1970: 1_700_000_000)
        let controller = ExpandedDayHostController(
            initial: makeSnapshot(date: "2026-05-12"),
            quietWindow: 0.4,
            clock: { now }
        )

        controller.noteSelectionChange(to: Date(timeIntervalSince1970: 1_700_000_010))
        now = now.addingTimeInterval(0.1)
        controller.adopt(makeSnapshot(date: "2026-05-13"))
        #expect(controller.displayed.date == "2026-05-12")

        controller.noteSelectionChange(to: nil)

        #expect(controller.displayed.date == "2026-05-13")
    }

    @Test("Adopting a refresh after the quiet window has already elapsed applies immediately")
    func refreshAfterQuietWindowAppliesImmediately() {
        var now = Date(timeIntervalSince1970: 1_700_000_000)
        let controller = ExpandedDayHostController(
            initial: makeSnapshot(date: "2026-05-12"),
            quietWindow: 0.4,
            clock: { now }
        )

        controller.noteSelectionChange(to: Date(timeIntervalSince1970: 1_700_000_010))
        now = now.addingTimeInterval(1.0)

        controller.adopt(makeSnapshot(date: "2026-05-13"))

        #expect(controller.displayed.date == "2026-05-13")
    }

    @Test("Selection changes during the quiet window keep refreshes deferred")
    func continuedSelectionKeepsRefreshDeferred() {
        var now = Date(timeIntervalSince1970: 1_700_000_000)
        let controller = ExpandedDayHostController(
            initial: makeSnapshot(date: "2026-05-12"),
            quietWindow: 0.4,
            clock: { now }
        )

        controller.noteSelectionChange(to: Date(timeIntervalSince1970: 1_700_000_010))
        now = now.addingTimeInterval(0.3)
        controller.adopt(makeSnapshot(date: "2026-05-13"))
        #expect(controller.displayed.date == "2026-05-12")

        controller.noteSelectionChange(to: Date(timeIntervalSince1970: 1_700_000_020))
        now = now.addingTimeInterval(0.3)
        controller.tick()

        #expect(controller.displayed.date == "2026-05-12")
    }

    private func makeSnapshot(date: String) -> ExpandedDayHostSnapshot {
        ExpandedDayHostSnapshot(date: date, readings: [], summary: nil)
    }
}
