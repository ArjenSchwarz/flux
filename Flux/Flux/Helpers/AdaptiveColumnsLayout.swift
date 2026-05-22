import SwiftUI

/// Reflows children into 1, 2, or 3 columns based on the container width,
/// collapsing one column at large accessibility Dynamic Type sizes.
///
/// Width tiers (provisional — see specs/ipad-adaptive-layout/implementation.md):
/// `< 700` → 1 column, `700..<1000` → 2 columns, `≥ 1000` → 3 columns.
/// At `dynamicTypeSize >= .accessibility4` the column count drops by one
/// (never below 1) so cards retain a readable per-column width.
struct AdaptiveColumnsLayout<Content: View>: View {
    let minCardWidth: CGFloat
    let spacing: CGFloat
    @ViewBuilder let content: () -> Content

    @State private var measuredWidth: CGFloat = 0
    @Environment(\.dynamicTypeSize) private var typeSize

    init(
        minCardWidth: CGFloat = 320,
        spacing: CGFloat = FluxTheme.Metrics.panelGap,
        @ViewBuilder content: @escaping () -> Content
    ) {
        self.minCardWidth = minCardWidth
        self.spacing = spacing
        self.content = content
    }

    var body: some View {
        let cols = columnCount(width: measuredWidth, typeSize: typeSize)
        let columns = Array(
            repeating: GridItem(.flexible(), spacing: spacing, alignment: .top),
            count: cols
        )
        LazyVGrid(columns: columns, alignment: .leading, spacing: spacing) {
            content()
        }
        .onGeometryChange(for: CGFloat.self) { proxy in
            proxy.size.width
        } action: { newWidth in
            measuredWidth = newWidth
        }
    }

    func columnCount(width: CGFloat, typeSize: DynamicTypeSize) -> Int {
        let base: Int
        if width >= 1000 {
            base = 3
        } else if width >= 700 {
            base = 2
        } else {
            base = 1
        }
        return typeSize >= .accessibility4 ? max(1, base - 1) : base
    }
}
