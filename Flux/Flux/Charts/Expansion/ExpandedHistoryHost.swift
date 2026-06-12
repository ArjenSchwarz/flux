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
    /// Day count derived from the observer's scope. The cards below receive a
    /// nominal `.days` query built from it: their expansion affordance is
    /// disabled in this host (see the environment override), so the query is
    /// never used to spawn a further expansion.
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
                    periodQuery: .days(rangeDays),
                    onSelect: handleSelect
                )
                .simultaneousGesture(dragLifecycleGesture)
            case .historyGridUsage:
                HistoryGridUsageCard(
                    entries: controller.displayed.grid,
                    summary: controller.displayed.summary,
                    selectedDate: selectedDate,
                    periodQuery: .days(rangeDays),
                    onSelect: handleSelect
                )
                .simultaneousGesture(dragLifecycleGesture)
            case .historyDailyUsage:
                HistoryDailyUsageCard(
                    entries: controller.displayed.dailyUsage,
                    summary: controller.displayed.summary,
                    selectedDate: selectedDate,
                    periodQuery: .days(rangeDays),
                    onSelect: handleSelect
                )
                .simultaneousGesture(dragLifecycleGesture)
            case .dayPower, .dayBatteryCombined:
                EmptyView()
                    .onAppear {
                        assertionFailure("ExpandedHistoryHost received a day kind: \(kind)")
                    }
            }
        }
        .environment(\.chartExpansionAffordanceVisible, false)
    }

    private func handleSelect(_ dayID: String) {
        onSelect(dayID)
    }

    private var dragLifecycleGesture: some Gesture {
        // 8 pt matches UIKit's default drag-recognition slop and
        // distinguishes intentional chart scrubs from taps on chart
        // annotations / bar-tap navigation. `minimumDistance: 0` would
        // open and immediately close the gate on every tap, which can
        // flush a pending snapshot if a refresh lands in the same
        // frame.
        DragGesture(minimumDistance: 8)
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
