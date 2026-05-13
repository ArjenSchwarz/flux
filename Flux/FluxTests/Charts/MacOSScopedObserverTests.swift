#if os(macOS)
import Foundation
import FluxCore
import Testing
@testable import Flux

@MainActor
@Suite
struct MacOSScopedObserverTests {
    @Test("Initial tick when active fetches the day endpoint at the scope's date")
    func initialTickFetchesDay() async {
        let now = Date(timeIntervalSince1970: 1_700_000_000)
        let stub = StubObserverAPIClient()
        stub.dayResponse = makeDayResponse(date: "2026-05-01")
        let observer = makeObserver(
            kind: .dayPower,
            scope: .daySpecific(date: dayDate("2026-05-01")),
            api: stub,
            clock: { now }
        )
        observer.appearsActive = true

        await observer.tick()

        #expect(stub.dayFetchCount == 1)
        #expect(stub.lastDayDate == "2026-05-01")
    }

    @Test("Ticks within the 60s window after a fetch do not refetch")
    func subThresholdTicksAreSkipped() async {
        var now = Date(timeIntervalSince1970: 1_700_000_000)
        let stub = StubObserverAPIClient()
        stub.dayResponse = makeDayResponse(date: "2026-05-01")
        let observer = makeObserver(
            kind: .dayPower,
            scope: .daySpecific(date: dayDate("2026-05-01")),
            api: stub,
            clock: { now }
        )
        observer.appearsActive = true
        await observer.tick()
        #expect(stub.dayFetchCount == 1)

        now = now.addingTimeInterval(30)
        await observer.tick()
        #expect(stub.dayFetchCount == 1)

        now = now.addingTimeInterval(29) // 59 s total
        await observer.tick()
        #expect(stub.dayFetchCount == 1)
    }

    @Test("Tick at or beyond 60 s refetches")
    func atThresholdTickRefetches() async {
        var now = Date(timeIntervalSince1970: 1_700_000_000)
        let stub = StubObserverAPIClient()
        stub.dayResponse = makeDayResponse(date: "2026-05-01")
        let observer = makeObserver(
            kind: .dayPower,
            scope: .daySpecific(date: dayDate("2026-05-01")),
            api: stub,
            clock: { now }
        )
        observer.appearsActive = true
        await observer.tick()

        now = now.addingTimeInterval(60)
        await observer.tick()

        #expect(stub.dayFetchCount == 2)
    }

    @Test("Inactive observer never fetches on tick")
    func inactiveObserverPauses() async {
        var now = Date(timeIntervalSince1970: 1_700_000_000)
        let stub = StubObserverAPIClient()
        stub.dayResponse = makeDayResponse(date: "2026-05-01")
        let observer = makeObserver(
            kind: .dayPower,
            scope: .daySpecific(date: dayDate("2026-05-01")),
            api: stub,
            clock: { now }
        )

        await observer.tick()
        #expect(stub.dayFetchCount == 0)

        now = now.addingTimeInterval(120)
        await observer.tick()
        #expect(stub.dayFetchCount == 0)
    }

    @Test("Deactivating after an initial fetch pauses subsequent fetches")
    func deactivationPausesPolling() async {
        var now = Date(timeIntervalSince1970: 1_700_000_000)
        let stub = StubObserverAPIClient()
        stub.dayResponse = makeDayResponse(date: "2026-05-01")
        let observer = makeObserver(
            kind: .dayPower,
            scope: .daySpecific(date: dayDate("2026-05-01")),
            api: stub,
            clock: { now }
        )
        observer.appearsActive = true
        await observer.tick()
        #expect(stub.dayFetchCount == 1)

        observer.appearsActive = false
        now = now.addingTimeInterval(120)
        await observer.tick()

        #expect(stub.dayFetchCount == 1)
    }

    @Test("Re-activating an idle observer triggers an immediate fetch")
    func reactivationRefetches() async {
        var now = Date(timeIntervalSince1970: 1_700_000_000)
        let stub = StubObserverAPIClient()
        stub.dayResponse = makeDayResponse(date: "2026-05-01")
        let observer = makeObserver(
            kind: .dayPower,
            scope: .daySpecific(date: dayDate("2026-05-01")),
            api: stub,
            clock: { now }
        )
        observer.appearsActive = true
        await observer.tick()
        observer.appearsActive = false

        now = now.addingTimeInterval(120)
        observer.appearsActive = true
        await observer.tick()

        #expect(stub.dayFetchCount == 2)
    }

    @Test("Setting a new scope triggers an immediate fetch on the next tick")
    func scopeChangeRefetches() async {
        let now = Date(timeIntervalSince1970: 1_700_000_000)
        let stub = StubObserverAPIClient()
        stub.dayResponse = makeDayResponse(date: "2026-05-01")
        let observer = makeObserver(
            kind: .dayPower,
            scope: .daySpecific(date: dayDate("2026-05-01")),
            api: stub,
            clock: { now }
        )
        observer.appearsActive = true
        await observer.tick()
        #expect(stub.lastDayDate == "2026-05-01")

        stub.dayResponse = makeDayResponse(date: "2026-05-02")
        observer.setScope(.daySpecific(date: dayDate("2026-05-02")))
        await observer.tick()

        #expect(stub.dayFetchCount == 2)
        #expect(stub.lastDayDate == "2026-05-02")
    }

