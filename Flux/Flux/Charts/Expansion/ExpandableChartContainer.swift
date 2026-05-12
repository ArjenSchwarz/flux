import SwiftUI

struct ExpandableChartContainer<Content: View>: View {
    static var buttonSymbolName: String { "arrow.up.left.and.arrow.down.right" }
    static var buttonInset: CGFloat { 8 }
    static var accessibilityLabel: String { "Expand chart" }

    let kind: ChartKind
    let scopeProvider: () -> ChartScope
    @ViewBuilder var content: () -> Content

    @Environment(\.chartExpansion) private var expansion

    var body: some View {
        content()
            .overlay(alignment: .topTrailing) {
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
            }
    }

    func invoke(action: ChartExpansionAction) {
        action(kind, scope: scopeProvider())
    }
}
