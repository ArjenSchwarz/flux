# Requirements: iPad Adaptive Layout

## Introduction

Flux already builds for iPad (`TARGETED_DEVICE_FAMILY = "1,2"`) but renders the iPhone V5 tab-bar shell stretched across the full screen, wasting horizontal space in landscape and on larger iPad sizes. This feature gives iPad a dedicated layout: a sidebar-driven `NavigationSplitView` shell at regular horizontal size class — mirroring the existing macOS pattern — and adaptive multi-column content on Dashboard, History, and Day Detail. At compact horizontal size class (Slide Over, narrow Split View) iPad falls back to the existing iPhone V5 shell so nothing breaks at small widths.

## Non-Goals

- Apple Pencil or multi-touch gesture support beyond standard SwiftUI defaults.
- iPad-specific keyboard shortcuts beyond what `FluxKeyboardCommands` already wires up on macOS.
- Stage Manager-specific behaviour beyond standard size-class adaptation.
- Widget extension changes (already cross-platform).
- New screens or features beyond layout adaptation of the existing Dashboard, History, Day Detail, and Settings screens.
- Changes to the iPhone V5 layout or to the macOS shell beyond shared-code refactors required to support iPad.
- A three-column iPad shell (sidebar + History list + Day Detail). The chosen pattern is a two-column sidebar + detail, matching macOS.

## Requirements

### 1. Adaptive Navigation Chrome

**User Story:** As an iPad user, I want the app to use a sidebar when my window is wide enough and the familiar tab bar when it isn't, so that navigation suits the available space.

**Acceptance Criteria:**

1. <a name="1.1"></a>WHEN the iPad app runs at regular horizontal size class, THEN the system SHALL render a two-column `NavigationSplitView` shell with a sidebar containing Dashboard, Today, and History entries and a detail column hosting the selected screen.
2. <a name="1.2"></a>WHEN the iPad app runs at compact horizontal size class (Slide Over, narrow Split View, or any width SwiftUI reports as compact), THEN the system SHALL render the existing iPhone `FluxiOSRoot` shell with `FluxTabBar` and no sidebar.
3. <a name="1.3"></a>The sidebar SHALL NOT display a Settings entry; Settings SHALL remain reachable via the existing per-screen settings affordance (the `onSettingsTap` button on Dashboard, Day Detail, and History headers) and SHALL be presented as a sheet, matching the current iPhone behaviour.
4. <a name="1.4"></a>WHEN the iPad app launches at regular size class with valid credentials, THEN the sidebar SHALL have Dashboard selected and the detail column SHALL display the Dashboard, with no blank detail state.
5. <a name="1.5"></a>WHEN the user selects a different sidebar entry, THEN the detail column's navigation stack SHALL reset to that entry's root view (any previously pushed Day Detail SHALL be popped).
6. <a name="1.6"></a>WHEN the iPad app launches at regular size class without configured credentials (no API URL or no keychain token), THEN it SHALL present the existing unconfigured Settings flow, matching the behaviour today on iPhone and macOS.
7. <a name="1.7"></a>The Today sidebar entry SHALL map to the existing `Screen.today` case and SHALL render `DayDetailView` for today's date as computed by `DateFormatting.todayDateString()`; WHEN the Today entry is already selected and the local date rolls over (midnight crossing while the app is in the foreground), the detail column SHALL update to render Day Detail for the new date on next view appearance or refresh tick, without requiring the user to re-select Today.
8. <a name="1.8"></a>WHEN the iPad app is launched at regular size class via a deep link (e.g. widget tap, `flux://` URL) that resolves to a specific screen via the existing `DeepLinkHandler`, THEN the sidebar SHALL select the corresponding entry and the detail column SHALL render that screen.

### 2. Adaptive Dashboard

**User Story:** As an iPad user, I want the Dashboard to fill the wider detail column with side-by-side content instead of a centered iPhone column, so that I can see more at a glance.

**Acceptance Criteria:**

