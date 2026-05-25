import SwiftUI

/// Central gate for the iPad regular-size-class layout. iPhone Plus/Max
/// landscape reports `.regular` horizontal size class but must keep the
/// iPhone shell (AC 7.1) — the idiom check makes that explicit. macOS
/// always returns `true` so Dashboard, Day Detail, and History pick their
/// adaptive multi-column bodies; the 1-column tier is unreachable at
/// runtime on macOS because `FluxApp`'s scene sets `minWidth=960pt`
/// (T-1342 AC 5.1, AC 2.2).
///
/// Keeping this in one place means the regression guard ships from a
/// single site instead of drifting across `AppNavigationView`,
/// `DashboardView`, `HistoryView`, `DayDetailView`, and `SettingsView`.
enum IPadLayoutGate {
    static func isActive(hSizeClass: UserInterfaceSizeClass?) -> Bool {
        #if os(iOS)
        return UIDevice.current.userInterfaceIdiom == .pad && hSizeClass == .regular
        #elseif os(macOS)
        return true
        #else
        return false
        #endif
    }
}
