# Decision Log: What's New

## Decision 1: Hand-authored Swift catalogue, not parsed from CHANGELOG.md

**Date**: 2026-05-08
**Status**: accepted

### Context

The repo already has a `CHANGELOG.md` in Keep a Changelog format, oriented toward developers and including ticket IDs and technical phrasing. The user explicitly called out that the new feature is a *non-technical* changelog and "not the already created changelog". A choice exists between auto-deriving copy from `CHANGELOG.md`, bundling JSON/plist/markdown, or hand-authoring a Swift structure.

### Decision

Author release entries as a typed Swift catalogue in `FluxCore`, hand-curated per release.

### Rationale

The whole point of this feature is voice control — the technical CHANGELOG entries (e.g. "T-896: implement History Usage Stats overview card") are not what should reach end users. A typed Swift array is the smallest moving part: no parser, no schema validation, no asset-bundling, and full Xcode/Swift type checking. JSON would only matter if a non-engineer needed to edit it, which is not the case here.

### Alternatives Considered

- **Auto-derive from CHANGELOG.md**: Rejected — wrong audience and voice; would force a parser plus a re-edit step on every release anyway.
- **Bundled JSON/plist**: Rejected — adds parsing and error handling for no real benefit in a two-developer app.
- **Bundled Markdown file**: Rejected — renders awkwardly in SwiftUI; categorization would be implicit.

### Consequences

**Positive:**
- Type-safe, no parse failures possible at runtime.
- Localization wrapping via `String(localized:)` works naturally.
- Adding a release is a single typed Swift edit.

**Negative:**
- Requires a code change (and rebuild) to update notes — acceptable given updates and notes ship together anyway.

---

## Decision 2: Auto-show on version bump plus Settings entry

**Date**: 2026-05-08
**Status**: accepted

### Context

The feature exists because Kelsey (secondary user) won't go looking for release notes. The trigger choices were: auto-show only, manual-only via Settings, or both.

### Decision

Auto-present the sheet on first launch after the marketing version increases, and provide a "What's New" entry in Settings as a re-read path.

### Rationale

Auto-presentation is what gets the content seen in the first place. A Settings entry costs almost nothing and prevents the "I dismissed it too fast and now can't find it" failure mode.

### Alternatives Considered

- **Manual only (Settings entry)**: Rejected — Kelsey would never look.
- **Auto-show only, no Settings entry**: Rejected — accidental dismissal becomes irrecoverable until the next release.

### Consequences

**Positive:**
- Visible to non-technical users without effort.
- Recoverable if dismissed accidentally.

**Negative:**
- One additional row in Settings to maintain.

---

## Decision 3: No v1.0 backfill; distinguish fresh install from pre-feature upgrade

**Date**: 2026-05-08
**Status**: accepted

### Context

Currently shipped version is 1.0. The What's New feature ships in a later version (likely 1.1+). Two different "no last-seen value" cases must be distinguished:

1. **Fresh install** of a version that already contains the feature — user has no app-group state at all.
2. **Upgrade from v1.0** which pre-dates the feature — user has app-group state (theme, font, etc.) but no last-seen-version key, since that key was never written by the older code.

If both are treated identically (silent-set to current), existing v1.0 users would silently lose the v1.1 entry, which is the exact opposite of the feature's goal.

### Decision

Do not author a v1.0 entry. Distinguish the two no-last-seen cases at runtime:

- **Fresh install** (no last-seen value AND no other Flux app-group preferences ever written): silently set last-seen to current, no sheet.
- **Pre-feature upgrade** (no last-seen value BUT other Flux app-group preferences exist): seed last-seen with `"1.0"` and run normal auto-presentation logic against that seed.

### Rationale

The presence of any other Flux preference key in the app-group store is a reliable signal that the app has run before. Seeding with `"1.0"` (the version that pre-dated this feature) means an existing user upgrading to v1.1+ will see the v1.1 entry on first launch — which is the whole point of the feature. Fresh installs see nothing, as intended. The seed approach was preferred over an explicit migration step because it has a single decision site (the auto-presentation check) instead of touching app launch lifecycle.

### Alternatives Considered

- **Backfill v1.0 with a welcome entry**: Rejected — adds work, only helps an empty cohort, and risks looking like onboarding (which this is not).
- **Treat all "no last-seen" cases as fresh install**: Rejected — silently swallows the first post-feature release for every existing user.
- **Explicit one-shot migration step at launch**: Rejected — more code paths to reason about than the seed-on-read approach for the same outcome.

