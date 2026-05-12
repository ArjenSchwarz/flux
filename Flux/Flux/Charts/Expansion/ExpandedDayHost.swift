import FluxCore
import SwiftUI

struct ExpandedDayHostSnapshot {
    var date: String
    var readings: [ParsedReading]
    var summary: DaySummary?

    init(date: String = "", readings: [ParsedReading] = [], summary: DaySummary? = nil) {
        self.date = date
        self.readings = readings
        self.summary = summary
    }
}

@MainActor
@Observable
final class ExpandedDayHostController {
    private(set) var displayed: ExpandedDayHostSnapshot
    let gate: XSelectionQuiescenceGate<ExpandedDayHostSnapshot>
    private let quietWindow: TimeInterval
    private var flushTask: Task<Void, Never>?

    init(
        initial: ExpandedDayHostSnapshot,
        quietWindow: TimeInterval = 0.4,
        clock: @escaping () -> Date = Date.init
    ) {
        self.displayed = initial
        self.quietWindow = quietWindow
        let gate = XSelectionQuiescenceGate<ExpandedDayHostSnapshot>(quietWindow: quietWindow, clock: clock)
        self.gate = gate
        gate.onApply = { [weak self] snapshot in
            self?.displayed = snapshot
        }
    }

    deinit {
        flushTask?.cancel()
    }

    func adopt(_ snapshot: ExpandedDayHostSnapshot) {
        gate.adopt(snapshot)
    }

    func noteSelectionChange(to selection: Date?) {
        if selection == nil {
            gate.noteSelectionCleared()
            flushTask?.cancel()
            flushTask = nil
        } else {
            gate.noteSelectionChange()
            scheduleFlush()
        }
    }

    /// Exposed for tests; production code drives flushing via the
    /// scheduled task created in `noteSelectionChange`.
    func tick() {
        gate.tick()
    }

    private func scheduleFlush() {
        flushTask?.cancel()
        let window = quietWindow
        flushTask = Task { @MainActor [weak self] in
            try? await Task.sleep(for: .seconds(window))
            guard !Task.isCancelled else { return }
            self?.gate.tick()
        }
    }
}

struct ExpandedDayHost: View {
    let kind: ChartKind
    @Bindable var controller: ExpandedDayHostController
    @Binding var selectedDate: Date?

    var body: some View {
        Group {
            switch kind {
            case .dayPower:
                PowerChartView(
                    date: controller.displayed.date,
                    readings: controller.displayed.readings,
                    selectedDate: $selectedDate
                )
            case .dayBatteryCombined:
                BatteryCombinedChartView(
                    date: controller.displayed.date,
                    readings: controller.displayed.readings,
                    summary: controller.displayed.summary,
                    selectedDate: $selectedDate
                )
            case .historySolar, .historyGridUsage, .historyDailyUsage:
                EmptyView()
            }
        }
        .environment(\.chartExpansionAffordanceVisible, false)
        .onChange(of: selectedDate) { _, newValue in
            controller.noteSelectionChange(to: newValue)
        }
    }
}
