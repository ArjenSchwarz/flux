# Decision Log: add-widgets (T-843)

## Decision 1: Use full spec workflow over smolspec

**Date**: 2026-04-20
**Status**: accepted

### Context

T-843 originally described "add home/lock screen widgets to the iOS app" without detailed scope. The user clarified that widgets must show the same data as the top of the Dashboard — battery state and load information (solar/load/grid). Implementation requires a new Xcode target, a new local Swift Package for shared code, an App Group cache, and coordinated changes to the main app's storage layer.

### Decision

Use the full spec-driven workflow (requirements → design → tasks) rather than the lightweight smolspec.

### Rationale

Estimated scope is 600–900 lines across 15+ files, with a new Xcode target, a new SPM package, and modifications to the main app's UserDefaults usage. Cross-cutting concerns (App Group provisioning, keychain sharing, timeline refresh budget, staleness UX) warrant explicit requirements and design decisions.

### Alternatives Considered

- **Smolspec with narrow scope (one widget family only)**: Rejected — the user explicitly said "full spec" and a single-family widget would still require the same underlying architectural work (package, cache, App Group migration), so narrowing the widget families saves little.
- **Plan mode without a written spec**: Rejected — this feature will change how the main app stores UserDefaults and how source files are organised; those changes need to be documented durably, not in chat history.

### Consequences

**Positive:**
- Architectural decisions (where shared code lives, how the cache is structured) are documented before code is written.
- The UserDefaults migration plan is visible before it runs, reducing risk of data loss.

**Negative:**
- Slower start — requirements/design reviews add calendar time before any code lands.

---

## Decision 2: Introduce a local Swift Package (`FluxCore`) for shared code

**Date**: 2026-04-20
**Status**: accepted

### Context

WidgetKit extensions live in a separate sandbox and cannot share source files with the main app target via "membership" without operational hazards (per `rules/language-rules/swift.md`, which explicitly warns against copying files). The widget must use the same `FluxAPIClient`, `KeychainService`, `APIModels`, formatters, and colour helpers as the app to avoid drift.

### Decision

Add a local Swift Package at `Flux/Packages/FluxCore` and migrate shared types into it. Both the main app and the widget extension depend on the package.

### Rationale

A single source of truth avoids the classic widget-extension maintenance trap where two copies of `FluxAPIClient` drift in subtle ways. The swift.md rule file prescribes exactly this approach.

### Alternatives Considered

- **Copy files into the widget target via file-system-synchronized group**: Rejected — swift.md forbids it; produces two copies that rot independently.
- **Use a remote Swift Package**: Rejected — the code is private to this project; a remote package adds publishing overhead for no benefit.

### Consequences

**Positive:**
- One implementation of networking and parsing.
- Clean public-surface boundary that forces sensible access control.

**Negative:**
- Requires changing `internal` to `public` on many types, plus updating imports in the main app.
- Xcode project must learn about the local package (one-time setup).

---

## Decision 3: App reads and writes an App Group `UserDefaults` cache for the widget

**Date**: 2026-04-20
**Status**: accepted

### Context

The widget extension cannot access the main app's SwiftData store, and calling `/status` from the widget on every refresh is wasteful given WidgetKit's refresh budget. The app already refreshes `/status` every 10 seconds while active.

### Decision

On every successful Dashboard refresh, write the latest `StatusResponse` plus a fetch timestamp to the shared App Group `UserDefaults` suite. The widget reads this cache first, then attempts its own live fetch to replace it.

### Rationale

Cache-first rendering lets the widget show useful data immediately after install. The widget's own fetch keeps data fresh when the user has not opened the app recently. App Group `UserDefaults` is supported by WidgetKit's sandbox and already enabled (`group.me.nore.ig.flux`).

### Alternatives Considered

- **Widget fetches `/status` every refresh with no cache**: Rejected — violates WidgetKit's ~40/day refresh budget if the user adds multiple widgets; leaves the first render empty.
- **Use a shared SQLite file in the App Group container**: Rejected — overkill for a single snapshot; JSON-in-UserDefaults is simpler and atomic.