### Consequences

**Positive:**
- Existing v1.0 users see the v1.1 entry on first launch after upgrading.
- Fresh installs see a clean dashboard, no unsolicited modal.
- No additional copy to author for v1.0.

**Negative:**
- The "other Flux preference exists?" check creates a soft coupling between this feature and other settings keys. Acceptable: those keys already exist and are stable. If all of them were ever cleared, an existing user would be misclassified as fresh.

---

## Decision 4: Authoring optional per release

**Date**: 2026-05-08
**Status**: accepted

### Context

A standing requirement that every release ship with at least one entry would force filler copy on bug-fix-only releases.

### Decision

Treat What's New entries as optional per release. Releases without any highlights do not trigger the sheet — the system silently advances the last-seen version.

### Rationale

Forcing entries on bug-only releases produces low-signal copy and trains users to ignore the sheet. Skipping silently keeps the channel high-signal.

### Alternatives Considered

- **Mandatory per-release entry**: Rejected — risks padding with trivial bullets that erode trust in the feature.

### Consequences

**Positive:**
- High signal-to-noise.
- No PR-review enforcement burden.

**Negative:**
- A version bump can pass through without showing anything; relies on author judgement.

---

## Decision 5: Dismissible by gesture plus Done button

**Date**: 2026-05-08
**Status**: accepted

### Context

The sheet could either behave like a standard iOS sheet (swipe-down + escape on macOS, plus a Done button) or block the user with an explicit Continue action that disables gestures.

### Decision

Use the standard sheet: swipe-down on iOS, escape on macOS, with a Done button as the explicit action.

### Rationale

The feature is informational, not a required acknowledgement. Standard sheet behaviour is what users expect; a blocking modal would feel like onboarding and over-emphasises the importance of the content.

### Alternatives Considered

- **Blocking modal with explicit Continue**: Rejected — the content does not warrant blocking, and the precedent in the app (Settings sheet, NoteEditorSheet) is gesture-dismissible.

### Consequences

**Positive:**
- Native feel; matches existing modal patterns in Flux.
- Less friction for re-reading via Settings.

**Negative:**
- Users may dismiss accidentally; mitigated by the Settings entry providing a re-read path.

---

## Decision 6: Last-seen version stored per-device in app-group UserDefaults

**Date**: 2026-05-08
**Status**: accepted

### Context

The "last-seen version" preference could live in the app-group `UserDefaults` (per-device, alongside other Flux settings) or in `NSUbiquitousKeyValueStore` for cross-device sync.

### Decision

Store the last-seen version in the existing app-group `UserDefaults`. Each device tracks dismissal independently.

### Rationale

The preference is low-stakes — being shown an "already-seen" sheet once on a second device is a trivial cost compared to the complexity of iCloud KVS conflict semantics, sync timing, and an extra dependency. Other Flux settings already live in the app-group store, so this stays consistent.

### Alternatives Considered

- **NSUbiquitousKeyValueStore (iCloud sync)**: Rejected — adds iCloud round-trip behaviour for a preference whose worst-case sync miss is "user sees the sheet again on another device".

### Consequences

**Positive:**
- Same persistence layer as other settings; no new dependency.
- No iCloud quota or conflict handling to reason about.

**Negative:**
- Dismissing on one device does not suppress on another; acceptable trade-off.

---

## Decision 7: Settings entry shows the most recent release only

**Date**: 2026-05-08
**Status**: accepted

### Context

The manual access path from Settings could either show only the latest release with highlights or a scrollable archive of all past releases.

### Decision

Tapping "What's New" in Settings opens a sheet showing only the most recent release that has at least one highlight.

### Rationale

The primary purpose of the Settings entry is "I dismissed it too fast — let me re-read what was new in this version". A historical archive is a different feature with a different shape and is not the user need this ticket addresses.

### Alternatives Considered

- **Scrollable archive of all releases**: Rejected — out of scope; would expand visual design and copy maintenance for limited additional value.

### Consequences

**Positive:**
- Single small sheet to design and test.
- Mirrors the auto-presentation path closely.

**Negative:**
- No way to read an entry from two releases ago without finding it elsewhere; not a current need.

---

## Decision 8: Skipped versions show stacked entries newest-first

**Date**: 2026-05-08
**Status**: accepted

