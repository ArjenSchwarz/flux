#if os(macOS)
import FluxCore
import SwiftUI

struct ChartDetailScene: Scene {
    var body: some Scene {
        WindowGroup("Chart", id: ChartDetailScene.id, for: ChartKind.self) { $kind in
            if let kind {
                ChartExpansionContent(kind: kind)
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
    func macOSChartExpansion(registry: ChartScopeRegistry, focus: ChartExpansionFocusCoordinator) -> some View {
        modifier(MacOSChartExpansionModifier(registry: registry, focus: focus))
    }
}

private struct MacOSChartExpansionModifier: ViewModifier {
    let registry: ChartScopeRegistry
    let focus: ChartExpansionFocusCoordinator
    @Environment(\.openWindow) private var openWindow

    func body(content: Content) -> some View {
        content
            .environment(\.chartExpansion, ChartExpansionAction { kind, scope in
                registry.current[kind] = scope
                openWindow(id: ChartDetailScene.id, value: kind)
            })
            .environment(\.chartExpansionFocus, focus)
    }
}
#endif
