#if canImport(UIKit) && !os(macOS)
import CoreFoundation
import SwiftUI

struct ExpandedChartTopHandle: View {
    static let bandHeight: CGFloat = 32
    static let dismissThreshold: CGFloat = 60
    static let pillWidth: CGFloat = 36
    static let pillHeight: CGFloat = 5

    enum DragResolution: Equatable {
        case none
        case dismiss
    }

    let onDismiss: () -> Void

    var body: some View {
        ZStack {
            Color.clear
            Capsule()
                .fill(FluxTheme.Palette.secondaryText.opacity(0.45))
                .frame(width: Self.pillWidth, height: Self.pillHeight)
                .accessibilityHidden(true)
        }
        .frame(maxWidth: .infinity)
        .frame(height: Self.bandHeight)
        .contentShape(Rectangle())
        .gesture(
            DragGesture(minimumDistance: 8)
                .onEnded { value in
                    if Self.resolve(translation: value.translation) == .dismiss {
                        onDismiss()
                    }
                }
        )
        .accessibilityLabel("Dismiss enlarged chart")
        .accessibilityAddTraits(.isButton)
    }

    static func resolve(translation: CGSize) -> DragResolution {
        guard translation.height > abs(translation.width) else { return .none }
        guard translation.height >= dismissThreshold else { return .none }
        return .dismiss
    }
}
#endif
