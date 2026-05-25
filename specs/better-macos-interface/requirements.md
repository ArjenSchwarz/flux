# Requirements: Better macOS Interface (T-1342)

## Introduction

The macOS app currently runs the iPhone-style single-column body inside its NavigationSplitView shell, even though iPad now has a fluid multi-column layout that reflows Dashboard, Day Detail, and History panels side-by-side at wider widths. Day Detail also renders a custom in-content date header (`DayNavigationHeader` — chevrons + centered date), and Dashboard renders an in-content eyebrow + title block (`legacyHeader`), both of which feel out of place on macOS where the navigation bar and window toolbar already carry that role. This feature ports the iPad's adaptive multi-column layout to macOS and replaces the in-content header blocks on Dashboard and Day Detail with native macOS chrome.

## Non-Goals

- Changing the iPhone layout, iPad layout, or any non-Dashboard / non-DayDetail / non-History macOS screen (Settings retains its current macOS presentation via `MacUnconfiguredView` and the existing `⌘,` Settings scene).
- Introducing per-platform `AdaptiveColumnsLayout` breakpoints — macOS reuses the iPad thresholds (1 / 2 / 3 columns at < 700pt / 700–1000pt / ≥ 1000pt).
- Adding a 4th column tier for ultra-wide macOS displays.
- Replacing the existing `AppNavigationView` NavigationSplitView shell with `FluxiPadRoot` or any new shell.
- Changing the Day Detail layout topology — macOS reuses the iPad fixed two-column `Grid` (summary cards left, charts right).
- Adding a calendar-popover date picker on macOS Day Detail (deferred to a future iteration).
- Changing Day Detail navigation semantics (`navigatePrevious` / `navigateNext`, today-is-max-date guard, midnight rollover).
- Removing or changing History's date-range header — only the Dashboard `legacyHeader` and the Day Detail `DayNavigationHeader` are in scope.
- Replacing the "Now · HH:MM · MMM d" eyebrow with an alternative freshness indicator on macOS.
- Changing macOS scene/window state semantics (per-window `@SceneStorage` and credential reload behavior remain as today).
- Renaming `IPadLayoutGate` — name retained for this iteration; rename considered as a non-blocking follow-up.

## Requirements

### 1. macOS Adaptive Multi-Column Body

**User Story:** As a macOS user, I want Dashboard and History to reflow panels side-by-side when my window is wide, so that I see more information at once without scrolling.

**Acceptance Criteria:**