1. <a name="2.1"></a>WHEN the Dashboard is rendered at regular horizontal size class, THEN the battery hero panel and the live trio panel SHALL be laid out side by side rather than stacked vertically.
2. <a name="2.2"></a>WHEN the Dashboard is rendered at regular horizontal size class, THEN secondary blocks (the panels currently stacked below the live trio on iPhone) SHALL flow into a multi-column arrangement that uses the available width without exceeding it.
3. <a name="2.3"></a>WHEN the Dashboard is rendered at compact horizontal size class on iPad, THEN it SHALL render the existing single-column iPhone layout unchanged.
4. <a name="2.4"></a>The Dashboard at regular size class SHALL continue to auto-refresh on the existing 10-second cadence and SHALL continue to surface the same data values as the iPhone layout (no metric is hidden by the new layout).

### 3. Adaptive History

**User Story:** As an iPad user, I want the History cards spread across the screen rather than stacked, so that I can compare Solar, Grid, Battery, and Daily Usage without scrolling.

**Acceptance Criteria:**

1. <a name="3.1"></a>WHEN the History screen is rendered at regular horizontal size class, THEN the Solar, Grid, Battery, and Daily Usage cards SHALL be arranged in a multi-column grid (at least two columns) rather than a single vertical stack.
2. <a name="3.2"></a>WHEN the History screen is rendered at compact horizontal size class on iPad, THEN it SHALL render the existing single-column iPhone stack unchanged.
3. <a name="3.3"></a>The History stats overview card SHALL continue to honour its existing 4-column-at-regular / 2-column-at-compact behaviour (`HistoryStatsOverviewCard.swift` line 22-28); the iPad adaptive layout SHALL NOT regress that branch.
4. <a name="3.4"></a>WHEN the user taps a day from the History list on iPad at regular size class, THEN Day Detail SHALL be pushed onto the detail column's navigation stack, matching the current iPhone push behaviour.

### 4. Adaptive Day Detail

**User Story:** As an iPad user, I want Day Detail's charts and summary content to sit side by side, so that I can read the day at a glance.

**Acceptance Criteria:**

1. <a name="4.1"></a>WHEN Day Detail is rendered at regular horizontal size class, THEN its chart content and its summary / cards content (Day-in-Five-Blocks, Peak Usage, Daily Usage, Compare controls, notes) SHALL be arranged in a two-column layout where charts occupy one column and summary content occupies the other.
2. <a name="4.2"></a>WHEN Day Detail is rendered at compact horizontal size class on iPad, THEN it SHALL render the existing single-column iPhone layout unchanged.
3. <a name="4.3"></a>WHEN Day Detail is reached via the Today sidebar entry on iPad at regular size class, THEN it SHALL behave identically to Day Detail pushed from History (same content, same chart interactions, same notes flow).
4. <a name="4.4"></a>The day-navigation affordances (previous / next day) and the compare-period controls SHALL remain operable in the regular-size-class layout.

### 5. Settings Screen Adaptation

**User Story:** As an iPad user, I want the Settings sheet to be legible without spanning the full screen width, so that the form is comfortable to read.

**Acceptance Criteria:**

1. <a name="5.1"></a>WHEN the Settings sheet is presented on iPad at regular horizontal size class, THEN its form content SHALL be width-constrained (not stretched edge-to-edge) so individual rows do not exceed a comfortable reading width.
2. <a name="5.2"></a>WHEN the Settings sheet is presented on iPad at compact horizontal size class, THEN it SHALL render the existing iPhone layout unchanged.

### 6. Size-Class Transition Handling

**User Story:** As an iPad user resizing my Split View or rotating my device, I want the layout to switch cleanly between shells without losing my place.

**Acceptance Criteria:**

