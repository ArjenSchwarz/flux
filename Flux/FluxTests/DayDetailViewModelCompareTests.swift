import FluxCore
import Foundation
import Testing
@testable import Flux

// Lifecycle tests for `DayDetailViewModel.updateCompare(enabled:period:)`.
// The Compare fetch is independent of `loadDay()`; cancellation, stale-task
// guards, and synchronous date-resolution failures all need explicit
// coverage because the bug surface is "stale state writes after cancel".
@MainActor @Suite(.serialized)
struct DayDetailViewModelCompareTests {
    @Test
    func updateCompareDisabledSetsStateToOff() async {
        let apiClient = MockCompareAPIClient()
        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        viewModel.updateCompare(enabled: false, period: .yesterday)
        #expect(viewModel.comparisonState == .off)
        #expect(apiClient.fetchDayCallCount == 0)
    }

    @Test
    func updateCompareEnabledFetchesAndSetsReadyOnSuccess() async {
        let apiClient = MockCompareAPIClient()
        apiClient.dayResults["2026-04-14"] = .success(makeResponse(epv: 12.0))

        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        viewModel.updateCompare(enabled: true, period: .yesterday)

        await waitForReady(viewModel)

        guard case .ready(let snapshot, let period) = viewModel.comparisonState else {
            Issue.record("expected .ready, got \(viewModel.comparisonState)")
            return
        }
        #expect(period == .yesterday)
        #expect(snapshot.solar == 12.0)
        #expect(apiClient.lastFetchDate == "2026-04-14")
    }

    @Test
    func sevenDaysAgoUsesMinusSevenOffset() async {
        let apiClient = MockCompareAPIClient()
        apiClient.dayResults["2026-04-08"] = .success(makeResponse(epv: 10.0))

        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        viewModel.updateCompare(enabled: true, period: .sevenDaysAgo)

        await waitForReady(viewModel)

        #expect(apiClient.lastFetchDate == "2026-04-08")
    }

    @Test
    func updateCompareSetsUnavailableWhenFetchThrows() async {
        let apiClient = MockCompareAPIClient()
        apiClient.dayResults["2026-04-14"] = .failure(FluxAPIError.notConfigured)

        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        viewModel.updateCompare(enabled: true, period: .yesterday)

        await waitForUnavailable(viewModel)

        #expect(viewModel.comparisonState == .unavailable(period: .yesterday))
    }

    @Test
    func updateCompareSetsUnavailableWhenResponseIsEmpty() async {
        let apiClient = MockCompareAPIClient()
        // Empty response: no summary, no dailyUsage → ComparisonSnapshot.from returns nil.
        apiClient.dayResults["2026-04-14"] = .success(DayDetailResponse(
            date: "2026-04-14", readings: [], summary: nil, peakPeriods: nil, dailyUsage: nil
        ))

        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        viewModel.updateCompare(enabled: true, period: .yesterday)

        await waitForUnavailable(viewModel)

        #expect(viewModel.comparisonState == .unavailable(period: .yesterday))
    }

    @Test
    func togglingOffCancelsInFlightFetchAndResetsToOff() async {
        let apiClient = MockCompareAPIClient()
        apiClient.dayResults["2026-04-14"] = .success(makeResponse(epv: 12.0))
        apiClient.delaySeconds = 0.5  // hold the fetch open

        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        viewModel.updateCompare(enabled: true, period: .yesterday)
        // Yield to let the task start.
        await Task.yield()

        viewModel.updateCompare(enabled: false, period: .yesterday)
        #expect(viewModel.comparisonState == .off)

        // Wait long enough for the cancelled in-flight fetch to settle —
        // a stale `.ready` would overwrite `.off` if cancellation guards
        // were missing.
        //
        // KNOWN CI FLAKINESS: this is a negative assertion (the stale
        // task must NOT have written), so we can't use the `waitFor`
        // polling pattern here — there's no positive state to wait for.
        // The 700 ms margin is generous on a developer Mac but may need
        // bumping if a loaded CI runner delays the cooperative
        // scheduler beyond the mock's 300 ms delay.
        try? await Task.sleep(nanoseconds: 700_000_000)
        #expect(viewModel.comparisonState == .off)
    }

    @Test
    func periodChangeCancelsInFlightAndKeepsLatestOutcome() async {
        let apiClient = MockCompareAPIClient()
        apiClient.dayResults["2026-04-14"] = .success(makeResponse(epv: 1.0))
        apiClient.dayResults["2026-04-08"] = .success(makeResponse(epv: 7.0))
        apiClient.delaySeconds = 0.3

        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        viewModel.updateCompare(enabled: true, period: .yesterday)
        await Task.yield()

        // Cancel and start a second fetch. The slow first task must not
        // overwrite the second task's state.
        viewModel.updateCompare(enabled: true, period: .sevenDaysAgo)
        await waitForReady(viewModel, timeoutSeconds: 2.0)

        guard case .ready(let snapshot, let period) = viewModel.comparisonState else {
            Issue.record("expected .ready, got \(viewModel.comparisonState)")
            return
        }
        #expect(period == .sevenDaysAgo)
        #expect(snapshot.solar == 7.0)
    }

    @Test
    func dayNavigationReissuesFetchWithNewResolvedDate() async {
        let apiClient = MockCompareAPIClient()
        apiClient.dayResults["2026-04-14"] = .success(makeResponse(epv: 1.0))
        apiClient.dayResults["2026-04-13"] = .success(makeResponse(epv: 9.0))

        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        viewModel.updateCompare(enabled: true, period: .yesterday)
        await waitForReady(viewModel)
        #expect(apiClient.lastFetchDate == "2026-04-14")

        viewModel.navigatePrevious()
        // Caller (DayDetailView) re-issues updateCompare on date change.
        viewModel.updateCompare(enabled: true, period: .yesterday)
        await waitForReady(viewModel)

        #expect(apiClient.lastFetchDate == "2026-04-13")
        guard case .ready(let snapshot, _) = viewModel.comparisonState else {
            Issue.record("expected .ready")
            return
        }
        #expect(snapshot.solar == 9.0)
    }

