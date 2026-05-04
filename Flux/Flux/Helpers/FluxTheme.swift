import SwiftUI

// V5 redesign tokens. Values are taken verbatim from
// docs/design_handoff_flux_redesign/prototype/screens/v5.jsx and the
// accompanying README.
enum FluxTheme {
    enum Palette {
        static let background = Color(red: 10 / 255, green: 10 / 255, blue: 12 / 255)
        static let panel = Color.white.opacity(0.04)
        static let border = Color.white.opacity(0.07)
        static let primaryText = Color.white
        static let secondaryText = Color(red: 235 / 255, green: 235 / 255, blue: 245 / 255).opacity(0.55)
        static let tertiaryText = Color(red: 235 / 255, green: 235 / 255, blue: 245 / 255).opacity(0.32)

        static let amber = Color(red: 1.0, green: 179 / 255, blue: 71 / 255)
        static let offpeak = Color(red: 90 / 255, green: 200 / 255, blue: 250 / 255)
        static let grid = Color(red: 1.0, green: 107 / 255, blue: 107 / 255)
        static let gridExport = Color(red: 123 / 255, green: 224 / 255, blue: 163 / 255)
        static let battery = Color(red: 191 / 255, green: 90 / 255, blue: 242 / 255)
        static let soc = Color(red: 1.0, green: 208 / 255, blue: 137 / 255)
        static let load = Color(red: 245 / 255, green: 233 / 255, blue: 216 / 255)
        static let night = Color(red: 91 / 255, green: 108 / 255, blue: 1.0)
    }

    enum Metrics {
        static let panelCornerRadius: CGFloat = 18
        static let panelPadding: CGFloat = 18
        static let panelHeroPadding: CGFloat = 18
        static let panelGap: CGFloat = 14
        static let screenHorizontalPadding: CGFloat = 16
        static let screenBottomPadding: CGFloat = 24
        static let statRowVerticalPadding: CGFloat = 9
        static let hairline: CGFloat = 0.5
        static let tabBarCornerRadius: CGFloat = 10
        static let tabBarItemCornerRadius: CGFloat = 8
        static let tabBarPadding: CGFloat = 3
    }

    enum Typography {
        static let pageTitle: Font = .system(size: 30, weight: .semibold)
        static let eyebrow: Font = .system(size: 10, weight: .semibold).monospacedDigit()
        static let panelHeader: Font = .system(size: 11, weight: .bold)
        static let panelHeaderRight: Font = .system(size: 11, design: .monospaced).monospacedDigit()
        static let trioValue: Font = .system(size: 26, weight: .medium).monospacedDigit()
        static let trioUnit: Font = .system(size: 11)
        static let trioLabel: Font = .system(size: 11, weight: .bold)
        static let trioSub: Font = .system(size: 11)
        static let statRowLabel: Font = .system(size: 15).monospacedDigit()
        static let statRowValue: Font = .system(size: 15).monospacedDigit()
        static let statRowSub: Font = .system(size: 10, design: .monospaced)
        static let touName: Font = .system(size: 15)
        static let touTime: Font = .system(size: 12, design: .monospaced)
        static let touValue: Font = .system(size: 15).monospacedDigit()
        static let tabItem: Font = .system(size: 12, weight: .medium)
        static let heroNumber: Font = .system(size: 108, weight: .light)
        static let heroUnit: Font = .system(size: 32, weight: .light)
        static let heroSubline: Font = .system(size: 15)
    }
}

extension View {
    /// Applies the V5 dark background to the screen and ensures content drawn
    /// over it inherits the correct foreground colour.
    func fluxScreenBackground() -> some View {
        background(FluxTheme.Palette.background.ignoresSafeArea())
            .foregroundStyle(FluxTheme.Palette.primaryText)
    }
}
