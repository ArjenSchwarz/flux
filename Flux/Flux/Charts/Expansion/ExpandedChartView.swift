import SwiftUI

struct ExpandedChartView: View {
    let kind: ChartKind
    let history: ExpandedHistoryHostController?
    let day: ExpandedDayHostController?
    let onSelectHistoryDay: ((String) -> Void)?
    let selectedHistoryDate: Binding<Date?>?
    let selectedDayDate: Binding<Date?>?

    init(
        kind: ChartKind,
        history: ExpandedHistoryHostController? = nil,
        day: ExpandedDayHostController? = nil,
        selectedHistoryDate: Binding<Date?>? = nil,
        onSelectHistoryDay: ((String) -> Void)? = nil,
        selectedDayDate: Binding<Date?>? = nil
    ) {
        self.kind = kind
        self.history = history
        self.day = day
        self.selectedHistoryDate = selectedHistoryDate
        self.onSelectHistoryDay = onSelectHistoryDay
        self.selectedDayDate = selectedDayDate
    }

    var body: some View {
        switch kind.hostKind {
        case .history:
            if let history {
                ExpandedHistoryHost(
                    kind: kind,
                    controller: history,
                    selectedDate: selectedHistoryDate ?? .constant(nil),
                    onSelect: onSelectHistoryDay ?? { _ in }
                )
            } else {
                ExpandedChartMissingDataView(kind: kind)
            }
        case .day:
            if let day {
                ExpandedDayHost(
                    kind: kind,
                    controller: day,
                    selectedDate: selectedDayDate ?? .constant(nil)
                )
            } else {
                ExpandedChartMissingDataView(kind: kind)
            }
        }
    }

    @MainActor
    static func resolvedScope(
        for kind: ChartKind,
        in registry: ChartScopeRegistry,
        today: () -> Date = Date.init
    ) -> ChartScope {
        if let registered = registry.current[kind] {
            return registered
        }
        switch kind.hostKind {
        case .history:
            return .historyRange(days: 7)
        case .day:
            return .daySpecific(date: today())
        }
    }
}

private struct ExpandedChartMissingDataView: View {
    let kind: ChartKind

    var body: some View {
        Text("Chart data unavailable")
            .appFont(.headline)
            .foregroundStyle(.secondary)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .accessibilityLabel("Chart data unavailable for \(kind.rawValue)")
    }
}
