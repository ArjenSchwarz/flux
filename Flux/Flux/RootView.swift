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
                ChartExpansionCover(kind: kind) {
                    expanded.wrappedValue = nil
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

private struct ChartExpansionCover: View {
    let kind: ChartKind
    let onClose: () -> Void

    var body: some View {
        OrientationLandscapeScope {
            VStack(spacing: 0) {
                ExpandedChartTopBar(onClose: onClose)
                ChartExpansionContent(kind: kind)
            }
        }
    }
}

private struct ExpandedChartTopBar: View {
    let onClose: () -> Void

    var body: some View {
        ZStack {
            ExpandedChartTopHandle(onDismiss: onClose)
            HStack {
                Button {
                    onClose()
                } label: {
                    Image(systemName: "xmark")
                        .font(.title3.weight(.medium))
                        .foregroundStyle(FluxTheme.Palette.primaryText)
                        .padding(8)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Close enlarged chart")
                .padding(.leading, 8)

                Spacer()
            }
        }
    }
}
#endif
