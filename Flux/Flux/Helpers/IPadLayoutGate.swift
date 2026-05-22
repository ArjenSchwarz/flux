import SwiftUI

/// Central gate for the iPad regular-size-class layout. iPhone Plus/Max
/// landscape reports `.regular` horizontal size class but must keep the
/// iPhone shell (AC 7.1) — the idiom check makes that explicit. macOS
/// never qualifies; the gate returns `false` so the existing
/// `NavigationSplitView` shell and single-column screen layouts remain
/// untouched (AC 7.2).
///
/// Keeping this in one place means the regression guard ships from a
/// single site instead of drifting across `AppNavigationView`,
/// `DashboardView`, `HistoryView`, `DayDetailView`, and `SettingsView`.
enum IPadLayoutGate {
    static func isActive(hSizeClass: UserInterfaceSizeClass?) -> Bool {
        #if os(iOS)
        return UIDevice.current.userInterfaceIdiom == .pad && hSizeClass == .regular
        #else
        return false
        #endif
    }
}
