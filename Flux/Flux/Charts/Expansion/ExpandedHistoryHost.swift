import Charts
import FluxCore
import SwiftUI

struct ExpandedHistoryHostSnapshot {
    var solar: [HistoryViewModel.SolarEntry]
    var grid: [HistoryViewModel.GridEntry]
    var dailyUsage: [HistoryViewModel.DailyUsageEntry]
    var summary: HistoryViewModel.PeriodSummary

    init(
        solar: [HistoryViewModel.SolarEntry] = [],
        grid: [HistoryViewModel.GridEntry] = [],
        dailyUsage: [HistoryViewModel.DailyUsageEntry] = [],
        summary: HistoryViewModel.PeriodSummary = .empty
    ) {
        self.solar = solar
        self.grid = grid
        self.dailyUsage = dailyUsage
        self.summary = summary
    }
}

@MainActor
@Observable
final class ExpandedHistoryHostController {
    private(set) var displayed: ExpandedHistoryHostSnapshot
    let gate: HistoryDragGate<ExpandedHistoryHostSnapshot>

    init(initial: ExpandedHistoryHostSnapshot) {
        self.displayed = initial
        let gate = HistoryDragGate<ExpandedHistoryHostSnapshot>()
        self.gate = gate
        gate.onApply = { [weak self] snapshot in
            self?.displayed = snapshot
        }
    }

    func adopt(_ snapshot: ExpandedHistoryHostSnapshot) {
        gate.adopt(snapshot)
    }

    func beginDrag() {
        gate.beginDrag()
    }

    func endDrag() {
        gate.endDrag()
    }
}

struct ExpandedHistoryHost: View {
    let kind: ChartKind
    @Bindable var controller: ExpandedHistoryHostController
    @Binding var selectedDate: Date?
    let rangeDays: Int
    let onSelect: (String) -> Void

    var body: some View {
        Group {
            switch kind {
            case .historySolar:
                HistorySolarCard(
                    entries: controller.displayed.solar,
                    summary: controller.displayed.summary,
                    selectedDate: selectedDate,
                    rangeDays: rangeDays,
                    onSelect: handleSelect
                )
                .simultaneousGesture(dragLifecycleGesture)
            case .historyGridUsage:
                HistoryGridUsageCard(
                    entries: controller.displayed.grid,
                    summary: controller.displayed.summary,
                    selectedDate: selectedDate,
                    rangeDays: rangeDays,
                    onSelect: handleSelect
                )
                .simultaneousGesture(dragLifecycleGesture)
            case .historyDailyUsage:
                HistoryDailyUsageCard(
                    entries: controller.displayed.dailyUsage,
                    summary: controller.displayed.summary,
                    selectedDate: selectedDate,
                    rangeDays: rangeDays,
                    onSelect: handleSelect
                )
                .simultaneousGesture(dragLifecycleGesture)
            case .dayPower, .dayBatteryCombined:
                EmptyView()
            }
        }
        .environment(\.chartExpansionAffordanceVisible, false)
    }

    private func handleSelect(_ dayID: String) {
        onSelect(dayID)
    }

    private var dragLifecycleGesture: some Gesture {
        DragGesture(minimumDistance: 0)
            .onChanged { _ in
                if !controller.gate.dragging {
                    controller.beginDrag()
                }
            }
            .onEnded { _ in
                controller.endDrag()
            }
    }
}
