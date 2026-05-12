#if !os(macOS)
import SwiftUI

struct RootView: View {
    @Environment(ChartScopeRegistry.self) private var registry
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    @SceneStorage("expandedChart") private var expandedRaw: String = ""

    private var expanded: Binding<ChartKind?> {
        Binding(
            get: { ChartKind(rawValue: expandedRaw) },
            set: { expandedRaw = $0?.rawValue ?? "" }
        )
    }

    var body: some View {
        AppNavigationView()
            .environment(\.chartExpansion, ChartExpansionAction { kind, scope in
                registry.current[kind] = scope
                expanded.wrappedValue = kind
            })
            .fullScreenCover(item: expanded) { kind in
                OrientationLandscapeScope {
                    ExpandedChartView(kind: kind)
                }
                .transaction { transaction in
                    if reduceMotion { transaction.disablesAnimations = true }
                }
            }
    }
}
#endif