1. <a name="1.1"></a>The Dashboard SHALL render the adaptive multi-column body on macOS, matching the iPad `dashboardContentRegular` topology (hero + trio reflow via `AdaptiveColumnsLayout`; summary and battery panels full-width).
2. <a name="1.2"></a>The History screen SHALL render the adaptive multi-column body on macOS, matching the iPad `historyContentRegular` topology (overview full-width; solar / grid / daily-usage cards reflow via `AdaptiveColumnsLayout`).
3. <a name="1.3"></a>The adaptive layout SHALL use the existing iPad breakpoints unchanged: 1 column below 700pt, 2 columns 700–1000pt (exclusive of 1000pt), 3 columns at or above 1000pt. Unit tests for `AdaptiveColumnsLayout` SHALL verify the 699 / 700 / 999 / 1000 pt boundaries cross-platform; the 699pt boundary is not reachable on macOS at runtime (see [2.2](#2.2)) and is exercised only through the unit test.
4. <a name="1.4"></a>The macOS adaptive body SHALL re-tier without overflow or chrome corruption during live window resize that crosses the 1000pt detail-column boundary.
5. <a name="1.5"></a>The macOS adaptive body SHALL drop one column tier when `dynamicTypeSize >= .accessibility4`, matching iPad behavior, and SHALL never collapse below 1 column.

### 2. macOS Day Detail Body and Window Sizing

**User Story:** As a macOS user, I want Day Detail to show the summary cards and charts side-by-side, so that I can compare numbers against the charts without scrolling between sections.

**Acceptance Criteria:**

1. <a name="2.1"></a>The Day Detail screen SHALL render the iPad two-column `Grid` body on macOS (summary cards left column, power and battery charts stacked in right column).
2. <a name="2.2"></a>The macOS app scene SHALL set `minWidth: 960pt` and `minHeight: 600pt`, so the detail column always exceeds the 700pt 2-column threshold for Day Detail and for `AdaptiveColumnsLayout` on Dashboard / History. As a consequence, the macOS adaptive bodies start at the 2-column tier and never reach the 1-column tier at runtime.
3. <a name="2.3"></a>The Day Detail two-column `Grid` SHALL render without horizontal clipping at the largest supported `dynamicTypeSize` (`.accessibility5`) at the minimum window width.

### 3. Remove Dashboard In-Content Header on macOS

**User Story:** As a macOS user, I want the Dashboard to not duplicate the navigation bar title with an in-content eyebrow and "Battery" header, so that the screen reads as a single titled view.

**Acceptance Criteria:**

1. <a name="3.1"></a>The macOS Dashboard SHALL NOT render the `legacyHeader` block (the eyebrow + "Battery" title VStack currently shown above the panels).
2. <a name="3.2"></a>The macOS Dashboard navigation bar / window title SHALL display "Dashboard".
3. <a name="3.3"></a>The iPhone Dashboard (which keeps `FluxScreenHeader`) and the iPad Dashboard (which already skips both headers) SHALL be unaffected.

### 4. Replace Day Detail In-Content Date Header on macOS

**User Story:** As a macOS user, I want the Day Detail date and prev/next navigation to live in native window chrome instead of as a bar inside the content, so that scrolling and content focus aren't disrupted.

**Acceptance Criteria:**

1. <a name="4.1"></a>The macOS Day Detail screen SHALL NOT render the `DayNavigationHeader` (the in-content chevrons + centered date row).
2. <a name="4.2"></a>The macOS Day Detail navigation title SHALL display `"Today"` when `viewModel.isToday` is true, otherwise the date formatted with `DayDetailEyebrow.full` (the same formatter the existing eyebrow uses).
3. <a name="4.3"></a>The macOS Day Detail navigation title SHALL be a reactive read of `viewModel.isToday` and `viewModel.date`, so it updates when either changes (including the midnight rollover handled by `DayDetailViewModel.setDate(_:)`).
4. <a name="4.4"></a>The macOS Day Detail toolbar SHALL include, in a trailing toolbar group, a previous-day button and a next-day button that invoke `viewModel.navigatePrevious()` and `viewModel.navigateNext()` respectively.
5. <a name="4.5"></a>The previous-day toolbar button SHALL carry `accessibilityLabel("Previous day")`; the next-day toolbar button SHALL carry `accessibilityLabel("Next day")`.
6. <a name="4.6"></a>The next-day toolbar button SHALL be disabled when `viewModel.isToday` is true, matching the existing in-content header behavior; the previous-day button SHALL remain enabled.
7. <a name="4.7"></a>The existing ←/→ keyboard navigation on macOS Day Detail SHALL continue to work unchanged.
8. <a name="4.8"></a>The new toolbar items SHALL be gated `#if os(macOS)` so the iPhone Day Detail and the iPad Day Detail do not receive duplicate or unintended chrome.
9. <a name="4.9"></a>The iPhone Day Detail (which keeps `DayNavigationHeader` in `dayDetailContent`) and the iPad Day Detail (which keeps `DayNavigationHeader` in `dayDetailContentRegular`) SHALL be unaffected.

### 5. Layout Gate Behavior

**User Story:** As a developer, I want a single source of truth for "should this view use the multi-column adaptive body" that includes macOS, so that Dashboard, Day Detail, and History select consistent branches without duplicating platform checks.

**Acceptance Criteria:**

1. <a name="5.1"></a>On macOS, the shared layout gate SHALL cause Dashboard, Day Detail, and History to select their adaptive multi-column branches.
2. <a name="5.2"></a>On iOS, the shared layout gate SHALL preserve current behavior: iPad with `hSizeClass == .regular` returns the adaptive branch; iPhone (any size class) and iPad in compact (e.g., Slide Over or split-screen narrow) return the existing single-column branch; iPhone Plus/Max landscape (regular hSizeClass but non-pad idiom) keeps the iPhone branch.
3. <a name="5.3"></a>The design document SHALL include a per-call-site table for the gate covering at minimum `DashboardView`, `DayDetailView`, `HistoryView`, `AppNavigationView.usesPadShell`, and `SettingsView.shouldApply`. For each site the table SHALL record: the compile-platform availability (e.g., `#if !os(macOS)` exclusion), the current behavior, the desired macOS behavior, and either "use the shared gate" or "use a separate platform check, gate not modified for this site". The implementation SHALL match the table.

### 6. macOS Visual Chrome Compatibility

**User Story:** As a macOS user, I want the new adaptive bodies to render without the Liquid Glass reflection artifacts that NavigationSplitView detail content can produce, so that the layout looks clean under the toolbar.

**Acceptance Criteria:**

1. <a name="6.1"></a>The macOS Dashboard, Day Detail, and History adaptive bodies SHALL render under the navigation toolbar without visible Liquid Glass reflection artifacts in either light or dark appearance — verified by manual visual inspection on a macOS 26 build before release.
2. <a name="6.2"></a>The macOS Day Detail toolbar chevrons and navigation title SHALL meet system contrast guidance against the Liquid Glass toolbar background in both appearances — verified by manual visual inspection.
3. <a name="6.3"></a>The change SHALL NOT alter iOS rendering of the same screens.

### 7. Tests

**User Story:** As a developer, I want automated coverage for the macOS layout gate, toolbar wiring, and breakpoint behavior, so that regressions are caught before release.

**Acceptance Criteria:**

1. <a name="7.1"></a>The test suite SHALL include a unit test verifying the shared layout gate selects the adaptive branch on macOS (compile-gated under `os(macOS)` if needed).
2. <a name="7.2"></a>The test suite SHALL include unit tests verifying that the macOS Day Detail toolbar next-day action is disabled when `viewModel.isToday` is true and enabled otherwise, that the previous-day action is always enabled, and that invoking each action calls the corresponding `viewModel.navigate*()` method.
3. <a name="7.3"></a>The test suite SHALL include a unit test verifying `AdaptiveColumnsLayout` returns the expected column count at the 699 / 700 / 999 / 1000 pt boundaries (extends existing `AdaptiveColumnsLayoutTests`).
4. <a name="7.4"></a>The test suite SHALL include a unit test verifying that the Day Detail navigation title formatter resolves to `"Today"` when `viewModel.isToday` is true and to the `DayDetailEyebrow.full` formatted string otherwise.
5. <a name="7.5"></a>Existing `AdaptiveColumnsLayoutTests`, Day Detail view-model tests, sidebar/tab-sync tests, and Screen tests SHALL continue to pass without modification.
