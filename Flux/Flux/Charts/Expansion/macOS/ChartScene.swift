#if os(macOS)
import FluxCore
import SwiftUI

struct ChartDetailScene: Scene {
    var body: some Scene {
        WindowGroup("Chart", id: ChartDetailScene.id, for: ChartKind.self) { $kind in
            if let kind {
                ChartDetailContent(kind: kind)
                    .frame(minWidth: ChartDetailScene.minWidth, minHeight: ChartDetailScene.minHeight)
            }
        }
        .defaultSize(width: 900, height: 600)
        .windowResizability(.contentMinSize)
        .defaultLaunchBehavior(.suppressed)
        .windowManagerRole(.associated)
    }

    static let id: String = "chart-detail"
    static let minWidth: CGFloat = 720
    static let minHeight: CGFloat = 480
}

extension View {
    /// Installs the macOS chart-expansion action into the environment.
    /// Tapping an expand button writes the scope to the registry, then
    /// opens (or brings forward) the chart-detail window for the kind.
    func macOSChartExpansion(registry: ChartScopeRegistry) -> some View {
        modifier(MacOSChartExpansionModifier(registry: registry))
    }
}

private struct MacOSChartExpansionModifier: ViewModifier {
    let registry: ChartScopeRegistry
    @Environment(\.openWindow) private var openWindow

    func body(content: Content) -> some View {
        content.environment(\.chartExpansion, ChartExpansionAction { kind, scope in
            registry.current[kind] = scope
            openWindow(id: ChartDetailScene.id, value: kind)
        })
    }
}

@MainActor
private struct ChartDetailContent: View {
    let kind: ChartKind

    @Environment(ChartScopeRegistry.self) private var registry
    @Environment(\.appearsActive) private var windowAppearsActive

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
        .onChange(of: windowAppearsActive, initial: true) { _, isActive in
            observer?.appearsActive = isActive
        }
        .onChange(of: registry.current[kind]) { _, newScope in
            if let newScope { observer?.setScope(newScope) }
        }
        .task(id: kind) {
            while !Task.isCancelled {
                await observer?.tick()
                try? await Task.sleep(for: .seconds(1))
            }
        }
    }

    private func ensureObserver() {
        guard observer == nil else { return }
        guard let api = makeAPIClient() else { return }
        let scope = ExpandedChartView.resolvedScope(for: kind, in: registry)
        observer = ChartSceneObserver(kind: kind, scope: scope, api: api)
    }

    private func makeAPIClient() -> (any FluxAPIClient)? {
        guard let urlString = UserDefaults.fluxAppGroup.apiURL?
            .trimmingCharacters(in: .whitespacesAndNewlines),
              let url = URL(string: urlString),
              keychainService.loadToken()?.isEmpty == false
        else {
            return nil
        }
        return URLSessionAPIClient(baseURL: url, keychainService: keychainService)
    }
}
#endif
