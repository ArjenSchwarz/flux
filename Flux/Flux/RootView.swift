#if !os(macOS)
import SwiftUI

struct RootView: View {
    @Environment(ChartScopeRegistry.self) private var registry
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    // Only the `ChartKind` is persisted across backgrounding (per
    // Decision 15) — the `ChartScope` (range days, specific date) is
    // held in `ChartScopeRegistry`, which is an in-memory `@MainActor`
    // store and therefore lost on cold relaunch. After restoration,
    // `ExpandedChartView.resolvedScope` falls back to
    // `historyRange(days: defaultHistoryRangeDays)` for History kinds
    // and `daySpecific(date: today())` for Day kinds. A user who
    // backgrounded with a 14-day range will see the chart reopen at
    // 7 days; documented gap.
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

    // iPad in Stage Manager renders window controls in the top-leading
    // corner of the scene; on iPad Pro full-screen there's also a wider
    // safe-area inset around the chrome. A ~16-pt leading padding sits
    // right under those controls. Bumping to ~56pt on iPad keeps the
    // xmark visible. iPhone keeps the tighter 8pt.
    private var leadingPadding: CGFloat {
        UIDevice.current.userInterfaceIdiom == .pad ? 56 : 8
    }

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
                .padding(.leading, leadingPadding)

                Spacer()
            }
        }
    }
}
#endif
