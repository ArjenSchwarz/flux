# Requirements: What's New

## Introduction

Flux currently has no way to surface the visible changes in a new app version to non-technical users. The repository's `CHANGELOG.md` is engineering-oriented and unsuitable for end users. This feature adds a "What's New" view that auto-presents on first launch after a version bump and is reachable from Settings, so users see plain-language summaries of what changed since they last opened the app.

## Non-Goals

- Auto-derivation of user-facing copy from the existing technical `CHANGELOG.md` — voice and audience differ.
- Localization — English-only; no localization wrapping or string catalogue entries.
- Showing release notes for previous versions never installed (e.g., a "history of all releases" archive).
- Any push, in-app messaging, or remote-fetched announcements — content is bundled with the app.
- Required entries per release — releases without visible user changes can ship with no entry.
- Backfilling a "What's New" entry for the current shipped version (1.0).
- App Store Connect release-notes automation.

## 1. Release Content Catalogue

**User Story:** As an app developer, I want to author user-facing release entries directly in code, so that adding a new release's notes is a single typed edit with no parsing or external file plumbing.

**Acceptance Criteria:**

1. <a name="1.1"></a>The system SHALL define each release as a typed record containing a version string, a release date, and an ordered list of highlight entries.  
2. <a name="1.2"></a>Each highlight entry SHALL have a category (one of: New, Improved, Fixed), a title, an optional one-line detail, and an optional symbol identifier.  
3. <a name="1.3"></a>The catalogue's version strings SHALL be compared component-wise as integers, with missing trailing components treated as zero, so that `1.10` is treated as newer than `1.9` and `1.2` is treated as equal to `1.2.0`.  
4. <a name="1.4"></a>The catalogue SHALL allow a release to have zero highlight entries, in which case the system treats that version as having no user-facing changes to display.

## 2. Auto-Presentation on Version Bump

**User Story:** As an app user, I want to see what changed when I open the app after an update, so that I notice new capabilities without having to look for them.

**Acceptance Criteria:**

1. <a name="2.1"></a>The "installed marketing version" SHALL be sourced from `CFBundleShortVersionString` in the app bundle's `Info.plist`.  
2. <a name="2.2"></a>WHEN the app launches and detects that the installed marketing version is greater than the persisted last-seen version, the system SHALL present the What's New sheet with all release entries whose version is greater than the last-seen version and less than or equal to the installed marketing version, ordered newest first.  
3. <a name="2.3"></a>WHEN the user dismisses the sheet, the system SHALL persist the installed marketing version as the last-seen version so it does not reappear on subsequent launches at the same version.  
4. <a name="2.4"></a>IF no last-seen version is persisted AND no other Flux app-group preference has ever been written on this device, the system SHALL treat this as a fresh install: silently set the last-seen version to the installed marketing version and SHALL NOT show the sheet.  
5. <a name="2.5"></a>IF no last-seen version is persisted BUT other Flux app-group preferences exist on this device, the system SHALL treat this as an upgrade from a pre-feature version: silently seed the last-seen version with `1.0` and proceed with the auto-presentation logic against that seed.  
6. <a name="2.6"></a>IF the installed marketing version is less than or equal to the persisted last-seen version (e.g., a TestFlight rollback), the system SHALL NOT show the sheet and SHALL NOT modify the persisted last-seen version.  
7. <a name="2.7"></a>IF the version range between last-seen and current contains no release entries with at least one highlight, the system SHALL silently update the last-seen version to the installed marketing version and SHALL NOT show the sheet.  
8. <a name="2.8"></a>Release entries with a version greater than the installed marketing version SHALL be ignored by both auto-presentation and manual access.  
9. <a name="2.9"></a>The sheet SHALL be presented at most once per cold launch, regardless of how many version-bump checks the system performs.

## 3. Manual Access from Settings

**User Story:** As an app user, I want to re-read the latest release notes whenever I want, so that I can revisit features I may have skimmed past.

**Acceptance Criteria:**

1. <a name="3.1"></a>The Settings screen SHALL include a "What's New" entry that opens the What's New sheet.  
2. <a name="3.2"></a>WHEN the user opens the sheet manually from Settings, the system SHALL show only the most recent release entry that has at least one highlight and whose version is less than or equal to the installed marketing version. (Note: this intentionally differs from auto-presentation, which can stack multiple unseen releases — see Decision 7.)  
3. <a name="3.3"></a>Manually opening the sheet SHALL NOT modify the persisted last-seen version.

## 4. Sheet Presentation

**User Story:** As an app user, I want the What's New view to feel native on my device, so that it fits the rest of the app and I can dismiss it the way I expect.

**Acceptance Criteria:**

1. <a name="4.1"></a>The sheet SHALL display each release with its version, release date, and a sectioned list of highlights grouped by category in the order: New, Improved, Fixed.  
2. <a name="4.2"></a>Each highlight SHALL render a category symbol, the title, and the optional detail line if present.  
3. <a name="4.3"></a>The sheet SHALL provide a primary "Done" action that dismisses it.  
4. <a name="4.4"></a>WHERE the platform supports interactive sheet dismissal (swipe-down on iOS, escape key on macOS), the sheet SHALL be dismissible via that gesture in addition to the Done action.  
5. <a name="4.5"></a>The sheet SHALL apply the app's existing visual styling so that it is consistent with other modal sheets in Flux.  
6. <a name="4.6"></a>WHEN the sheet is presented and contains more highlights than fit on screen, the content SHALL scroll vertically.

## 5. Cross-Platform Behaviour

**User Story:** As an app user on either iOS or macOS, I want the What's New experience to behave correctly on whichever platform I'm using, so that I don't see broken or misplaced UI.

**Acceptance Criteria:**

1. <a name="5.1"></a>The auto-presentation logic SHALL run on both iOS and macOS app launches.  
2. <a name="5.2"></a>The persisted last-seen version SHALL be stored in the existing app-group UserDefaults store on each device independently; dismissing the sheet on one device SHALL NOT affect re-presentation on the user's other devices.  
3. <a name="5.3"></a>The Settings entry SHALL appear in both the iOS Settings sheet and the macOS Settings scene.

## 6. Accessibility

**User Story:** As a VoiceOver user, I want the What's New view to read out the release contents in order, so that I can understand what changed without sighted assistance.

**Acceptance Criteria:**

1. <a name="6.1"></a>Each highlight SHALL expose an accessibility label that combines its category, title, and detail (if present) into a single readable sentence.  
2. <a name="6.2"></a>The Done action SHALL have an accessibility label that identifies it as dismissing the sheet.  
3. <a name="6.3"></a>The sheet SHALL be navigable in VoiceOver focus order from version header through highlights to the Done action.