    @Test
    func corruptDateResolvesSynchronouslyToUnavailable() async {
        let apiClient = MockCompareAPIClient()
        // Construct the view model with a date string that parseDayDate
        // cannot handle.
        let viewModel = DayDetailViewModel(date: "garbage-date", apiClient: apiClient)
        viewModel.updateCompare(enabled: true, period: .yesterday)

        // Synchronous transition: no Task started, no fetch issued.
        #expect(viewModel.comparisonState == .unavailable(period: .yesterday))
        #expect(apiClient.fetchDayCallCount == 0)
    }

    @Test
    func staleSlowTaskCancellationDoesNotOverwriteFastReady() async {
        let apiClient = MockCompareAPIClient()
        apiClient.dayResults["2026-04-14"] = .success(makeResponse(epv: 1.0))
        apiClient.dayResults["2026-04-08"] = .success(makeResponse(epv: 7.0))
        apiClient.delaySeconds = 0.3

        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        viewModel.updateCompare(enabled: true, period: .yesterday)
        await Task.yield()

        // Switch to fast-resolving period; the slow task A is now cancelled
        // but its fetchDay body is still suspended on the simulated delay.
        apiClient.delaySeconds = 0
        viewModel.updateCompare(enabled: true, period: .sevenDaysAgo)
        await waitForReady(viewModel, timeoutSeconds: 2.0)

        // Wait long enough that task A's body would resume if it weren't
        // cancellation-guarded.
        //
        // KNOWN CI FLAKINESS: same constraint as
        // `togglingOffCancelsInFlightFetchAndResetsToOff` — negative
        // assertion, so no polling alternative. 500 ms exceeds the
        // mock's 300 ms delay with margin on a developer Mac but may
        // need bumping on a loaded CI runner.
        try? await Task.sleep(nanoseconds: 500_000_000)

        guard case .ready(let snapshot, let period) = viewModel.comparisonState else {
            Issue.record("expected .ready after slow task A finishes")
            return
        }
        #expect(period == .sevenDaysAgo)
        #expect(snapshot.solar == 7.0)
    }

    // MARK: - Helpers

    private func makeResponse(epv: Double) -> DayDetailResponse {
        DayDetailResponse(
            date: "ignored",
            readings: [],
            summary: DaySummary(
                epv: epv, eInput: 1.0, eOutput: 0.5,
                eCharge: 2.0, eDischarge: 1.5,
                socLow: nil, socLowTime: nil,
                offpeakGridImportKwh: 0.3, offpeakGridExportKwh: nil
            ),
            peakPeriods: nil,
            dailyUsage: nil
        )
    }

    private func waitForReady(_ viewModel: DayDetailViewModel, timeoutSeconds: Double = 1.0) async {
        await waitFor(viewModel, timeoutSeconds: timeoutSeconds) {
            if case .ready = $0 { return true } else { return false }
        }
    }

    private func waitForUnavailable(_ viewModel: DayDetailViewModel, timeoutSeconds: Double = 1.0) async {
        await waitFor(viewModel, timeoutSeconds: timeoutSeconds) {
            if case .unavailable = $0 { return true } else { return false }
        }
    }

    private func waitFor(
        _ viewModel: DayDetailViewModel,
        timeoutSeconds: Double,
        until predicate: (ComparisonState) -> Bool
    ) async {
        let deadline = Date.now.addingTimeInterval(timeoutSeconds)
        while Date.now < deadline {
            if predicate(viewModel.comparisonState) { return }
            try? await Task.sleep(nanoseconds: 10_000_000) // 10 ms
        }
        // Record the timeout so CI failures point at the wait, not at the
        // caller's next assertion against `comparisonState`.
        Issue.record(
            "waitFor timed out after \(timeoutSeconds)s; comparisonState = \(viewModel.comparisonState)"
        )
    }
}

private final class MockCompareAPIClient: FluxAPIClient, @unchecked Sendable {
    var dayResults: [String: Result<DayDetailResponse, Error>] = [:]
    var delaySeconds: Double = 0
    private(set) var lastFetchDate: String?
    private(set) var fetchDayCallCount = 0

    func fetchStatus() async throws -> StatusResponse {
        throw FluxAPIError.notConfigured
    }

    func fetchHistory(days _: Int) async throws -> HistoryResponse {
        throw FluxAPIError.notConfigured
    }

    func fetchDay(date: String) async throws -> DayDetailResponse {
        fetchDayCallCount += 1
        lastFetchDate = date
        let delay = delaySeconds
        if delay > 0 {
            // `try?` is deliberate: swallowing the CancellationError makes
            // the mock fall through to the dictionary lookup so the
            // ViewModel's post-sleep `Task.isCancelled` guard is what
            // discards the result. Production code path goes through the
            // bare `catch` in `updateCompare`, which also maps to
            // `.unavailable` — both routes are correct but the test
            // exercises the guard explicitly.
            try? await Task.sleep(nanoseconds: UInt64(delay * 1_000_000_000))
        }
        guard let result = dayResults[date] else {
            throw FluxAPIError.notConfigured
        }
        return try result.get()
    }

    func saveNote(date _: String, text _: String) async throws -> NoteResponse {
        throw FluxAPIError.notConfigured
    }
}
