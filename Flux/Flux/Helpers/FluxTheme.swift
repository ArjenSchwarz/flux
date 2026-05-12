import SwiftUI
#if canImport(UIKit)
import UIKit
#endif
#if canImport(AppKit)
import AppKit
#endif

// V5 redesign tokens. Values are taken verbatim from
// docs/design_handoff_flux_redesign/prototype/screens/v5.jsx and the
// accompanying README.
enum FluxTheme {
    enum Palette {
        // Adaptive tokens: chrome and text swap between the dark V5 palette
        // and a light equivalent. Accent / data colours below stay tuned for
        // the dark design until light mode gets its own palette.
        static let background = adaptiveColor(
            light: Color.white,
            dark: Color(red: 10 / 255, green: 10 / 255, blue: 12 / 255)
        )
        static let primaryText = adaptiveColor(
            light: Color.black,
            dark: Color.white
        )
        static let secondaryText = adaptiveColor(
            light: Color.black.opacity(0.6),
            dark: Color(red: 235 / 255, green: 235 / 255, blue: 245 / 255).opacity(0.55)
        )
        static let tertiaryText = adaptiveColor(
            light: Color.black.opacity(0.36),
            dark: Color(red: 235 / 255, green: 235 / 255, blue: 245 / 255).opacity(0.32)
        )
        static let panel = adaptiveColor(
            light: Color.black.opacity(0.04),
            dark: Color.white.opacity(0.04)
        )
        static let border = adaptiveColor(
            light: Color.black.opacity(0.12),
            dark: Color.white.opacity(0.07)
        )
        static let tabBarFill = adaptiveColor(
            light: Color.black.opacity(0.05),
            dark: Color.white.opacity(0.05)
        )
        static let tabBarItemActiveFill = adaptiveColor(
            light: Color.black.opacity(0.10),
            dark: Color.white.opacity(0.12)
        )

        static let amber = Color(red: 1.0, green: 179 / 255, blue: 71 / 255)
        static let offpeak = Color(red: 90 / 255, green: 200 / 255, blue: 250 / 255)
        static let grid = Color(red: 1.0, green: 107 / 255, blue: 107 / 255)
        static let gridExport = Color(red: 123 / 255, green: 224 / 255, blue: 163 / 255)
        static let battery = Color(red: 191 / 255, green: 90 / 255, blue: 242 / 255)
        static let soc = Color(red: 1.0, green: 168 / 255, blue: 102 / 255)
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

    /// V5 typography tokens. Each entry is a function that takes the optional
    /// chosen font family — pulled from `\.appFontFamily` in the environment —
    /// and returns a concrete `Font`. Monospaced-digit / design-specific
    /// entries keep the system font when a custom family is active so digits
    /// stay aligned in charts and stat rows.
    enum Typography {
        nonisolated static func pageTitle(family: String?) -> Font {
            AppFontResolver.resolve(size: 30, weight: .semibold, family: family)
        }
        /// `.monospacedDigit()` selects an alternate glyph variant on the
        /// system font; on custom families that don't ship a tabular-figures
        /// variant the modifier is a no-op. Acceptable here because the
        /// eyebrow shows a colon-separated `HH:mm · MMM d` line where minor
        /// digit-width drift isn't visible.
        nonisolated static func eyebrow(family: String?) -> Font {
            AppFontResolver.resolve(size: 10, weight: .semibold, family: family).monospacedDigit()
        }
        nonisolated static func panelHeader(family: String?) -> Font {
            AppFontResolver.resolve(size: 11, weight: .bold, family: family)
        }
        /// Right-side header label — kept on monospaced digits regardless of
        /// the chosen body family so unit captions stay tabular.
        nonisolated static func panelHeaderRight(family _: String?) -> Font {
            .system(size: 11, design: .monospaced).monospacedDigit()
        }
        /// `.monospacedDigit()` is honoured by the system font; custom
        /// families without a tabular-figures variant render proportional
        /// digits, which can cause minor horizontal drift between the three
        /// trio columns (Solar / House / Grid) as values change. Accepted
        /// trade-off — falling back to the system font here would defeat the
        /// purpose of the picker.
        nonisolated static func trioValue(family: String?) -> Font {
            AppFontResolver.resolve(size: 26, weight: .medium, family: family).monospacedDigit()
        }
        nonisolated static func trioUnit(family: String?) -> Font {
            AppFontResolver.resolve(size: 11, weight: .regular, family: family)
        }
        nonisolated static func trioLabel(family: String?) -> Font {
            AppFontResolver.resolve(size: 11, weight: .bold, family: family)
        }
        nonisolated static func trioSub(family: String?) -> Font {
            AppFontResolver.resolve(size: 11, weight: .regular, family: family)
        }
        nonisolated static func statRowLabel(family: String?) -> Font {
            AppFontResolver.resolve(size: 15, weight: .regular, family: family).monospacedDigit()
        }
        nonisolated static func statRowValue(family: String?) -> Font {
            AppFontResolver.resolve(size: 15, weight: .regular, family: family).monospacedDigit()
        }
        /// Sub-caption next to stat row labels stays on the monospaced system
        /// font so trailing units like "kWh" align across rows.
        nonisolated static func statRowSub(family _: String?) -> Font {
            .system(size: 10, design: .monospaced)
        }
        nonisolated static func touName(family: String?) -> Font {
            AppFontResolver.resolve(size: 15, weight: .regular, family: family)
        }
        /// Time-of-use timestamps stay on the monospaced system font so the
        /// `HH:mm` ranges stay tabular.
        nonisolated static func touTime(family _: String?) -> Font {
            .system(size: 12, design: .monospaced)
        }
        nonisolated static func touValue(family: String?) -> Font {
            AppFontResolver.resolve(size: 15, weight: .regular, family: family).monospacedDigit()
        }
        nonisolated static func tabItem(family: String?) -> Font {
            AppFontResolver.resolve(size: 12, weight: .medium, family: family)
        }
        nonisolated static func heroNumber(family: String?) -> Font {
            AppFontResolver.resolve(size: 108, weight: .light, family: family)
        }
        nonisolated static func heroUnit(family: String?) -> Font {
            AppFontResolver.resolve(size: 32, weight: .light, family: family)
        }
        nonisolated static func heroSubline(family: String?) -> Font {
            AppFontResolver.resolve(size: 15, weight: .regular, family: family)
        }
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

private func adaptiveColor(light: Color, dark: Color) -> Color {
    #if canImport(UIKit)
    return Color(uiColor: UIColor { traits in
        traits.userInterfaceStyle == .light ? UIColor(light) : UIColor(dark)
    })
    #elseif canImport(AppKit)
    return Color(nsColor: NSColor(name: nil) { appearance in
        let isDark = appearance.bestMatch(from: [.darkAqua, .vibrantDark]) != nil
        return isDark ? NSColor(dark) : NSColor(light)
    })
    #else
    // Fallback for platforms without UIKit/AppKit (watchOS, visionOS, Linux):
    // no dynamic provider is available, so collapse to the dark value.
    return dark
    #endif
}
