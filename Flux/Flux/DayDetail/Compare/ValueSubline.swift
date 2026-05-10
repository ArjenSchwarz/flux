import SwiftUI

/// Shared sub-line view used by `FluxStatRow` and `DayInFiveBlocksPanel`
/// to render the per-row delta (or reserve its height while loading or
/// in fallback). The three-state contract is encoded in `SublineContent`.
struct ValueSubline: View {
    let content: SublineContent

    var body: some View {
        switch content {
        case .hidden:
            EmptyView()
        case .reserved:
            // U+00A0 (non-breaking space) keeps the line height while
            // rendering nothing visible.
            Text("\u{00A0}")
                .appFont(FluxTheme.Typography.touTime)
                .foregroundStyle(FluxTheme.Palette.tertiaryText)
                .accessibilityHidden(true)
        case .text(let value):
            Text(value)
                .appFont(FluxTheme.Typography.touTime)
                .foregroundStyle(FluxTheme.Palette.tertiaryText)
                .accessibilityHidden(true)
        }
    }
}
