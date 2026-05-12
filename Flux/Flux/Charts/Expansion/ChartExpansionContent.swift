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
                    selectedHistoryDate: $selectedHistoryDate,
                    selectedDayDate: $selectedDayDate
                )
            } else {
                ExpandedChartView(kind: kind)
            }
        }
        .onAppear { ensureObserver() }
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
        #else
        .onAppear {
            observer?.appearsActive = true
            Task { await observer?.tick() }
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
}
