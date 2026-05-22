import Foundation

/// Pure helpers driving the bidirectional sidebar ↔ tab sync that
/// `AppNavigationView` runs across iPad size-class transitions. Two
/// trigger-aware functions instead of one symmetric reducer — the
/// onChange handler that fired tells us which side is canonical:
///
/// - The `selectedScreen` handler calls ``mappedIosTab`` to derive the
///   new tab from the new selection.
/// - The `iosTab` handler calls ``mappedSelectedScreen`` to derive the
///   new selection from the new tab.
///
/// Both helpers preserve `.settings` (no `FluxTab` counterpart) and
/// guard against feedback loops by returning the current value when
/// the other side already matches.

/// Computes the `FluxTab` that should mirror the given selected screen.
/// Returns `currentTab` unchanged when `selected` has no tab counterpart
/// (`.settings` or `nil`) — leaving the tab alone in that case keeps the
/// user out of an inconsistent state.
func mappedIosTab(for selected: Screen?, currentTab: FluxTab) -> FluxTab {
    selected?.tab ?? currentTab
}

/// Computes the `Screen?` that should mirror the given tab. Preserves a
/// `.settings` selection so a tab change does not knock the user out of
/// Settings; otherwise maps via ``Screen/init(tab:)``.
func mappedSelectedScreen(for tab: FluxTab, currentSelection: Screen?) -> Screen? {
    if currentSelection == .settings { return .settings }
    return Screen(tab: tab)
}
