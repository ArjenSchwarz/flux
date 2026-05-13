# Requirements: Zoom in on Graphs

## Introduction

Flux currently renders all charts inline at card size, which is too small for inspecting fine detail in solar, grid, daily-usage, day-power, and day-battery charts. This feature adds an explicit affordance on each active chart that opens it in an enlarged presentation — a full-screen sheet (with landscape) on iOS and a dedicated window on macOS — while preserving the chart's existing selection interactions. The motivation is to make the data legible without forcing every screen to allocate more vertical space to its charts.

## Non-Goals

- Pinch-to-zoom or pan along the time axis inside the enlarged chart.
- New chart types, additional series, or any change to the data shown by existing charts.
- Adding the affordance to the three chart views that exist in code but are not currently rendered on any screen (HistoryBatteryCard, SOCChartView, BatteryPowerChartView).
- Side-by-side comparison of multiple enlarged charts on iOS.
- Persisting open enlarged windows across app launches on macOS.
- Exporting, printing, or sharing the enlarged chart.
- Changes to widgets, the Dashboard tile graphics, or any non-Chart visualisation.

## Requirements

### 1. Expand Affordance on Active Charts

**User Story:** As a Flux user, I want a clearly visible expand control on each chart card, so that I know I can view that chart in a larger size.

**Acceptance Criteria:**

1. <a name="1.1"></a>The system SHALL render an expand affordance on each of the five active chart cards: HistorySolarCard, HistoryGridUsageCard, HistoryDailyUsageCard, PowerChartView, and BatteryCombinedChartView.  
2. <a name="1.2"></a>The expand affordance SHALL be a button using SF Symbol `arrow.up.left.and.arrow.down.right`, rendered at `.title3` size with `.medium` weight in `FluxTheme.Palette.secondaryText`, with `.plain` button style, overlaid in the top-trailing corner of the chart's drawing area with an 8-point inset from each edge. All five chart cards SHALL use the same affordance with the same placement so that the control is in a predictable spot on every chart.  
3. <a name="1.3"></a>WHEN the user activates the expand affordance, THEN the system SHALL open the enlarged presentation for that chart.  
4. <a name="1.4"></a>The expand affordance SHALL NOT alter, intercept, or disable the chart's existing inline selection behaviour (chartXSelection on Day Detail charts; the synchronised day-selection overlay on History charts).  
5. <a name="1.5"></a>The expand affordance SHALL have an accessibility label of "Expand chart" and SHALL be reachable by VoiceOver on iOS and by keyboard focus on macOS.

### 2. Enlarged Presentation on iOS

**User Story:** As an iOS user, I want the enlarged chart to fill the screen and rotate to landscape, so that I have enough horizontal space to read the chart in detail.

**Acceptance Criteria:**

1. <a name="2.1"></a>WHEN the user activates expand on iOS, THEN the system SHALL present the chart in a full-screen modal that covers the underlying screen.  
2. <a name="2.2"></a>The enlarged presentation SHALL support both portrait and landscape orientations regardless of the orientation lock applied to the rest of the app.  
3. <a name="2.3"></a>The enlarged presentation SHALL provide an explicit Close control. The enlarged presentation SHALL also display a visible drag handle at its top edge that dismisses the presentation on a downward drag past a system-appropriate threshold; the drag handle SHALL NOT overlap the chart's drawing area.  
4. <a name="2.4"></a>The enlarged presentation SHALL show the same title and any contextual header information (e.g. selected date, totals) that the inline card shows.  
5. <a name="2.5"></a>The chart inside the enlarged presentation SHALL expand to consume the available area in both orientations, leaving room only for the title, header, and Close control.  
6. <a name="2.6"></a>WHEN the enlarged presentation is dismissed by any path (Close button, swipe-down, tab switch, or any deep navigation that unmounts the presenter), THEN the system SHALL restore the host scene to the orientation lock that was in effect before the presentation was shown.  
7. <a name="2.7"></a>IF the per-scene orientation override is denied by the system, THEN the enlarged presentation SHALL render in portrait rather than failing to appear.

### 3. Enlarged Presentation on macOS

**User Story:** As a macOS user, I want the enlarged chart in its own window so that I can position it alongside the main app window.

**Acceptance Criteria:**

