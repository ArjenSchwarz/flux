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

    init(
        initial: ExpandedDayHostSnapshot,
        quietWindow: TimeInterval = 0.4,
        clock: @escaping () -> Date = Date.init
    ) {
        self.displayed = initial
        let gate = XSelectionQuiescenceGate<ExpandedDayHostSnapshot>(quietWindow: quietWindow, clock: clock)
        self.gate = gate
        gate.onApply = { [weak self] snapshot in
            self?.displayed = snapshot
        }
    }

    func adopt(_ snapshot: ExpandedDayHostSnapshot) {
        gate.adopt(snapshot)
    }

    func noteSelectionChange(to selection: Date?) {
        if selection == nil {
            gate.noteSelectionCleared()
        } else {
            gate.noteSelectionChange()
        }
    }

    func tick() {
        gate.tick()
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
        .onChange(of: selectedDate) { _, newValue in
            controller.noteSelectionChange(to: newValue)
        }
        .task(id: ObjectIdentifier(controller)) {
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: 100_000_000)
                controller.tick()
            }
        }
    }
}