1. <a name="6.1"></a>WHEN the horizontal size class transitions from compact to regular (e.g. expanding Split View, rotating to landscape on iPad mini), THEN the app SHALL switch from the `FluxiOSRoot` tab-bar shell to the sidebar shell, and the currently active tab SHALL map to the corresponding sidebar entry (Dashboard tab → Dashboard sidebar item, Today tab → Today sidebar item, History tab → History sidebar item).
2. <a name="6.2"></a>WHEN the horizontal size class transitions from regular to compact, THEN the app SHALL switch from the sidebar shell to the `FluxiOSRoot` tab-bar shell, and the currently selected sidebar entry SHALL map to the corresponding tab.
3. <a name="6.3"></a>WHEN the size class transitions in either direction while the user is viewing a pushed Day Detail (opened from History), THEN the resulting shell SHALL preserve the Day Detail context for the in-progress date OR return the user to the History list with the originating date scrolled into view; it SHALL NOT crash, deadlock, display an empty screen, or leave the surviving shell's `NavigationPath` in an invalid state.
4. <a name="6.4"></a>The shell transition SHALL NOT trigger a full re-fetch of Dashboard, History, or Day Detail data; the Dashboard, History, and Day Detail view-model instances SHALL be shared across both shell branches rather than recreated when the size class flips, so cached fetch state and in-flight refresh timers survive the transition.
5. <a name="6.5"></a>Sidebar selection and the detail column's `NavigationPath` SHALL be scene-local: opening a second app window (multi-window) SHALL give the new window an independent sidebar selection and navigation stack; the spec does NOT require persistence of selection across scene termination.

### 7. Cross-Platform Regression Safety

**User Story:** As an iPhone or macOS user, I want my existing layout to keep working unchanged after the iPad work lands.

**Acceptance Criteria:**

1. <a name="7.1"></a>WHEN the app runs on iPhone (any size class), THEN it SHALL render the existing `FluxiOSRoot` + `FluxTabBar` shell with no visible change introduced by this feature.
2. <a name="7.2"></a>WHEN the app runs on macOS, THEN it SHALL render the existing `AppNavigationView` `NavigationSplitView` shell with no visible change introduced by this feature.
3. <a name="7.3"></a>Existing Dashboard, History, Day Detail, and Settings views SHALL render unchanged on iPhone at compact size class and on macOS; the iPad adaptive layouts SHALL be added behind size-class branches that do not alter the existing branches.
4. <a name="7.4"></a>The existing test suite (`make ios-test`, `make macos-test`) SHALL continue to pass, AND a manual smoke check SHALL be performed before merge confirming on iPhone: (a) `FluxTabBar` is visible and switches between Dashboard / Today / History, (b) Settings is reachable from each screen via the existing button and presents as a sheet, (c) History → tap day → Day Detail push works; and on macOS: (d) sidebar shows Dashboard / Today / History, (e) detail column updates on selection, (f) ⌘, opens Settings scene.

### 8. Accessibility and Dynamic Type

**User Story:** As a user with larger text sizes or VoiceOver enabled, I want the iPad layouts to remain readable and operable.

**Acceptance Criteria:**

1. <a name="8.1"></a>WHEN the user has selected any Dynamic Type size up to and including `accessibility3` (AX3), THEN the iPad regular-size-class layouts on Dashboard, History, and Day Detail SHALL remain readable: no truncated metric values in the battery hero or live trio, no clipped card titles, no unreachable controls.
2. <a name="8.2"></a>WHEN any card or panel's preferred width at the current Dynamic Type size exceeds the available column width in a multi-column section, THEN that section SHALL drop to one fewer column (collapsing to a single column at AX4 or larger if needed); the resulting single-column layout SHALL itself remain non-clipping and fully scrollable through `accessibility5` (AX5).
3. <a name="8.3"></a>The sidebar SHALL expose each entry as a discrete accessibility element with a label that matches its visible title; sidebar selection SHALL be operable via VoiceOver.

### 9. Device Coverage

**User Story:** As the developer, I want the feature verified across the iPad lineup before shipping.

**Acceptance Criteria:**

1. <a name="9.1"></a>The implementation SHALL be verified before merge on at least one simulator per size-class boundary — iPad mini (portrait, compact-leaning), iPad Air (landscape), iPad Pro 13" (landscape) — in both orientations where the size class changes with rotation, and on at least one physical iPad available to the developer.
2. <a name="9.2"></a>The implementation SHALL be verified in Slide Over and at the narrowest Split View width on the chosen physical iPad to confirm the iPhone-shell fallback engages.
3. <a name="9.3"></a>Verification results (which simulators / device, which orientations, which Split View widths) SHALL be recorded in `specs/ipad-adaptive-layout/implementation.md`.
