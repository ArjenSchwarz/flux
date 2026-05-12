#if os(macOS)
import FluxCore
import Foundation
import Observation

/// Scope-aware data observer for the macOS chart-detail scene.
///
/// Each enlarged window owns its own observer that polls `FluxAPIClient`
/// at the inactive 60-second tier when `appearsActive` is true, pauses
/// when the window is not visible, and switches its fetch target when
/// the registered scope changes. The observer feeds its result into the
/// host controller (`ExpandedHistoryHostController` or
/// `ExpandedDayHostController`) read by `ExpandedChartView`.
@MainActor
@Observable
final class ChartSceneObserver {
    let kind: ChartKind
    private(set) var scope: ChartScope
    private(set) var historyController: ExpandedHistoryHostController?
    private(set) var dayController: ExpandedDayHostController?

    /// True while the window is visible. Polling only runs while active;
    /// flipping from false to true triggers an immediate refresh.
    var appearsActive: Bool {
        didSet {
            guard appearsActive, !oldValue else { return }
            pendingFetch = true
        }
    }

    private let api: any FluxAPIClient
    private let pollInterval: TimeInterval
    private let clock: () -> Date
    private var lastFetched: Date = .distantPast
    private var pendingFetch: Bool

    init(
        kind: ChartKind,
        scope: ChartScope,
        api: any FluxAPIClient,
        pollInterval: TimeInterval = 60,
        clock: @escaping () -> Date = Date.init
    ) {
        self.kind = kind
        self.scope = scope
        self.api = api
        self.pollInterval = pollInterval
        self.clock = clock
        self.appearsActive = false
        self.pendingFetch = true

        switch kind.hostKind {
        case .history:
            historyController = ExpandedHistoryHostController(initial: ExpandedHistoryHostSnapshot())
        case .day:
            dayController = ExpandedDayHostController(initial: ExpandedDayHostSnapshot())
        }
    }

    func setScope(_ newScope: ChartScope) {
        guard newScope != scope else { return }
        scope = newScope
        pendingFetch = true
    }

    /// Called once per polling iteration. Issues a refresh if the
    /// observer is active and either a fetch is pending (init / scope
    /// change / reactivation) or the poll interval has elapsed.
    func tick() async {
        guard appearsActive else { return }
        let now = clock()
        let elapsed = now.timeIntervalSince(lastFetched)
        guard pendingFetch || elapsed >= pollInterval else { return }
        await refresh()
    }

    /// Forces an immediate fetch regardless of timing.
    func refresh() async {
        pendingFetch = false
        lastFetched = clock()

        switch scope {
        case let .historyRange(days):
            await fetchHistory(days: days)
        case let .daySpecific(date):
            await fetchDay(at: date)
        }
    }

    private func fetchHistory(days: Int) async {
        do {
            let response = try await api.fetchHistory(days: days)
            let derived = HistoryViewModel.DerivedState(days: response.days, now: clock())
            historyController?.adopt(
                ExpandedHistoryHostSnapshot(
                    solar: derived.solar,
                    grid: derived.grid,
                    dailyUsage: derived.dailyUsage,
                    summary: derived.summary
                )
            )
        } catch {
            // Leave the previously-displayed snapshot in place on failure.
        }
    }

    private func fetchDay(at date: Date) async {
        let dateString = DateFormatting.dayDateString(from: date)
        do {
            let response = try await api.fetchDay(date: dateString)
            let parsed = parseReadings(response.readings)
            dayController?.adopt(
                ExpandedDayHostSnapshot(
                    date: response.date,
                    readings: parsed,
                    summary: response.summary
                )
            )
        } catch {
            // Leave the previously-displayed snapshot in place on failure.
        }
    }

    private func parseReadings(_ readings: [TimeSeriesPoint]) -> [ParsedReading] {
        var parsed: [ParsedReading] = []
        parsed.reserveCapacity(readings.count)
        for reading in readings {
            guard let parsedDate = DateFormatting.parseTimestamp(reading.timestamp) else { continue }
            parsed.append(ParsedReading(id: reading.id, date: parsedDate, point: reading))
        }
        return parsed
    }
}
#endif
