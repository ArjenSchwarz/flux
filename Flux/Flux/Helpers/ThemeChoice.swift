import SwiftUI

/// Appearance options exposed in Settings. Controls the SwiftUI
/// `colorScheme` for the whole app; individual palette tokens still live
/// in `FluxTheme`.
enum ThemeChoice: String, CaseIterable, Identifiable {
    case system
    case light
    case dark

    static let `default`: ThemeChoice = .system

    var id: String { rawValue }

    var displayName: String {
        switch self {
        case .system: "Follow System"
        case .light: "Light"
        case .dark: "Dark"
        }
    }

    var colorScheme: ColorScheme? {
        switch self {
        case .system: nil
        case .light: .light
        case .dark: .dark
        }
    }
}