### Consequences

**Positive:**
- Immediate widget render after install (if the app has been opened at least once).
- Simple, testable cache layer.

**Negative:**
- `UserDefaults+Settings` must migrate from `UserDefaults.standard` to the App Group suite so that `loadAlertThreshold` is visible in both targets. One-time migration required for existing installs.

---

## Decision 4: Static configuration only in v1 (no `WidgetConfigurationIntent`)

**Date**: 2026-04-20
**Status**: accepted

### Context

WidgetKit allows widgets to declare user-editable parameters via `AppIntentConfiguration`. Examples in this project would be "which metric to show in the small widget" or "which battery threshold triggers a red warning".

### Decision

Ship v1 with `StaticConfiguration` only. No user-configurable parameters.

### Rationale

The widget's data source is unambiguous (the single AlphaESS system the app is already connected to). Configurability introduces AppIntent plumbing and a Shortcuts surface that adds scope without clear user benefit at v1. A future spec can add `AppIntentConfiguration` without changing the shared package contract.

### Alternatives Considered

- **Ship with `AppIntentConfiguration` for metric selection**: Rejected — user has not asked for it; adds parameters that may become dead weight if defaults turn out to be right.

### Consequences

**Positive:**
- Smaller v1 surface; faster to ship.
- No risk of shipping a broken configuration picker in the widget gallery.

**Negative:**
- If the chosen small-widget metric turns out to be wrong for the user's daily glance, a second spec is needed to fix it.

---

## Decision 5: Lock-screen families skip the power trio

**Date**: 2026-04-20
**Status**: accepted

### Context

Lock-screen widgets (`accessoryCircular`, `accessoryRectangular`, `accessoryInline`) have strict contrast, size, and rendering-mode (monochrome/tinted) constraints. Displaying three distinct numeric columns reliably at this size is not feasible.

### Decision

Lock-screen families render battery state only (SOC + status). The power trio is a home-screen concept.

### Rationale

Legibility beats feature parity on the lock screen. Someone checking their lock screen wants SOC at a glance; for detail, they unlock.

### Alternatives Considered

- **Cram power trio into `accessoryRectangular`**: Rejected — at that size the numbers become uninterpretable and fail Dynamic Type above `.accessibility1`.

### Consequences

**Positive:**
- Lock-screen widgets remain legible in all rendering modes.
- Simpler test matrix.

**Negative:**
- Users who wanted load on the lock screen get status only.

---

## Decision 6: Keychain token must use `kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly`

**Date**: 2026-04-20
**Status**: accepted

### Context

Lock-screen widgets run while the device is locked. If the stored Keychain item uses `kSecAttrAccessibleWhenUnlocked` (the common default), any Keychain read from the widget after the device auto-locks will fail with `errSecInteractionNotAllowed`, silently denying the widget access to the bearer token and breaking the whole lock-screen feature. Both the design-critic and peer-review-validator flagged this during review.

### Decision

The app SHALL store the Keychain token with `kSecAttrAccessible = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly`. Any existing Keychain item with a stricter class is migrated on first launch after upgrade. On boot, before first unlock, the widget falls back to the cache without attempting a live fetch.

### Rationale

`AfterFirstUnlock` is the standard accessibility class for background / extension Keychain access. `ThisDeviceOnly` prevents iCloud Keychain sync of a token that is specific to this AWS deployment. Together they unblock the lock-screen widget while keeping the token out of iCloud backups and out of other devices.

### Alternatives Considered

- **Keep `WhenUnlocked` and disable lock-screen widgets**: Rejected — the user explicitly asked for both home- and lock-screen widgets.
- **Use `AfterFirstUnlock` without `ThisDeviceOnly`**: Rejected — the token is paired to a specific Lambda deployment with a bearer token the user provisioned manually; syncing it through iCloud Keychain has no benefit.

