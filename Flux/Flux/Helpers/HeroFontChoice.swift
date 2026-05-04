import SwiftUI
#if canImport(UIKit)
import UIKit
#endif
#if canImport(AppKit)
import AppKit
#endif

/// Font options exposed in Settings for the Dashboard hero numeral. The body
/// and values across the app stay on San Francisco; this choice only affects
/// the giant battery percentage on the Dashboard.
///
/// Custom fonts are looked up by PostScript name; if the bundle doesn't ship
/// the font (deliberate — fonts are added as resources separately), the
/// `font(size:)` helper falls back to San Francisco so the UI never breaks.
enum HeroFontChoice: String, CaseIterable, Identifiable {
    case geist
    case sanFrancisco
    case ibmPlexSans
    case inter
    case fraunces
    case newsreader
    case instrumentSans
    case jetBrainsMono

    static let `default`: HeroFontChoice = .geist

    var id: String { rawValue }

    var displayName: String {
        switch self {
        case .geist: "Geist"
        case .sanFrancisco: "San Francisco"
        case .ibmPlexSans: "IBM Plex Sans"
        case .inter: "Inter"
        case .fraunces: "Fraunces"
        case .newsreader: "Newsreader"
        case .instrumentSans: "Instrument Sans"
        case .jetBrainsMono: "JetBrains Mono"
        }
    }

    /// PostScript family name the app looks up at runtime. `nil` means
    /// "use the system default" (San Francisco).
    var postScriptFamily: String? {
        switch self {
        case .geist: "Geist"
        case .sanFrancisco: nil
        case .ibmPlexSans: "IBM Plex Sans"
        case .inter: "Inter"
        case .fraunces: "Fraunces"
        case .newsreader: "Newsreader"
        case .instrumentSans: "Instrument Sans"
        case .jetBrainsMono: "JetBrains Mono"
        }
    }

    /// Returns a `Font` for the requested size and weight, falling back to the
    /// system font when the chosen family isn't installed.
    func font(size: CGFloat, weight: Font.Weight) -> Font {
        if let family = postScriptFamily, FontAvailability.isInstalled(family) {
            return .custom(family, size: size).weight(weight)
        }
        return .system(size: size, weight: weight)
    }
}

private enum FontAvailability {
    nonisolated(unsafe) private static var cache: [String: Bool] = [:]
    private static let cacheLock = NSLock()

    static func isInstalled(_ family: String) -> Bool {
        cacheLock.lock()
        defer { cacheLock.unlock() }
        if let cached = cache[family] { return cached }
        #if canImport(UIKit)
        let installed = !UIFont.fontNames(forFamilyName: family).isEmpty
        #elseif canImport(AppKit)
        let installed = NSFontManager.shared.availableFontFamilies.contains(family)
        #else
        let installed = false
        #endif
        cache[family] = installed
        return installed
    }
}
