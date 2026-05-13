import SwiftUI

struct ExpandableChartContainer<Content: View>: View {
    static var buttonSymbolName: String { "arrow.up.left.and.arrow.down.right" }
    static var buttonInset: CGFloat { 8 }
    static var accessibilityLabel: String { "Expand chart" }

    let kind: ChartKind
    let scopeProvider: () -> ChartScope
    @ViewBuilder var content: () -> Content

    @Environment(\.chartExpansion) private var expansion
    @Environment(\.chartExpansionAffordanceVisible) private var affordanceVisible
    @Environment(\.chartExpansionFocus) private var focusCoordinator

    @AccessibilityFocusState private var isExpandButtonFocused: Bool

    var body: some View {
        content()
            .overlay(alignment: .topTrailing) {
                if affordanceVisible {
                    Button {
                        invoke(action: expansion)
                    } label: {
                        Image(systemName: Self.buttonSymbolName)
                            .font(.title3.weight(.medium))
                            .foregroundStyle(FluxTheme.Palette.secondaryText)
                    }
                    .buttonStyle(.plain)
                    .padding(Self.buttonInset)
                    .accessibilityLabel(Self.accessibilityLabel)
                    .accessibilityFocused($isExpandButtonFocused)
                }
            }
            .onChange(of: focusCoordinator.pendingRequest) { _, newRequest in
                guard affordanceVisible, let newRequest, newRequest.kind == kind else { return }
                isExpandButtonFocused = true
                focusCoordinator.consume(newRequest)
            }
    }

    func invoke(action: ChartExpansionAction) {
        action(kind, scope: scopeProvider())
    }
}
