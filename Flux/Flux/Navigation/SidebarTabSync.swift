import Foundation

/// Canonical bidirectional mapping between sidebar selection (iPad regular,
/// macOS) and tab-bar selection (iPad compact, iPhone). Used by
/// `AppNavigationView` to keep both bindings in sync across size-class flips
/// without feedback loops.
///
/// Rules:
/// - If `selected` is already the canonical screen for `tab`, the input is a
///   fixed point — output equals input.
/// - Otherwise tab wins: the screen is rewritten to `Screen(tab:)`. This
///   matches the more frequent transition direction (compact → regular,
///   where the user's last tap was on the tab bar).
/// - `.settings` selection has no tab counterpart; the tab is left unchanged.
func syncedState(selected: Screen?, tab: FluxTab) -> (Screen?, FluxTab) {
    if selected == .settings {
        return (.settings, tab)
    }
    if let selected, selected.tab == tab {
        return (selected, tab)
    }
    return (Screen(tab: tab), tab)
}