1. <a name="3.1"></a>WHEN the user activates expand on macOS, THEN the system SHALL open the chart in a separate, resizable window distinct from the main app window.  
2. <a name="3.2"></a>Enlarged-window identity SHALL be the chart's view type only (one of: HistorySolar, HistoryGridUsage, HistoryDailyUsage, DayPower, DayBatteryCombined). Underlying state such as the selected date range SHALL NOT contribute to window identity.  
3. <a name="3.3"></a>WHEN the user activates expand on a chart whose enlarged window is already open, THEN the system SHALL bring the existing window to the front rather than opening a duplicate.  
4. <a name="3.4"></a>WHEN the user activates expand on a different chart while another enlarged window is already open, THEN the system SHALL open a second window so that the existing one is not replaced.  
5. <a name="3.5"></a>WHEN a new enlarged window is opened, THEN the system SHALL place it using SwiftUI's default scene placement (which positions the window on the screen of the key window). A future iteration MAY tighten this to follow the cursor's display if doing so proves to matter in practice.  
6. <a name="3.6"></a>The enlarged window SHALL have a minimum content size of 720 × 480 points and SHALL be user-resizable above that minimum.  
7. <a name="3.7"></a>The enlarged window SHALL NOT be persisted across app launches: WHEN the app launches with no user interaction since the previous quit, THEN no enlarged chart window SHALL appear, regardless of whether one was open at quit time.  
8. <a name="3.8"></a>Closing the main app window SHALL NOT close open enlarged chart windows; closing the last enlarged chart window SHALL NOT terminate the app.

### 4. Preserved Interactivity in Enlarged View

**User Story:** As a user inspecting a chart in detail, I want the same selection behaviour as the inline card, so that I can read values at a specific time or day at higher precision.

**Acceptance Criteria:**

1. <a name="4.1"></a>The enlarged presentation of PowerChartView and BatteryCombinedChartView SHALL support chartXSelection with the same selection state, callouts, and header readout as the inline card.  
2. <a name="4.2"></a>The enlarged presentation of HistorySolarCard, HistoryGridUsageCard, and HistoryDailyUsageCard SHALL support the same drag-to-select-a-day overlay as the inline card.  
3. <a name="4.3"></a>The enlarged presentation SHALL NOT introduce pinch-to-zoom or time-axis panning.  
4. <a name="4.4"></a>WHEN the user changes the selected point or selected day inside the enlarged presentation, THEN the system SHALL reflect that selection in the underlying inline card on close.  
5. <a name="4.5"></a>WHEN the underlying data refreshes while the enlarged presentation is open and no selection gesture is in progress, THEN the system SHALL update the enlarged chart with the new data without forcing the user to reopen it.  
6. <a name="4.6"></a>WHEN a data refresh arrives while a chartXSelection drag or History day-selection drag is in progress, THEN the system SHALL defer applying the new data to the chart until the gesture ends.

### 5. Dismissal and Lifecycle

**User Story:** As a user, I want predictable ways to dismiss the enlarged view and have the app behave sensibly while it is open.

**Acceptance Criteria:**

1. <a name="5.1"></a>The enlarged presentation SHALL be dismissible on iOS via the Close control and via a downward swipe.  
2. <a name="5.2"></a>The enlarged presentation SHALL be dismissible on macOS via the standard window close control and via the standard ⌘W shortcut while focused.  
3. <a name="5.3"></a>WHEN the iOS user navigates away from the screen containing the originating chart (tab switch or any deep navigation that unmounts the presenter), THEN the system SHALL dismiss the enlarged presentation and SHALL NOT preserve its selection state if the user returns to that screen later.  
4. <a name="5.4"></a>On macOS, enlarged windows SHALL be independent of the main window's current navigation: changing the date range or active tab in the main window SHALL NOT close them.  
5. <a name="5.5"></a>WHEN the iOS user backgrounds the app while the enlarged presentation is open, THEN the system SHALL preserve the enlarged presentation and its current selection on returning to the foreground.

### 6. Performance and Accessibility Baselines

**User Story:** As a user, I want the enlarged view to open quickly and remain usable with assistive technologies.

**Acceptance Criteria:**

1. <a name="6.1"></a>The system SHALL NOT perform any synchronous data fetch or main-thread blocking work between activating the expand affordance and the start of the presentation animation; the presentation animation SHALL begin on the next render pass after activation. Verified via an `os_signpost` interval covering activation-to-animation-start on devices with an A16 or newer chip (iOS) and any Apple Silicon Mac (macOS).  
2. <a name="6.2"></a>The enlarged chart and its controls SHALL be reachable by VoiceOver on iOS and by full keyboard navigation on macOS, using the same accessibility labels as the inline card. WHEN the enlarged presentation is dismissed, THEN VoiceOver / keyboard focus SHALL return to the expand affordance that opened it.  
3. <a name="6.3"></a>The enlarged presentation SHALL respect Dynamic Type for all text it renders — title, header, axis labels, and selection callouts — up to the largest accessibility size that the inline card already supports.  
4. <a name="6.4"></a>The presentation and dismissal animations SHALL respect the Reduce Motion accessibility setting; WHEN Reduce Motion is enabled, THEN the system SHALL use a cross-fade rather than a slide/scale transition.
