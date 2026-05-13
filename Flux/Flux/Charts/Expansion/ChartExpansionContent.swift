import FluxCore
import SwiftUI

/// Cross-platform mount point for an enlarged chart. Owns the scoped
/// observer and renders `ExpandedChartView` with its host controllers.
/// Used inside the iOS full-screen cover and the macOS chart-detail
/// window so both platforms get a real chart instead of the
/// missing-data placeholder.
@MainActor
struct ChartExpansionContent: View {
    let kind: ChartKind

    @Environment(ChartScopeRegistry.self) private var registry
    @Environment(\.chartExpansionFocus) private var focusCoordinator
    #if os(macOS)
    @Environment(\.appearsActive) private var sceneAppearsActive
    #endif

    @State private var observer: ChartSceneObserver?
    @State private var selectedHistoryDate: Date?
    @State private var selectedDayDate: Date?
    @State private var keychainService = KeychainService()

    var body: some View {
        Group {
            if let observer {
                ExpandedChartView(
                    kind: kind,
                    history: observer.historyController,
                    day: observer.dayController,
                    historyRangeDays: Self.historyRangeDays(from: observer.scope),
                    selectedHistoryDate: $selectedHistoryDate,
                    // Bar-tap navigation from the enlarged History view to
                    // Day Detail is intentionally not wired. Decisions 11
                    // and 18 decouple the enlarged presentation from the
                    // main window/cover's navigation state; routing a
                    // dismiss-then-push from here would reintroduce that
                    // coupling. Users dismiss and tap the same bar in the
                    // inline card to navigate. See Decision 21.
                    onSelectHistoryDay: nil,
                    selectedDayDate: $selectedDayDate
                )
            } else {
                ExpandedChartView(kind: kind)
            }
        }
        .onAppear {
            ensureObserver()
            #if !os(macOS)
            // iOS path: the observer is always active for the lifetime
            // of the full-screen cover (no scene `appearsActive` signal
            // to gate on). Activate it and kick off the initial fetch
            // in the same closure that created the observer so the
            // ordering dependency is explicit.
            observer?.appearsActive = true
            Task { await observer?.tick() }
            #endif
        }
        .onDisappear { focusCoordinator.requestRestore(for: kind) }
        .onChange(of: registry.current[kind]) { _, newScope in
            if let newScope { observer?.setScope(newScope) }
        }
        #if os(macOS)
        .onChange(of: sceneAppearsActive, initial: true) { _, isActive in
            observer?.appearsActive = isActive
            if isActive {
                Task { await observer?.tick() }
            }
        }
        #endif
        .task(id: kind) {
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(60))
                guard !Task.isCancelled else { return }
                await observer?.tick()
            }
        }
    }

    private func ensureObserver() {
        guard observer == nil else { return }
        guard let api = ExpandedChartObserverFactory.makeAPIClient(keychainService: keychainService) else { return }
        let scope = ExpandedChartView.resolvedScope(for: kind, in: registry)
        observer = ChartSceneObserver(kind: kind, scope: scope, api: api)
    }

    static func historyRangeDays(from scope: ChartScope) -> Int {
        if case let .historyRange(days) = scope {
            return days
        }
        return ExpandedChartView.defaultHistoryRangeDays
    }
}
