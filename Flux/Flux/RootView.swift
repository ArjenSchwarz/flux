#if !os(macOS)
import SwiftUI

struct RootView: View {
    @Environment(ChartScopeRegistry.self) private var registry
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    @SceneStorage("expandedChart") private var expandedRaw: String = ""
    @State private var focusCoordinator = ChartExpansionFocusCoordinator()

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
            .environment(\.chartExpansionFocus, focusCoordinator)
            .fullScreenCover(item: expanded) { kind in
                OrientationLandscapeScope {
                    ExpandedChartView(kind: kind)
                }
                .transaction { transaction in
                    if reduceMotion { transaction.disablesAnimations = true }
                }
            }
            .onChange(of: expanded.wrappedValue) { oldValue, newValue in
                if let oldValue, newValue == nil {
                    focusCoordinator.requestRestore(for: oldValue)
                }
            }
    }
}
#endif