### Context

If a user upgrades across multiple versions (e.g., 1.0 → 1.3 directly), the sheet could show entries for every unseen release stacked together or only the newest.

### Decision

Show all unseen release entries with at least one highlight in a single scrollable sheet, ordered newest-first.

### Rationale

A user who skipped versions still benefits from knowing what changed in between. Stacked rendering is a natural consequence of "show every entry whose version is greater than last-seen".

### Alternatives Considered

- **Latest-only**: Rejected — would hide changes the user has not yet seen, defeating the feature for skip-version cases.

### Consequences

**Positive:**
- Skip-version case Just Works.
- One sheet handles every scenario.

**Negative:**
- Sheet can be longer if many releases ship between launches; mitigated by scrolling.

---

## Decision 9: Ignore catalogue entries newer than the installed app

**Date**: 2026-05-08
**Status**: accepted

### Context

During development, an entry for an upcoming release (e.g., 1.2) is typically authored before the corresponding `MARKETING_VERSION` bump in the Xcode project. Without filtering, a developer running an in-progress build at marketing version 1.1 with a 1.2 entry already in the catalogue would see the 1.2 sheet.

### Decision

Both auto-presentation and manual access SHALL ignore release entries whose version is greater than the installed marketing version.

### Rationale

A user only benefits from notes for a version they are actually running. Filtering at the consumption boundary keeps the catalogue editable in advance of the version bump without leaking unreleased copy to TestFlight builds.

### Alternatives Considered

- **No filtering**: Rejected — leaks unreleased copy on dev/TestFlight builds.
- **Forbid entries newer than current**: Rejected — would prevent normal authoring workflow where notes land before the version bump.

### Consequences

**Positive:**
- Notes can be authored in advance of the corresponding `MARKETING_VERSION` bump.
- TestFlight and dev builds at the previous version don't show the future entry.

**Negative:**
- Slight asymmetry between catalogue contents and what the running app shows; well-defined and documented.

---

## Decision 10: TestFlight rollback / downgrade is a no-op

**Date**: 2026-05-08
**Status**: accepted

### Context

A user could install a newer TestFlight build, then roll back to a previous build. The installed marketing version then becomes less than the persisted last-seen version.

### Decision

If the installed marketing version is less than or equal to the persisted last-seen version, do not show the sheet and do not modify the persisted last-seen version.

### Rationale

A downgrade isn't a meaningful "moving forward" event for the user. Leaving last-seen alone means that when they upgrade again past their previous high-water mark, they'll see only the genuinely-new entries — not a re-show of what they already saw at the higher version.

### Alternatives Considered

- **Reset last-seen to current on downgrade**: Rejected — would re-show the same entries on the next upgrade.
- **Show entries between current and last-seen**: Rejected — those are entries the user has already seen.

### Consequences

**Positive:**
- Symmetric, predictable behaviour across version movements in either direction.
- No spurious re-presentation after a TestFlight rollback.

**Negative:**
- None of note for this scope.

---

## Decision 11: Manual sheet hosted on SettingsView on both platforms

**Date**: 2026-05-08
**Status**: accepted

### Context

On macOS, Settings is a top-level `Scene` distinct from the main `WindowGroup` containing `AppNavigationView`. Three options exist for where to present the manual What's New sheet on macOS: (a) inside the Settings scene as a sheet over the Settings form, (b) plumb the action across to `AppNavigationView` via shared state, (c) open a dedicated `Window` scene.

### Decision

Present the manual sheet directly within the Settings view on both iOS and macOS, using a local `@State` flag and `.sheet` modifier on `SettingsView`. No cross-scene plumbing.

### Rationale

The manual access path is informational and dismissible — it does not need to escape the Settings surface to be useful. Cross-scene plumbing introduces an `@Observable` presenter or `@AppStorage` flag whose only purpose is to bridge two scenes; the simplification of a directly-hosted sheet is worth the minor convention bend. iOS already presents Settings as a sheet, so a sheet-on-sheet on iOS is the existing pattern for any modal launched from Settings (e.g., NoteEditorSheet from a row action). macOS handles a sheet over the Settings scene without issue.

### Alternatives Considered

- **Plumb to AppNavigationView via shared `@Observable` presenter**: Rejected — adds a presenter type, environment injection, and observer wiring purely to relocate where a single sheet renders. No user-visible benefit.
- **Dedicated `Window` scene for What's New**: Rejected — would expose a free-floating What's New window in macOS's Window menu and require Scene-level wiring not currently used in the app.

