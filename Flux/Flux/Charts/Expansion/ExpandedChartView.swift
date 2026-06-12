import FluxCore
import SwiftUI

struct ExpandedChartView: View {
    static let crossFadeDuration: TimeInterval = 0.2

    static let defaultHistoryRangeDays = 7

    let kind: ChartKind
    let history: ExpandedHistoryHostController?
    let day: ExpandedDayHostController?
    let historyRangeDays: Int
    let onSelectHistoryDay: ((String) -> Void)?
    let selectedHistoryDate: Binding<Date?>?
    let selectedDayDate: Binding<Date?>?

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var contentVisible = false

    init(
        kind: ChartKind,
        history: ExpandedHistoryHostController? = nil,
        day: ExpandedDayHostController? = nil,
        historyRangeDays: Int = ExpandedChartView.defaultHistoryRangeDays,
        selectedHistoryDate: Binding<Date?>? = nil,
        onSelectHistoryDay: ((String) -> Void)? = nil,
        selectedDayDate: Binding<Date?>? = nil
    ) {
        self.kind = kind
        self.history = history
        self.day = day
        self.historyRangeDays = historyRangeDays
        self.selectedHistoryDate = selectedHistoryDate
        self.onSelectHistoryDay = onSelectHistoryDay
        self.selectedDayDate = selectedDayDate
    }

    var body: some View {
        // The opacity/animation gate is intentionally inverted from
        // the usual Reduce-Motion pattern:
        //   * Reduce Motion OFF: opacity stays at 1 and animation is
        //     nil, so the system's default cover/window presentation
        //     transition plays unchanged.
        //   * Reduce Motion ON: opacity fades from 0 → 1, replacing
        //     the system's slide/zoom with the explicit cross-fade
        //     required by AC 6.4.
        routedContent
            .opacity(reduceMotion ? (contentVisible ? 1 : 0) : 1)
            .animation(reduceMotion ? .easeInOut(duration: Self.crossFadeDuration) : nil, value: contentVisible)
            .onAppear { contentVisible = true }
    }

    @ViewBuilder
    private var routedContent: some View {
        switch kind.hostKind {
        case .history:
            if let history {
                ExpandedHistoryHost(
                    kind: kind,
                    controller: history,
                    selectedDate: selectedHistoryDate ?? .constant(nil),
                    rangeDays: historyRangeDays,
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
            return .historyRange(.days(defaultHistoryRangeDays))
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
            .accessibilityLabel("Chart data unavailable for \(kind.displayName)")
    }
}