### Consequences

**Positive:**
- Lock-screen widgets authenticate reliably after the first unlock post-boot.
- Security posture improves (`ThisDeviceOnly` blocks iCloud sync).

**Negative:**
- One-time Keychain migration adds a small amount of code to run at launch.
- Between reboot and first unlock, the widget is cache-only. Requirement [11.6](requirements.md#11.6) handles this explicitly.

---

## Decision 7: Cache payload carries a `schemaVersion`

**Date**: 2026-04-20
**Status**: accepted

### Context

The cache stores a JSON-encoded `StatusResponse`. Without a version marker, a future backend rename that changes `StatusResponse` Codable keys would leave the widget silently decoding zero bytes from a pre-upgrade cache, producing a confusing "No data yet" state even though the app has cached valid data under a new shape.

### Decision

The cache envelope is `{ "schemaVersion": <int>, "fetchedAt": <iso8601>, "status": <StatusResponse> }`. Readers that encounter an unknown `schemaVersion` treat the cache as empty.

### Rationale

Adds three lines of code and makes future schema evolution painless. This is a one-way door — without it, every future change to `StatusResponse` becomes a correctness risk during the first run after upgrade.

### Alternatives Considered

- **No schema version, rely on Codable tolerance**: Rejected — decoder silently produces empty optional fields; a user with the old app cached would see garbled or empty widget data without warning.
- **Date-based versioning**: Rejected — harder to reason about than monotonic integers.

### Consequences

**Positive:**
- Backwards-incompatible changes to `StatusResponse` become an intentional version bump rather than a silent regression.

**Negative:**
- Every cache write includes two extra fields — trivial size impact.

---

## Decision 8: Cache writers skip if the cache is already newer

**Date**: 2026-04-20
**Status**: accepted

### Context

The app and the widget extension can both write the cache. If the user opens the app during a widget timeline refresh, the two processes may attempt to write within seconds of each other; whichever writes last wins. A late widget fetch (say, completed after a fresher app fetch) could overwrite the newer snapshot with its own older data.

### Decision

Cache writers read the existing `fetchedAt` before writing. If the existing timestamp is newer than the one about to be written, the writer aborts the write.

### Rationale

Makes "newest wins" the invariant regardless of write order. The race is rare (widget refreshes are infrequent) but when it happens it produces subtly wrong widgets until the next refresh cycle.

### Alternatives Considered

- **Lock the cache via `NSFileCoordinator`**: Rejected — not supported in widget extensions and overkill for a single-key JSON write.
- **Accept the race and let last-writer win**: Rejected — produces a stutter effect where widget shows older data after a successful app refresh.

### Consequences

**Positive:**
- Clock-monotonic behaviour from the user's perspective: widget data never goes backwards.

**Negative:**
- Writers do a cheap read-then-compare before every write. Negligible overhead.

---

## Decision 9: 30-minute refresh cadence; staleness buckets 45 min / 3 h

**Date**: 2026-04-20
**Status**: accepted

### Context

The initial draft hard-coded a 15-minute `.after` refresh and fresh/stale/offline buckets at 5/30 minutes. Peer review pointed out that with a realistic 15–60 minute actual reload cadence on iOS 26, `fresh < 5 min` would make "stale" the visual steady state, training users to ignore the marker. The design-critic further noted these are design decisions disguised as requirements.

### Decision

Nominal refresh cadence is 30 minutes (`.after(now + 30 min)`). Staleness buckets are `fresh < 45 min`, `stale 45 min – 3 h`, `offline ≥ 3 h`. User confirmed 30 minutes during requirements review; the buckets are tuned to 1.5× the nominal cadence so a normal refresh lands in `fresh`.

### Rationale

30 minutes fits the WidgetKit reload budget (~40–70 reloads/day) comfortably even with multiple widget instances, and the app triggers an immediate reload whenever it refreshes the Dashboard (so the widget is fresh whenever the user has recently used the app). The 45-minute `fresh` threshold gives one refresh interval plus headroom before "stale" appears, so the stale marker remains meaningful. The 3-hour offline threshold corresponds to the user's mental model of "something is really wrong" — at that point the backend has probably failed or the device has been offline, and the widget visibly says so.

### Alternatives Considered

- **15-minute refresh with 5/30-minute buckets (original draft)**: Rejected — peer review showed this would make `stale` the steady state because WidgetKit's actual cadence is usually slower than the hint.
- **60-minute refresh**: Rejected — user chose 30 minutes; 60 felt abandoned between app opens.
- **Skip staleness classification and always show data without age context**: Rejected — violates the "do not show untrustworthy data silently" principle.

### Consequences

**Positive:**
- Refresh budget comfortable even across 3+ widget instances.
- Staleness marker remains meaningful (rare) rather than steady-state.

**Negative:**
- Users who leave the app closed for more than 45 minutes will see "stale" labels. Combined with app-triggered reload this only happens during extended non-use.

---

## Decision 10: Widget timeline MAY include bucket-transition entries (no value extrapolation)

**Date**: 2026-04-20
**Status**: accepted

### Context

SwiftUI inside a widget does not re-evaluate `Date()` between timeline reloads, so a "12 min ago" string cannot update itself — the string rendered at reload time is frozen until the next reload. Without help, a widget that shows staleness labels only transitions states when WidgetKit decides to reload it, which may be well past the bucket boundary.

### Decision

The timeline provider MAY emit multiple entries for a single cached snapshot, at the timestamps where the staleness bucket changes (`fresh → stale`, `stale → offline`). Each such entry reuses the same snapshot data with an advanced `displayAge`. No other form of forward projection is allowed — SOC, power, and any numeric values are NEVER extrapolated.

### Rationale

Bucket-transition entries are cheap (same data, different ages) and are the only way to get the staleness UI to update without a reload. Disallowing value extrapolation is important: the data is live and cannot be projected, so pretending otherwise would lie to the user.

### Alternatives Considered

- **Single-entry timeline with periodic forced reloads**: Rejected — wastes the WidgetKit reload budget for what is essentially a label change.
- **Hide the age label and let the `fresh/stale/offline` boundary be invisible**: Rejected — contradicts [6.2](requirements.md#6.2).

### Consequences

**Positive:**
- Widget age labels transition at the right time even if the system does not reload.

**Negative:**
- The timeline provider is slightly more complex — it must compute the next two bucket boundaries and emit entries at them.

---

## Decision 11: `systemSmall` secondary metric is current load

**Date**: 2026-04-20
**Status**: accepted

### Context

Initial draft chose the small-widget secondary metric dynamically (solar if `ppv > pload`, otherwise net grid). Design-critic pointed out this will flip multiple times per hour on a partly-cloudy day as the sign of `ppv - pload` drifts across zero, giving the widget a jittery feel.

### Decision

The `systemSmall` widget shows SOC + status + current household load (`pload`) formatted via `PowerFormatting.format`, coloured red when it exceeds the user's load-alert threshold (same rule the Dashboard already uses).

### Rationale

User selected "current load only" during requirements review. Load is a stable metric (changes gradually, never flips sign), is immediately actionable ("is the house using too much right now?"), and it reuses the existing `loadAlertThreshold` colour logic so the widget and Dashboard stay consistent.

### Alternatives Considered

- **Solar-if-ppv>pload-else-net-grid (original draft)**: Rejected — flips as clouds move, making the small widget feel jittery.
- **Signed net grid**: Rejected during selection — user preferred load as a more consumption-focused glance.
- **Estimated cutoff time when discharging, else blank**: Rejected during selection — too often blank; under-utilises the space.

### Consequences

**Positive:**
- Stable, immediately-actionable content; same colour logic as the Dashboard's load row.

**Negative:**
- The small widget does not show grid direction at a glance; user must go to medium or the app for that.

---

## Decision 12: No dedicated "refresh when shown" hook; rely on app-triggered reload + iOS relevance signals

**Date**: 2026-04-20
**Status**: accepted

### Context

The user asked whether a widget can refresh the moment it becomes visible on the Home Screen / Lock Screen. WidgetKit does not expose an "on visibility" callback — `TimelineProvider.getTimeline` is called by the system on its own schedule, not on user interaction.

### Decision

The widget will rely on three independent mechanisms to feel fresh:
1. **Targeted `WidgetCenter.reloadTimelines(ofKind:)` calls from the main app**, fired after successful Dashboard `/status` refreshes under the throttling rules in Decision 13. This is the closest approximation to "refresh on visibility" — whenever the user has recently used the app, the widget is fresh the next time they glance.
2. **The `.after(now + 30 min)` policy** ([5.1](requirements.md#5.1)) for when the user has not opened the app in a while.
3. **`TimelineEntryRelevance` scoring** (Decision 18) so iOS's intelligence prioritises reloads when the data is most actionable (e.g. approaching battery cutoff).

### Rationale

Mechanism (1) is the closest thing WidgetKit offers to a "refresh on visibility". Mechanism (2) covers the "user away" case. Mechanism (3) gives iOS a signal about when this widget most benefits from a fresh reload, influencing how WidgetKit prioritises the daily budget across competing widgets.

### Alternatives Considered

- **Periodic `Task` inside the widget view body to self-refresh**: Rejected — WidgetKit view bodies do not run tasks, only produce views from entries.
- **`Button(intent:)` that triggers a manual refresh**: Rejected — adds a tap target that the user would have to remember; contradicts the "glance surface" goal.
- **Silent push notifications to force a reload**: Rejected — requires APNs wiring that is out of proportion for a two-user app.

### Consequences

**Positive:**
- Widget feels fresh during active app use without wasting reload budget when the user is away.
- No APNs or background-refresh infrastructure needed.

**Negative:**
- If the user puts the phone down with the widget visible and never opens the app, the widget will show data up to 30 minutes old before it reloads — and iOS may delay that further under budget pressure.

---

## Decision 13: Throttle `WidgetCenter` reload triggers; target both widget kinds

**Date**: 2026-04-20
**Status**: accepted

### Context

The Dashboard refreshes `/status` every 10 s while foregrounded. The initial design called `WidgetCenter.shared.reloadAllTimelines()` after every refresh, which would exhaust iOS's ~40–70 reload/day budget in a single foreground session (the budget is shared across the whole widget bundle, multiplied by how many widget instances the user has placed). Both peer review and the design-critic review flagged this as a must-fix.

### Decision

Two guards apply to the widget-reload trigger inside `DashboardViewModel.refresh`:

1. Gate on `writeIfNewer` returning `true` — if the cached snapshot is unchanged, no reload.
2. Debounce to at most one reload per 5 minutes via `lastWidgetReload` state.

The closure called is `reloadTimelines(ofKind:)` for each widget kind (`me.nore.ig.flux.widget.battery`, `me.nore.ig.flux.widget.accessory`) instead of `reloadAllTimelines()`.

### Rationale

A 5-minute debounce emits at most 12 reloads per hour of active app usage, comfortably within the daily budget even if the user keeps the app open continuously. Targeting specific kinds avoids forcing iOS to reconsider every widget (including any future kinds) on every refresh.

### Alternatives Considered

- **Call `reloadAllTimelines()` every refresh**: Rejected — reviewers unanimous that this exhausts the daily budget.
- **Call it only when the app moves to background**: Rejected — too rare to give the user a fresh widget after interacting with the app.
- **Call it after every refresh but with just the kind filter**: Rejected — still pays the budget cost per call, just more surgically.

### Consequences

**Positive:**
- Widget reload budget preserved even during extended app use.
- Only reloads when data actually changed.

**Negative:**
- Slightly more state on `DashboardViewModel` (`lastWidgetReload: Date?`).
- A user who refreshes the app, looks at the widget 2 minutes later, refreshes again, and expects an instant update may not see one — they'll see a reload within the debounce window next time the cache changes.

---

## Decision 14: 5-second fetch timeout enforced via `URLSessionConfiguration`, not `withTaskGroup`

**Date**: 2026-04-20
**Status**: accepted

### Context

The initial design wrapped `apiClient.fetchStatus()` in a `withTaskGroup` that raced it against `Task.sleep(for: .seconds(5))`, cancelling the loser. Both reviews flagged this as ineffective: `withTaskGroup.cancelAll()` cancels the Swift Task, but the underlying `URLSession.data(for:)` request does not cancel its TCP/TLS connection in response — the socket continues until URLSession's own timeout, potentially blowing through WidgetKit's ~15 s extension lifetime.

### Decision

The widget's `URLSessionAPIClient` is constructed with a dedicated `URLSession` whose `URLSessionConfiguration` has `timeoutIntervalForRequest = 5`, `timeoutIntervalForResource = 5`, and `waitsForConnectivity = false`. No TaskGroup wrapper is needed.

### Rationale

Setting the timeout on the session configuration cancels the underlying request at the networking layer — the socket tears down, the Swift Task resumes with a `URLError.timedOut`, and the widget proceeds to its cache-fallback path. This is the idiomatic iOS API for "give this request at most N seconds" and is the only way to actually interrupt a stalled network request.

### Alternatives Considered

- **Keep the TaskGroup wrapper**: Rejected — does not cancel the socket; risk of the extension being killed by WidgetKit before the request resolves.
- **Use `Task.timeout(_:)` (a hypothetical iOS 26 API)**: There is no such API on iOS 26.4; session-level timeout is the correct primitive.

### Consequences

**Positive:**
- Timeout is actually enforced; socket is torn down at 5 s.
- Clearer code — one configuration line replaces a TaskGroup.

**Negative:**
- Widget's `URLSession` is separate from the app's. Slightly more connection-pool overhead, but negligible.

---

## Decision 15: Keychain accessibility migration uses `SecItemUpdate`, not delete-then-add

**Date**: 2026-04-20
**Status**: accepted

### Context

The initial design's `KeychainAccessibilityMigrator` called `saveToken(token)`, which internally deletes the existing item and adds a new one. If the process is killed (backgrounding, OOM, user force-quit) between the delete and the add, the token is permanently lost and the user is signed out.

### Decision

The migrator uses `SecItemUpdate` to change `kSecAttrAccessible` in place, preserving the token bytes atomically. Delete-then-add is only used as a fallback when `SecItemUpdate` fails (a rare Keychain-corruption path) and logs an error when that fallback runs.

### Rationale

`SecItemUpdate` is atomic: either the accessibility class is changed or the item remains as-is, but the token is never missing. This makes the migration crash-safe for the common case. The fallback covers pathological Keychain states without changing the happy-path behaviour.

### Alternatives Considered

- **Keep delete-then-add**: Rejected — unnecessary risk of token loss.
- **Require the user to re-enter credentials after upgrade**: Rejected — breaks the silent-migration promise; poor UX.

### Consequences

**Positive:**
- Migration is crash-safe.
- Token preserved across any interruption.

**Negative:**
- `KeychainService` gains an `updateAccessibility(_:)` method beyond the original three. Small API surface addition.

---

## Decision 16: `writeIfNewer` uses strict `>` for timestamp comparison

**Date**: 2026-04-20
**Status**: accepted

### Context

The initial design used `existing.fetchedAt >= envelope.fetchedAt` to decide whether to skip a write — equal timestamps were silently dropped. Requirement [4.8](requirements.md#4.8) says "SHALL NOT overwrite an existing cached snapshot whose fetch timestamp is newer"; equality is explicitly not "newer". Reviewers flagged the `>=` as a bug disguised as a policy.

### Decision

The comparison is strict `>`: equal `fetchedAt` proceeds with the write.

### Rationale

In the race case where app and widget both compute `fetchedAt` in the same second, the second write may carry a richer or corrected payload (e.g. a slightly-later live fetch that completed in the same second). Silently dropping it discards valid data. Requirement wording matches this interpretation.

### Alternatives Considered

- **Keep `>=`**: Rejected — silently drops writes that should go through.
- **Use nanosecond-precision ordering with a tiebreaker**: Rejected — over-engineered; strict `>` is sufficient.

### Consequences

**Positive:**
- No silently-dropped writes at timestamp ties.
- Contract matches requirement wording.

**Negative:**
- Ties (rare) may see two back-to-back writes. No correctness impact.

---

## Decision 17: Wrap `kSecAttrAccessible` in `KeychainAccessibility` Swift enum

**Date**: 2026-04-20
**Status**: accepted

### Context

`FluxCore`'s original `KeychainService.readAccessibility() -> CFString?` leaked `Security.framework` ABI into the package's public surface. Consumers (widget, main app) would need `import Security` to compare against `kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly`. Additionally, raw `CFString == CFString` comparison via Swift's `==` can silently fail because the bridging is not Swift-aware — Codex flagged this during peer review.

### Decision

`FluxCore` exposes `KeychainAccessibility` as a Swift `Sendable, Equatable` enum with cases `afterFirstUnlockThisDeviceOnly`, `other(String)` (carries the raw attribute string for forward compatibility), and `missing` (attribute absent from Security framework's response). Comparisons happen on the enum, not on `CFString`. Internally the enum maps to/from `CFString` via a private `cfString` property using `as String` bridging.

### Rationale

The enum is the right Swift-side abstraction: consumers don't need to know the Security framework exists, comparisons are safe, and the `missing` case explicitly handles iOS 26's occasional omission of the attribute from `SecItemCopyMatching` results.

### Alternatives Considered

- **Expose `CFString` directly with a helper function**: Rejected — bleeds ABI; consumers must `import Security`.
- **Compare via `CFEqual`**: Rejected — still requires CFString in the public surface.

### Consequences

**Positive:**
- `FluxCore`'s public surface stays Swift-native.
- Safe, intentional comparisons.
- `missing` case documented.

**Negative:**
- Small type-mapping layer in `KeychainService` internals.

---

## Decision 18: Ship `TimelineEntryRelevance` in v1

**Date**: 2026-04-20
**Status**: accepted

### Context

The initial design marked `TimelineEntryRelevance` as "optional polish, skip if it adds complexity". The expert-level self-validation argued that because the widget's primary value is timeliness near battery cutoff, relevance scoring is not optional — iOS uses it to prioritise which widgets to keep fresh under the daily reload ceiling.

### Decision

Every `StatusEntry` carries a `TimelineEntryRelevance` score derived from staleness and proximity-to-cutoff. Scoring logic lives in a standalone `RelevanceScoring` module in `FluxCore` (consolidated there per Decision 19), with its own unit tests.

### Rationale

Relevance scoring is two lines of code at the entry-construction site plus a scoring table. For a widget whose raison d'être is "show me the cutoff time before it matters", surfacing that "this data matters more right now" to iOS's intelligence is directly load-bearing.

### Alternatives Considered

- **Skip and rely on `.after` alone**: Rejected — reviewer pushed back that this leaves the widget visible to the user but potentially reloaded less than a less-relevant widget.

### Consequences

**Positive:**
- iOS can prioritise this widget's reloads when battery is near cutoff.

**Negative:**
- One more table to keep in sync with real-world expectations. Low-cost.

---

## Decision 19: Consolidate widget-testable logic into `FluxCore`; no `FluxWidgetsTests` target

**Date**: 2026-04-20
**Status**: accepted

### Context

The original design created a separate `FluxWidgetsTests` unit-test bundle hosted by the widget extension. Xcode's "Unit Testing Bundle" template on iOS 26.4 does not allow an `.appex` (app extension) to be selected as the target-to-be-tested — only apps (and sometimes frameworks) can host unit tests. Without a host, a widget-extension logic test bundle has no way to `@testable import` the widget target's modules.

### Decision

Widget-testable logic is consolidated into `FluxCore` and tested in `FluxCoreTests`. The widget extension (`FluxWidgetsExtension`) is kept as a thin shell containing only `@main WidgetBundle`, the two `Widget` declarations, the SwiftUI views, and a thin `StatusTimelineProvider` that delegates to a testable `StatusTimelineLogic` type in `FluxCore`. Types moved into `FluxCore` as a result: `RelevanceScoring`, `WidgetAccessibility`, `StatusTimelineLogic`, `StatusEntry` (the `TimelineEntry`-conforming value type).

### Rationale

`WidgetKit` is iOS-only but importable from any iOS-targeting SPM package, including `FluxCore`. Moving the orchestration/pure-function types into the package makes them testable without the host-extension limitation, and keeps the widget extension itself as thin as WidgetKit requires. This aligns with the "`FluxCore` is the testability boundary" principle already established in Decision 2.

### Alternatives Considered

- **Host the test bundle on the main `Flux` app**: Rejected — you still can't `@testable import FluxWidgetsExtension` from a bundle hosted by the main app, so the tests couldn't reach widget types.
- **Add widget-extension source files as members of a separate test target**: Rejected — requires duplicating file membership across targets, which re-introduces the "never copy files between targets" anti-pattern from Decision 2.
- **Skip unit tests for widget logic, rely on `#Preview` blocks**: Rejected — requirement [15.1](requirements.md#15.1) mandates testability of the timeline provider with injected dependencies.

### Consequences

**Positive:**
- One test target for all widget logic (`FluxCoreTests`).
- The widget extension stays as thin as possible, reducing code that needs WidgetKit-runtime testing.
- No Xcode GUI fight to wrestle the test bundle into an unsupported hosting configuration.

**Negative:**
- `FluxCore` imports `WidgetKit`, which means any future non-iOS consumer of `FluxCore` would need to wrap the WidgetKit-dependent types behind `#if os(iOS)`. For v1 there is no such consumer, so this is a latent cost, not an immediate one.

---

## Decision 20: Widget extension target is named `FluxWidgetsExtension`

**Date**: 2026-04-20
**Status**: accepted

### Context

Xcode 26.4's "Widget Extension" template auto-appends the suffix "Extension" to whatever name the user types in the target-creation wizard. Attempting to rename the target after creation did not trigger Xcode's usual scheme/product-name rename prompt.

### Decision

Accept the name `FluxWidgetsExtension` for the target. The architectural names that matter in code (folder path `Flux/FluxWidgets/`, widget kinds `me.nore.ig.flux.widget.battery` and `…accessory`, bundle identifier `me.nore.ig.Flux.FluxWidgetsExtension`) are unchanged.

### Rationale

The target name is cosmetic — it does not appear in any user-facing surface. Fighting Xcode over a template default is not worth the churn. Documentation is updated to refer to `FluxWidgetsExtension` where it refers to the target, and `FluxWidgets` where it refers to the folder or conceptual grouping.

### Alternatives Considered

- **Rename the target via pbxproj hand-edit**: Rejected — the agent tried this with hand-generated UUIDs; UUID length was wrong (23 vs 24 chars) and the operation was aborted. Not worth retrying when the default name is harmless.
- **Delete and recreate**: Rejected — Xcode always auto-appends "Extension"; no starting name avoids the suffix.

### Consequences

**Positive:**
- Zero-risk setup — Xcode's template defaults are preserved.

**Negative:**
- The target name does not match the folder name (`FluxWidgets` vs `FluxWidgetsExtension`). A new reader briefly has to reconcile the two.

---