### Consequences

**Positive:**
- One sheet host site per access path (auto on `AppNavigationView`, manual on `SettingsView`) — both trivially testable in isolation.
- No cross-scene state to reason about.
- Symmetry with how Settings already launches modal sub-views on iOS.

**Negative:**
- macOS Settings has a sheet rendered over it, which is a soft convention bend (Apple's own apps would more typically use the Help menu for this). Not a functional issue.

---

## Decision 12: Coordinator returns a decision enum; presentation sites apply it

**Date**: 2026-05-08
**Status**: accepted

### Context

The auto-presentation logic could either be a function with side effects (reads `Bundle`, reads/writes `UserDefaults`, returns `Void`) or a pure decision that the call site interprets and applies.

### Decision

`WhatsNewCoordinator` is a `Sendable` value type that takes its inputs (catalogue, installed version, lastSeen, hasAnyFluxPref) by injection and returns an `AutoDecision` enum (`present(releases:) | silentSet(version:) | skip`). The view layer reads `Bundle` and `UserDefaults`, builds the coordinator, applies the decision, and writes back when needed.

### Rationale

Keeps every behavioural rule in the AC table testable in isolation without mocking `Bundle` or `UserDefaults`. The view-layer glue is small enough that not testing it directly is acceptable; the coordinator carries the logic worth testing.

### Alternatives Considered

- **Side-effecting coordinator**: Rejected — would force `UserDefaults` and `Bundle` mocking for every decision-table test.
- **Plain function returning `[WhatsNewRelease]?`**: Rejected — collapses `silentSet` and `skip` into the same `nil` result and loses the "advance last-seen without showing" branch needed by AC 2.7.

### Consequences

**Positive:**
- Unit tests are fast, deterministic, and read like the AC table.
- The "silent advance" path (AC 2.7) is explicit at every call site.

**Negative:**
- Two-step usage at the call site (compute decision, then apply it) instead of a single function call; trivial overhead.

---

## Decision 13: Category symbols defaulted in the view layer, not the catalogue

**Date**: 2026-05-08
**Status**: accepted

### Context

`Highlight` includes an optional `symbol: String?`. Whether the catalogue is required to populate it or whether the view derives a default from `category` was undecided.

### Decision

`symbol` is optional. When nil, the view layer maps the category to a default SF Symbol (`sparkles` / `wand.and.stars` / `checkmark.circle`).

### Rationale

Authoring is faster when defaults are sensible. Per-highlight symbol overrides remain available for the rare case where a specific glyph reads better than the category default.

### Alternatives Considered

- **Required symbol**: Rejected — forces author to think about iconography on every highlight; mostly noise.
- **No symbols**: Rejected — sectioning and visual scan benefit from the icons.

### Consequences

**Positive:**
- Catalogue entries are typically two or three lines.
- View-layer defaults are easy to evolve without catalogue churn.

**Negative:**
- A consumer of the data type alone (without the view) wouldn't know what symbol to use; not a current concern since the catalogue is consumed only by the bundled view.

---

## Decision 14: Settings row hidden when no manual content exists

**Date**: 2026-05-08
**Status**: accepted

### Context

If the catalogue is empty (or contains no entry whose version is ≤ the installed marketing version with at least one highlight), `WhatsNewCoordinator.manualLatest()` returns `nil`. Two options: render the row anyway and show an empty-state sheet on tap, or hide the row.

### Decision

Hide the "What's New" row in Settings when `manualLatest()` returns `nil`.

### Rationale

A row that opens an empty sheet is a dead end. Hiding the row is the conservative behaviour and removes the need for the sheet view to render an empty state. This case is rare in practice (the catalogue will normally have entries for the installed version) but the hide-on-nil rule is simpler than designing a defensible empty state.

### Alternatives Considered

- **Show the row and render an empty state**: Rejected — exposes a non-functional UI element to no benefit.
- **Disable the row visibly**: Rejected — communicates "not yet" instead of "nothing to see", which is misleading.

### Consequences

**Positive:**
- No empty-state sheet to design.
- Fresh installs of a build with at least one entry for the installed version still see the row, matching expectations.

**Negative:**
- A user inspecting the Settings layout in development with an empty catalogue might be briefly confused; not a real-world concern.

---
