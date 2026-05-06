import SwiftUI
#if canImport(UIKit)
import UIKit
#endif
#if canImport(AppKit)
import AppKit
#endif

/// User-chosen font family applied to every text element in the app. The value
/// is the PostScript family name (e.g. "Avenir Next") or an empty string for
/// the system font. Stored under the `appFontFamily` UserDefaults key so the
/// app and macOS Settings scene share it.
enum AppFont {
    nonisolated(unsafe) private static var cachedFamilies: [String]?
    nonisolated private static let familiesLock = NSLock()

    /// Returns the alphabetised list of font families installed on the device.
    /// Filters out families whose names start with `.` (Apple's private/system
    /// faces like `.AppleSystemUIFont`) since they don't render predictably
    /// when invoked by name. Memoised after the first call — on macOS the
    /// underlying `NSFontManager.availableFontFamilies` is non-trivial, so
    /// subsequent opens of Settings reuse the cached list.
    nonisolated static func installedFamilies() -> [String] {
        familiesLock.lock()
        defer { familiesLock.unlock() }
        if let cached = cachedFamilies { return cached }
        let raw: [String]
        #if canImport(UIKit)
        raw = UIFont.familyNames
        #elseif canImport(AppKit)
        raw = NSFontManager.shared.availableFontFamilies
        #else
        raw = []
        #endif
        let result = raw.filter { !$0.hasPrefix(".") }.sorted()
        cachedFamilies = result
        return result
    }

    nonisolated static func isInstalled(_ family: String) -> Bool {
        guard !family.isEmpty else { return false }
        return FontAvailability.isInstalled(family)
    }
}

private enum FontAvailability {
    nonisolated(unsafe) private static var cache: [String: Bool] = [:]
    /// On macOS the entire family list is fetched once and reused. On iOS the
    /// per-family `UIFont.fontNames(forFamilyName:)` lookup is cheap, so we
    /// don't bother caching the full list there.
    #if canImport(AppKit)
    nonisolated(unsafe) private static var familySet: Set<String>?
    #endif
    nonisolated private static let cacheLock = NSLock()

    nonisolated static func isInstalled(_ family: String) -> Bool {
        cacheLock.lock()
        defer { cacheLock.unlock() }
        if let cached = cache[family] { return cached }
        #if canImport(UIKit)
        let installed = !UIFont.fontNames(forFamilyName: family).isEmpty
        #elseif canImport(AppKit)
        if familySet == nil {
            familySet = Set(NSFontManager.shared.availableFontFamilies)
        }
        let installed = familySet?.contains(family) ?? false
        #else
        let installed = false
        #endif
        cache[family] = installed
        return installed
    }
}

private struct AppFontFamilyKey: EnvironmentKey {
    static let defaultValue: String? = nil
}

extension EnvironmentValues {
    /// PostScript family name to use for app text. `nil` (or unresolvable)
    /// means fall back to the system font. Set on the root view from
    /// `@AppStorage("appFontFamily")`.
    var appFontFamily: String? {
        get { self[AppFontFamilyKey.self] }
        set { self[AppFontFamilyKey.self] = newValue }
    }
}

/// Resolves a built-in `Font.TextStyle` to a concrete `Font`, honouring the
/// chosen family and optional weight while preserving Dynamic Type scaling.
enum AppFontResolver {
    /// Returns a fixed-size font in the chosen family — does not scale with
    /// Dynamic Type. Used for chart annotations and pixel-precise UI; for
    /// body / heading text use the `textStyle:` overload, which preserves
    /// Dynamic Type via `Font.custom(_:size:relativeTo:)`.
    nonisolated static func resolve(
        size: CGFloat,
        weight: Font.Weight,
        design: Font.Design = .default,
        family: String?
    ) -> Font {
        if let family, AppFont.isInstalled(family) {
            return .custom(family, size: size).weight(weight)
        }
        return .system(size: size, weight: weight, design: design)
    }

    nonisolated static func resolve(
        textStyle: Font.TextStyle,
        weight: Font.Weight? = nil,
        family: String?
    ) -> Font {
        if let family, AppFont.isInstalled(family) {
            let size = baseSize(for: textStyle)
            var font = Font.custom(family, size: size, relativeTo: textStyle)
            if let weight { font = font.weight(weight) }
            return font
        }
        let base = Font.system(textStyle)
        return weight.map { base.weight($0) } ?? base
    }

    /// Apple's default Dynamic Type sizes at the `.large` content size,
    /// used as the reference size for `Font.custom(_:size:relativeTo:)` so
    /// custom fonts still scale with accessibility settings.
    nonisolated private static let textStyleBaseSizes: [Font.TextStyle: CGFloat] = [
        .largeTitle: 34, .title: 28, .title2: 22, .title3: 20,
        .headline: 17, .body: 17, .callout: 16, .subheadline: 15,
        .footnote: 13, .caption: 12, .caption2: 11
    ]

    nonisolated private static func baseSize(for style: Font.TextStyle) -> CGFloat {
        textStyleBaseSizes[style] ?? 17
    }
}

/// Modifier that applies a family-aware font computed from the chosen
/// `appFontFamily` environment value. Use `.appFont(.headline)` etc. instead
/// of raw `.font(.headline)` so the user's font preference is honoured.
private struct AppFontModifier: ViewModifier {
    @Environment(\.appFontFamily) private var family
    let resolve: @Sendable (String?) -> Font

    func body(content: Content) -> some View {
        content.font(resolve(family))
    }
}

extension View {
    /// Applies a font computed from a closure that receives the current
    /// `appFontFamily` environment value. Call sites can resolve any
    /// `FluxTheme.Typography` entry or hand-tuned size/weight without
    /// pulling the environment value themselves.
    func appFont(_ resolve: @escaping @Sendable (String?) -> Font) -> some View {
        modifier(AppFontModifier(resolve: resolve))
    }

    /// Applies a built-in text style with the user's chosen family.
    func appFont(_ style: Font.TextStyle, weight: Font.Weight? = nil) -> some View {
        appFont { AppFontResolver.resolve(textStyle: style, weight: weight, family: $0) }
    }

    /// Applies a fixed-size font (does not scale with Dynamic Type). Use this
    /// for chart annotations, axis labels, glyph-anchored controls, and other
    /// designed-pixel positions where Dynamic Type scaling would break the
    /// layout. For body text and headings prefer `appFont(_:weight:)` with a
    /// `Font.TextStyle`, which scales via `Font.custom(_:size:relativeTo:)`.
    func appFontSystem(
        size: CGFloat,
        weight: Font.Weight = .regular,
        design: Font.Design = .default
    ) -> some View {
        appFont { AppFontResolver.resolve(size: size, weight: weight, design: design, family: $0) }
    }
}