    @Test("Setting the same scope value does not trigger an extra fetch")
    func sameScopeDoesNotRefetch() async {
        let now = Date(timeIntervalSince1970: 1_700_000_000)
        let stub = StubObserverAPIClient()
        stub.dayResponse = makeDayResponse(date: "2026-05-01")
        let scope = ChartScope.daySpecific(date: dayDate("2026-05-01"))
        let observer = makeObserver(kind: .dayPower, scope: scope, api: stub, clock: { now })
        observer.appearsActive = true
        await observer.tick()
        #expect(stub.dayFetchCount == 1)

        observer.setScope(scope)
        await observer.tick()

        #expect(stub.dayFetchCount == 1)
    }

    @Test("History scope drives fetchHistory, not fetchDay")
    func historyScopeUsesHistoryEndpoint() async {
        let now = Date(timeIntervalSince1970: 1_700_000_000)
        let stub = StubObserverAPIClient()
        stub.historyResponse = HistoryResponse(days: [])
        let observer = makeObserver(
            kind: .historySolar,
            scope: .historyRange(days: 14),
            api: stub,
            clock: { now }
        )
        observer.appearsActive = true

        await observer.tick()

        #expect(stub.historyFetchCount == 1)
        #expect(stub.lastHistoryDays == 14)
        #expect(stub.dayFetchCount == 0)
    }

    @Test("Scope change while inactive defers fetch until the observer becomes active")
    func scopeChangeWhileInactiveDefers() async {
        let now = Date(timeIntervalSince1970: 1_700_000_000)
        let stub = StubObserverAPIClient()
        stub.dayResponse = makeDayResponse(date: "2026-05-01")
        let observer = makeObserver(
            kind: .dayPower,
            scope: .daySpecific(date: dayDate("2026-05-01")),
            api: stub,
            clock: { now }
        )

        observer.setScope(.daySpecific(date: dayDate("2026-05-02")))
        await observer.tick()
        #expect(stub.dayFetchCount == 0)

        observer.appearsActive = true
        await observer.tick()

        #expect(stub.dayFetchCount == 1)
        #expect(stub.lastDayDate == "2026-05-02")
    }

    @Test("Successful day fetch updates the dayController's displayed snapshot")
    func dayFetchUpdatesController() async {
        let now = Date(timeIntervalSince1970: 1_700_000_000)
        let stub = StubObserverAPIClient()
        stub.dayResponse = makeDayResponse(date: "2026-05-01")
        let observer = makeObserver(
            kind: .dayPower,
            scope: .daySpecific(date: dayDate("2026-05-01")),
            api: stub,
            clock: { now }
        )
        observer.appearsActive = true

        await observer.tick()

        #expect(observer.dayController?.displayed.date == "2026-05-01")
    }

    @Test("Failed fetch leaves the controller's previous snapshot intact")
    func failedFetchPreservesSnapshot() async {
        let now = Date(timeIntervalSince1970: 1_700_000_000)
        let stub = StubObserverAPIClient()
        stub.dayResponse = nil // throws
        let observer = makeObserver(
            kind: .dayPower,
            scope: .daySpecific(date: dayDate("2026-05-01")),
            api: stub,
            clock: { now }
        )
        observer.appearsActive = true

        await observer.tick()

        #expect(observer.dayController?.displayed.date == "")
    }

    // MARK: - Helpers

    private func makeObserver(
        kind: ChartKind,
        scope: ChartScope,
        api: any FluxAPIClient,
        clock: @escaping () -> Date
    ) -> ChartSceneObserver {
        ChartSceneObserver(
            kind: kind,
            scope: scope,
            api: api,
            pollInterval: 60,
            clock: clock
        )
    }

    private func makeDayResponse(date: String) -> DayDetailResponse {
        DayDetailResponse(
            date: date,
            readings: [],
            summary: nil,
            peakPeriods: nil,
            dailyUsage: nil
        )
    }

    private func dayDate(_ string: String) -> Date {
        DateFormatting.parseDayDate(string) ?? Date(timeIntervalSince1970: 0)
    }
}

private final class StubObserverAPIClient: FluxAPIClient, @unchecked Sendable {
    var historyResponse: HistoryResponse?
    var dayResponse: DayDetailResponse?
    var statusResponse: StatusResponse?

    var historyFetchCount = 0
    var dayFetchCount = 0
    var lastHistoryDays: Int?
    var lastDayDate: String?

    func fetchStatus() async throws -> StatusResponse {
        if let response = statusResponse { return response }
        throw FluxAPIError.notConfigured
    }

    func fetchHistory(days: Int) async throws -> HistoryResponse {
        historyFetchCount += 1
        lastHistoryDays = days
        if let response = historyResponse { return response }
        throw FluxAPIError.notConfigured
    }

    func fetchDay(date: String) async throws -> DayDetailResponse {
        dayFetchCount += 1
        lastDayDate = date
        if let response = dayResponse { return response }
        throw FluxAPIError.notConfigured
    }

    func saveNote(date: String, text _: String) async throws -> NoteResponse {
        NoteResponse(date: date, text: "", updatedAt: nil)
    }
}
#endif
