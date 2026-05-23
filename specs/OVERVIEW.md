# Specs Overview

| Name | Creation Date | Status | Summary |
|------|---------------|--------|---------|
| [Infrastructure](#infrastructure) | 2026-04-12 | Done | Single CloudFormation stack for the AWS backend: VPC, ECS Fargate, Lambda, DynamoDB, SSM. |
| [Poller](#poller) | 2026-04-14 | Done | Go ECS Fargate task that polls AlphaESS, writes five DynamoDB tables, computes off-peak deltas. |
| [Lambda API](#lambda-api) | 2026-04-15 | Done | Go Lambda behind a Function URL serving /status, /history, /day from DynamoDB. |
| [iOS App](#ios-app) | 2026-04-16 | Done | SwiftUI iOS 26+ app: Dashboard, History, Day Detail, Settings; reads only the Lambda API. |
| [Realtime Energy](#realtime-energy) | 2026-04-16 | Done | Compute today's energy by integrating live readings instead of relying on 6-hourly snapshots. |
| [Peak Usage Periods](#peak-usage-periods) | 2026-04-17 | Done | Day detail card highlighting top high-consumption periods outside the off-peak window. |
| [Add Widgets](#add-widgets) | 2026-04-21 | Done | WidgetKit home and lock-screen widgets surfacing battery state and live power data. |
| [History Multi Card](#history-multi-card) | 2026-04-26 | Done | History screen rewrite: solar / grid (peak vs off-peak) / battery cards with shared selection. |
| [Evening / Night Stats](#evening--night-stats) | 2026-04-26 | Done | Day detail card showing usage during the no-solar evening (sunset → midnight) and night (midnight → sunrise) periods. |
| [Peak Usage Stats](#peak-usage-stats) | 2026-04-27 | Done | Day detail card replacing Evening/Night with five chronological load blocks (Night, Morning Peak, Off-Peak, Afternoon Peak, Evening). |
| [Day Notes](#day-notes) | 2026-04-28 | Done | Per-date free-text note (≤200 graphemes) shared across users; new `flux-notes` DynamoDB table and PUT /note endpoint; rendered on Dashboard, History, Day Detail; edited only on Day Detail. |
| [Daily Derived Stats](#daily-derived-stats) | 2026-04-29 | Done | Pre-compute three reading-derived per-day stats (`findDailyUsage`, `findMinSOC`, `findPeakPeriods`) in the poller hourly against yesterday; persist on `flux-daily-energy` via UpdateItem; `/day` and `/history` read storage for past dates, live-compute for today. Unblocks history-daily-usage (T-1022). |
| [History Daily Usage](#history-daily-usage) | 2026-04-30 | Done | New History card rendering one stacked bar per day across the five chronological blocks (Night, Morning Peak, Off-Peak, Afternoon Peak, Evening) over the 7/14/30-day range, plus a fix to the cache upsert path so derived fields backfill on already-cached rows with observability on unexpected nil-overwrites. UI-only consumer of the data shipped by Daily Derived Stats. |
| [macOS App](#macos-app) | 2026-05-01 | Done | Native macOS 26+ build of Flux (no Catalyst, no Designed-for-iPad). Adds dedicated Settings scene, menu commands (⌘R, ←/→), single main window that quits on close, refresh tiers via `appearsActive`, iCloud Keychain + `NSUbiquitousKeyValueStore` credential sync (no migrator), and a macOS Control Center widget alongside the existing home-screen widgets. iOS scenePhase pause preserved unchanged. T-1081. |
| [History Usage Stats](#history-usage-stats) | 2026-05-07 | Done | New "Period overview" card on the History screen with eight tiles for the active 7/14/30-day range: Total usage, Total solar, Exported, Peak imports, Avg night, Most usage, Most solar, Lowest SoC. Day-record tiles tap-select the day across the existing chart cards. UI-only; reuses existing `/history` data. T-896. |
| [What's New](#whats-new) | 2026-05-08 | Done | User-friendly "What's New" sheet auto-presented after a `MARKETING_VERSION` bump and reachable from Settings, distinct from the engineering `CHANGELOG.md`. Hand-authored Swift catalogue in `FluxCore`, per-device app-group `UserDefaults` last-seen tracking with a `"1.0"` seed for pre-feature upgrades. iOS + macOS. T-1112. |
| [Solar by Block](#solar-by-block) | 2026-05-09 | Done | Adds solar production (kWh) per daylight block (morning peak, off-peak, afternoon peak) on the Day Detail five-block panel. Backend integrates `ppv` per block in `derivedstats.Blocks()`, persists via the existing poller summarisation, and ships a one-shot backfill CLI (`cmd/backfill-solar`) that patches `solarKwh` in place without altering historic `totalKwh`. iOS renders the per-block kWh value in amber next to the existing usage figure on daylight rows. T-1162. |
| [Stat Comparisons](#stat-comparisons) | 2026-05-10 | Done | Opt-in Compare toggle on Day Detail with a period chip (Yesterday / 7 days ago) that renders an absolute-kWh delta as a sub-line under each SummaryBlock row and DayInFiveBlocksPanel value, in the same lighter/smaller treatment as the five-block time-range captions. Client-only — comparison day fetched via the existing `/day` endpoint; backend untouched. T-1161. |
| [Zoom in on Graphs](#zoom-in-on-graphs) | 2026-05-12 | Done | Tap-to-enlarge affordance on the five active chart cards (History solar / grid usage / daily usage; Day Detail power and battery combined). iOS uses a `fullScreenCover` with a UIKit bridge requesting landscape (orientation lock restored on every dismissal path, portrait fallback if denied). macOS opens a separate `WindowGroup(for: ChartKind)` per chart, identity by view type only — re-expand brings the existing window forward; `.defaultLaunchBehavior(.suppressed)` prevents launch-time restoration; `.windowManagerRole(.associated)` keeps the Window menu honest. Selection interactivity preserved: History reuses `historySelectionOverlay` with a real `HistoryDragGate`; Day Detail uses `XSelectionQuiescenceGate` (400 ms quiet window) since `chartXSelection` has no lifecycle. Day Detail `selectedDate` lifted from the two chart views into `DayDetailView` so inline and enlarged share state. macOS data flows via a per-scene `ChartSceneObserver` (60 s polling) keyed by `ChartScopeRegistry`, not by lifting per-screen view models. iOS sheet state survives backgrounding via `@SceneStorage`. T-1215. |
| [SoC Alerts](#soc-alerts) | 2026-05-19 | Done | User-defined battery state-of-charge alerts. Per-device rules pair a percentage threshold with a daily time window (HH:MM device-local, cross-midnight supported); the Go poller evaluates rules each 10s cycle and dispatches APNs pushes via `sideshow/apns2` (entitlements already provisioned). Three new DynamoDB tables (`flux-devices`, `flux-soc-rules`, `flux-soc-fire-state`). Fires once per (rule, local-window-start day) — collapse-id `base64url(SHA256(deviceId|ruleId|date))[:22]` keeps APNs under its 64-byte cap. In-process previous-SoC comparator keyed by `(deviceRule, windowStartDate)` and tagged with rule `UpdatedAt` so Lambda rule edits propagate without IPC. Pushes go through a 64-deep / 4-worker bounded queue so APNs RTT cannot stall the live-poll cadence. Lambda routing migrates to `http.ServeMux` (Go 1.22+ path params). Stable device identifier is a UUID in container `UserDefaults` (not Keychain, not app-group — so reinstall resets). iOS/macOS Settings → Alerts list/add/edit/delete sheet driven by a new `FluxCore/Notifications/` module; `FluxiOSAppDelegate` moves to its own file alongside new APNs callbacks. Orphan device rows (no successful registration in 30 days) are cascade-deleted by the existing midnight pass with conditional delete to dodge the re-registration race. T-1288. |
| [iPad Adaptive Layout](#ipad-adaptive-layout) | 2026-05-22 | Done | iPad-tailored shell: `NavigationSplitView(.balanced)` with sidebar at regular horizontal size class, falling back to the iPhone V5 `FluxiOSRoot` + `FluxTabBar` at compact (Slide Over / narrow Split View). Gate on `userInterfaceIdiom == .pad && hSizeClass == .regular` so iPhone Plus/Max landscape stays on the iPhone shell. Dashboard / History / Day Detail get adaptive multi-column layouts above 700 / 1000 pt detail widths via a new `AdaptiveColumnsLayout` helper that collapses one column at `.accessibility4`+. Dashboard / History / Today `DayDetailViewModel` instances hoisted into `AppNavigationView` so cached state survives size-class transitions; the Today entry handles midnight rollover via a new `setDate(_:)` on the hoisted VM (no `.id(today)` rebuild, so chart highlight / scroll / note-editor state is preserved). `reloadDependencies()` rebuilds hoisted VMs when the API client identity changes after a credentials edit. Two-way `selectedScreen ↔ iosTab` sync via a pure `syncedState(selected:tab:)` reducer keeps the two shells coherent across transitions. Settings stays as a sheet (no sidebar entry; reached via per-screen gear and an iPad toolbar `ToolbarItem(.primaryAction)` gear). iPhone V5 and macOS shells unchanged. T-1150. |
| [Battery Can't Empty Before Off-Peak](#battery-cant-empty-before-off-peak) | 2026-05-23 | Done | Dashboard hero shows a "won't empty before HH:MM" indicator when, even at the 5 kW max sustained discharge rate, the battery cannot reach the 5 % cutoff before the next off-peak window opens. New `pbat`-independent boolean `cantEmptyBeforeOffpeak` on `battery` in `/status`, computed inside the existing `liveFresh` branch and reusing `derivedstats.ParseOffpeakWindow`. Swift `BatteryInfo` gains an optional `Bool` with a defaulted memberwise init so existing constructors keep compiling; `DashboardHeroPanel` swaps in a new `secondaryText` subview (no `Mode` enum changes) with a literal VoiceOver label `"Battery won't empty before off-peak at <HH:MM>"`. iOS+macOS shared. Inherits the no-midnight-spanning-window limit from `ParseOffpeakWindow`. T-1327. |
| [Daily Costs](#daily-costs) | 2026-05-23 | Planned | New Day Detail "costs" card (peak imports cost / solar feed-in income / net / off-peak savings) and a new History "Period costs" card with four aggregate tiles. Backed by a single shared tenant-wide pricing configuration: new `flux-pricing` DynamoDB table with a sentinel row enforcing AC 1.9 atomicity via `TransactWriteItems` (first use in the repo); five Lambda CRUD endpoints plus a transactional close-and-create remediation for the editor's overlap flow. Rates stored as Float64 with 4-dp normalisation on write and bounded to [0, $10/kWh]. Pricing dates are `YYYY-MM-DD` strings interpreted in `Australia/Melbourne`. Cost computation is client-side in FluxCore (`DayCosts.costs(forDate:in:)` on `DaySummary`, `PeriodCosts.compute(days:pricing:)`) — no backend enrichment of `/day` or `/history`. `@Observable @MainActor` `PricingService` injects into Day Detail / History view models; refresh policy: on Settings open, Day Detail load, History range change, and after any mutation. Card hides only when the day's aggregate row is missing; zero kWh is a legitimate value. iOS + macOS Settings UI mirrors the SoC Alerts pattern. T-1326. |

---

## Infrastructure

Single CloudFormation stack for the AWS backend: VPC, ECS Fargate, Lambda, DynamoDB, SSM.

- [decision_log.md](infrastructure/decision_log.md)
- [design.md](infrastructure/design.md)
- [implementation.md](infrastructure/implementation.md)
- [prerequisites.md](infrastructure/prerequisites.md)
- [requirements.md](infrastructure/requirements.md)
- [tasks.md](infrastructure/tasks.md)

## Poller

Go ECS Fargate task that polls AlphaESS, writes five DynamoDB tables, computes off-peak deltas.

- [decision_log.md](poller/decision_log.md)
- [design.md](poller/design.md)
- [implementation.md](poller/implementation.md)
- [requirements.md](poller/requirements.md)
- [tasks.md](poller/tasks.md)

## Lambda API

Go Lambda behind a Function URL serving /status, /history, /day from DynamoDB.

- [decision_log.md](lambda-api/decision_log.md)
- [design.md](lambda-api/design.md)
- [explanation.md](lambda-api/explanation.md)
- [implementation.md](lambda-api/implementation.md)
- [requirements.md](lambda-api/requirements.md)
- [tasks.md](lambda-api/tasks.md)

## iOS App

SwiftUI iOS 26+ app: Dashboard, History, Day Detail, Settings; reads only the Lambda API.

- [decision_log.md](ios-app/decision_log.md)
- [design.md](ios-app/design.md)
- [implementation.md](ios-app/implementation.md)
- [prerequisites.md](ios-app/prerequisites.md)
- [requirements.md](ios-app/requirements.md)
- [tasks.md](ios-app/tasks.md)

## Realtime Energy

Compute today's energy by integrating live readings instead of relying on 6-hourly snapshots.

- [decision_log.md](realtime-energy/decision_log.md)
- [design.md](realtime-energy/design.md)
- [implementation.md](realtime-energy/implementation.md)
- [requirements.md](realtime-energy/requirements.md)
- [tasks.md](realtime-energy/tasks.md)

## Peak Usage Periods

Day detail card highlighting top high-consumption periods outside the off-peak window.

- [decision_log.md](peak-usage-periods/decision_log.md)
- [design.md](peak-usage-periods/design.md)
- [implementation.md](peak-usage-periods/implementation.md)
- [requirements.md](peak-usage-periods/requirements.md)
- [tasks.md](peak-usage-periods/tasks.md)

## Add Widgets

WidgetKit home and lock-screen widgets surfacing battery state and live power data.

- [decision_log.md](add-widgets/decision_log.md)
- [design.md](add-widgets/design.md)
- [implementation.md](add-widgets/implementation.md)
- [prerequisites.md](add-widgets/prerequisites.md)
- [requirements.md](add-widgets/requirements.md)
- [tasks.md](add-widgets/tasks.md)

## History Multi Card

History screen rewrite: solar / grid (peak vs off-peak) / battery cards with shared selection.

- [pre-push-review.md](history-multi-card/pre-push-review.md)

## Evening / Night Stats

Day detail card showing usage during the no-solar evening (sunset → midnight) and night (midnight → sunrise) periods, computed by `/day` from existing readings with a static Melbourne sunrise/sunset table as fallback.

- [decision_log.md](evening-night-stats/decision_log.md)
- [design.md](evening-night-stats/design.md)
- [requirements.md](evening-night-stats/requirements.md)
- [tasks.md](evening-night-stats/tasks.md)

## Peak Usage Stats

Day detail card replacing Evening/Night with five chronological load blocks (Night, Morning Peak, Off-Peak, Afternoon Peak, Evening), each carrying total kWh, average kWh/h, and percent of day. Replaces the `eveningNight` API field with `dailyUsage`.

- [decision_log.md](peak-usage-stats/decision_log.md)
- [design.md](peak-usage-stats/design.md)
- [requirements.md](peak-usage-stats/requirements.md)
- [tasks.md](peak-usage-stats/tasks.md)

## Day Notes

Per-date free-text note (≤200 graphemes after NFC + trim) shared across users. Adds the Lambda's first write endpoint (`PUT /note`) and a new `flux-notes` DynamoDB table with PITR. Notes bundled into `/status`, `/history`, `/day` responses; rendered read-only on Dashboard (today) and History (selected day); editable on Day Detail.

- [decision_log.md](day-notes/decision_log.md)
- [design.md](day-notes/design.md)
- [requirements.md](day-notes/requirements.md)
- [tasks.md](day-notes/tasks.md)

## Daily Derived Stats

Pre-compute three reading-derived per-day stats (`findDailyUsage`, `findMinSOC`, `findPeakPeriods`) in the poller via an hourly summarisation pass against yesterday; persist on the existing `flux-daily-energy` row via `UpdateItem`; both `/day` and `/history` read derivedStats from storage for past dates and live-compute for today. New `internal/derivedstats` shared package decouples the helpers from `internal/api`. Unblocks history-daily-usage (T-1022) by avoiding a 30-day rollup that would otherwise re-fetch ~258k readings per call against the 30-day TTL.

- [decision_log.md](daily-derived-stats/decision_log.md)
- [design.md](daily-derived-stats/design.md)
- [requirements.md](daily-derived-stats/requirements.md)
- [tasks.md](daily-derived-stats/tasks.md)

## History Daily Usage

New History card rendering one stacked bar per day across the five chronological blocks (Night, Morning Peak, Off-Peak, Afternoon Peak, Evening) over the 7/14/30-day range, with column-level selection synced to the existing Solar/Grid/Battery cards and a chart density spec for 30-day readability. Bundles the cache upsert fix so the four derived fields (`dailyUsage`, `socLow`, `socLowTime`, `peakPeriods`) backfill on already-cached `CachedDayEnergy` rows on every successful response, with a warning-level log emitted via `os.Logger` whenever a previously non-nil value is overwritten with nil (observability for unexpected backend regressions). UI-only consumer of the data shipped by Daily Derived Stats; no backend, FluxCore, or SwiftData migration changes.

- [decision_log.md](history-daily-usage/decision_log.md)
- [design.md](history-daily-usage/design.md)
- [requirements.md](history-daily-usage/requirements.md)
- [tasks.md](history-daily-usage/tasks.md)

## macOS App

Native macOS 26+ build of Flux that reuses FluxCore, the existing widget extension, and ~all of the iOS view code, with platform-specific shells for app entry (Settings scene + commands + NSApplicationDelegate), navigation chrome (sidebar filter, Liquid Glass modifiers), Dashboard refresh tier signal (Mac-only via `appearsActive`), credential plane (iCloud Keychain + `NSUbiquitousKeyValueStore` mirror — no migrator), and a Control Center widget. Single main window that quits on close; ⌘R + ←/→ keyboard shortcuts; iOS scenePhase pause preserved. Distribution via TestFlight Mac / Mac App Store under the same `me.nore.ig.Flux` bundle ID. T-1081.

- [decision_log.md](macos-app/decision_log.md)
- [design.md](macos-app/design.md)
- [prerequisites.md](macos-app/prerequisites.md)
- [requirements.md](macos-app/requirements.md)
- [tasks.md](macos-app/tasks.md)

## History Usage Stats

New "Period overview" card on the History screen rendering eight stat tiles for the active 7/14/30-day range: Total usage, Total solar, Exported, Peak imports, Avg night, Most usage, Most solar, Lowest SoC. The three day-record tiles tap-select the day across the existing chart cards via the existing `onSelect` plumbing. UI-only feature: every value is derived from the existing `/history` response — `solarTotalKwh`, `exportTotalKwh`, `peakImportTotalKwh`, and `dailyUsageTotalKwh` are reused; four new aggregates (Avg night, Most usage, Most solar, Lowest SoC) are added to `PeriodSummary` in the same single-pass `Totals` accumulator. `HistoryCardChrome.kpi` becomes optional so the card's KPI slot shows the inclusive date range covered. T-896.

- [decision_log.md](history-usage-stats/decision_log.md)
- [design.md](history-usage-stats/design.md)
- [requirements.md](history-usage-stats/requirements.md)
- [tasks.md](history-usage-stats/tasks.md)

## What's New

User-friendly "What's New" sheet shown to non-technical users after an app update, distinct from the engineering `CHANGELOG.md`. Auto-presents on first launch after a `CFBundleShortVersionString` bump and is reachable from Settings on both iOS and macOS. Release entries are hand-authored as a typed Swift catalogue in `FluxCore` (`WhatsNew/`); a pure `WhatsNewCoordinator` value type maps inputs (catalogue, installed version, last-seen, has-other-prefs) to one of `present`/`silentSet`/`skip`. Per-device app-group `UserDefaults` stores `lastSeenWhatsNewVersion`; pre-feature v1.0 upgrades are seeded with `"1.0"` so existing users see the v1.1 entry on first launch. Skip-version stacks newest-first; downgrade is a no-op; future-version entries are filtered. Optional per release. English-only; no localization wrapping. T-1112.

- [decision_log.md](whats-new/decision_log.md)
- [design.md](whats-new/design.md)
- [prerequisites.md](whats-new/prerequisites.md)
- [requirements.md](whats-new/requirements.md)
- [tasks.md](whats-new/tasks.md)

## Solar by Block

Adds solar production (kWh) per daylight block (morning peak, off-peak, afternoon peak) on the Day Detail five-block panel. Backend extends `derivedstats.DailyUsageBlock` with an optional `SolarKwh *float64`, computed via a sibling `integratePpv` (mirrors the existing `integratePload` algorithm — half-open `[start, end)`, 60s pair-gap rule, negative-clamp). Persistence rides through the existing once-per-day poller summarisation; no new write paths. A standalone `cmd/backfill-solar` CLI patches `solarKwh` in place on existing rows by `Kind`, preserving historic `totalKwh` and boundary metadata even when readings have been partially TTL-pruned. iOS extends the model with `solarKwh: Double?` (default-`nil` init keeps existing call sites source-compatible) and renders the per-block kWh value in amber next to the existing usage figure on daylight rows; null omits the value entirely. Out of scope: Dashboard / History solar splits. T-1162.

- [decision_log.md](solar-by-block/decision_log.md)
- [design.md](solar-by-block/design.md)
- [requirements.md](solar-by-block/requirements.md)
- [tasks.md](solar-by-block/tasks.md)

## Stat Comparisons

Opt-in Compare toggle on Day Detail with a period chip (Yesterday / 7 days ago) that renders a signed absolute-kWh delta as a right-aligned sub-line directly beneath each SummaryBlock row primary value and beneath each value column on the DayInFiveBlocksPanel rows. Sub-line uses the same `touTime` + `tertiaryText` treatment already used for the five-block time-range captions; chevrons (▲ / ▼ / —) encode direction only — no good/bad colouring. Client-only feature: comparison day data is fetched via a second call to the existing `/day` endpoint (past-date `/day` is a cheap point-read on `flux-daily-energy`), so the backend is unchanged. State machine has four cases (`.off / .loading / .ready / .unavailable`) with three `Task.isCancelled` guards in `loadComparison` to prevent stale writes on rapid period or day-nav changes. Slot height is reserved on every supported row when Compare is on so the card stays jitter-free across loading transitions; off-state rows revert to their pre-feature layout exactly. New `SublineContent` enum makes the three-state slot semantics (hidden / reserved / text) unrepresentable as anything else. Toggle and period preferences persist per-device via `UserDefaults.fluxAppGroup` (`@AppStorage`); no iCloud sync. Out of scope: comparisons on Battery / Peak Usage cards, the three Day Detail charts, percentage deltas, per-stat semantic colouring, same-time-of-day cutoff for Today, last-month / last-year periods, localization, and explicit Dynamic Type / RTL / iPad column-variant ACs (deferred per Decision 14). T-1161.

- [decision_log.md](stat-comparisons/decision_log.md)
- [design.md](stat-comparisons/design.md)
- [requirements.md](stat-comparisons/requirements.md)
- [tasks.md](stat-comparisons/tasks.md)

## Zoom in on Graphs

Tap-to-enlarge affordance on the five currently rendered chart cards: HistorySolarCard, HistoryGridUsageCard, HistoryDailyUsageCard, PowerChartView, BatteryCombinedChartView. The trigger is a dedicated expand button (SF Symbol `arrow.up.left.and.arrow.down.right`, top-trailing 8 pt inset) overlaid on each chart's drawing area — no chart-surface gesture is added, so it cannot conflict with the existing `chartXSelection` (Day Detail) or `historySelectionOverlay` drag (History). On iOS the enlarged view is a `fullScreenCover` wrapped in `OrientationLandscapeScope`, a UIKit bridge that calls `UIWindowScene.requestGeometryUpdate(.iOS(.landscape))` and rolls back via a ref-counted `OrientationLock` on every dismissal path (Close, top-handle drag, tab-switch, deep pop). Portrait fallback when the geometry request is denied. AC 2.3 renegotiated to a visible 32 pt drag-indicator pill above the title rather than swipe-from-anywhere, because `fullScreenCover` has no system swipe-to-dismiss and a hidden full-area gesture would fight chart selection. Sheet state survives backgrounding via `@SceneStorage("expandedChart")`. On macOS, `WindowGroup("Chart", id: "chart-detail", for: ChartKind.self)` with `.defaultLaunchBehavior(.suppressed)` and `.windowManagerRole(.associated)`. Window identity is `ChartKind` only (no payload) — reopening the same kind brings the existing window forward (SwiftUI's `for:` semantics); different kinds open additional windows; date/range state mutates in-place. Data for the enlarged scene is delivered by a self-contained `ChartSceneObserver` polling at 60 s (not by lifting `HistoryViewModel`/`DayDetailViewModel` to the app root), keyed via a small app-root `ChartScopeRegistry` written by the expansion action immediately before `openWindow`. Selection interactivity preserved inline-style: History reuses `historySelectionOverlay` with a `HistoryDragGate` (real `DragGesture` begin/end); Day Detail uses `XSelectionQuiescenceGate` (400 ms quiet window after the most recent `chartXSelection` change), because `chartXSelection` is a binding with no lifecycle. Day Detail `selectedDate` lifted from `PowerChartView`/`BatteryCombinedChartView` into `DayDetailView` so iOS inline and enlarged share state and AC 4.4 holds. Out of scope: pinch-zoom/pan, new chart types, the three dormant chart views (HistoryBatteryCard, SOCChartView, BatteryPowerChartView), side-by-side comparison, export/print/share, widget changes. T-1215.

- [decision_log.md](zoom-in-on-graphs/decision_log.md)
- [design.md](zoom-in-on-graphs/design.md)
- [requirements.md](zoom-in-on-graphs/requirements.md)
- [tasks.md](zoom-in-on-graphs/tasks.md)

## SoC Alerts

User-defined battery state-of-charge alerts. Each device manages up to 10 rules; a rule pairs a 1–99% threshold with a daily HH:MM time window in device-local time (cross-midnight supported via `start > end`; end-of-day expressed as `00:00`). The Go poller evaluates rules each 10s cycle and dispatches APNs pushes via `github.com/sideshow/apns2` (entitlements `aps-environment` and `remote-notification` background mode already provisioned). Three new DynamoDB tables: `flux-devices` (PK `deviceId`), `flux-soc-rules` (PK `deviceId`, SK `ruleId`), `flux-soc-fire-state` (PK `deviceRule = deviceId#ruleId`, SK `windowStartDate`, TTL 7d). Five new Lambda endpoints behind the existing bearer-token auth via a `http.ServeMux` migration (Go 1.22+ path params). Fires at most once per (rule, local-window-start-day); the `(deviceId, ruleId, windowStartDate)` triple drives both the fire-state row and the APNs `apns-collapse-id` (hashed to fit Apple's 64-byte cap, Decision 14). Fire-state `PutIfAbsent` survives the rolling-deploy two-evaluator overlap. The previous-in-window comparator lives in poller process memory keyed by `(deviceRule, windowStartDate)` and tagged with rule `UpdatedAt` — yesterday's last value cannot poison today's first evaluation, and Lambda rule edits propagate to the comparator within ≤40 s (cache refresh + poll cycle) without IPC. Pushes go through a 64-deep / 4-worker bounded queue so APNs RTT cannot stall the live-poll cadence; queue overflow drops with fire-state row left in place. Stable device identifier is a UUID in the app's container `UserDefaults` (not Keychain, not app-group, so reinstall resets per Decision 8). New `FluxCore/Notifications/` module (`DeviceIdentifier`, `SoCAlertRule`, `SoCAlertsService`, `NotificationAuthService`); `FluxiOSAppDelegate` moves to its own file with new `didRegisterForRemoteNotificationsWithDeviceToken` callbacks on both platforms. Settings → Alerts iOS and macOS UI (list, editor sheet, denial banner). Orphan device rows (no successful registration in 30 consecutive days) are cascade-deleted by the existing midnight pass with conditional `DeleteItem` on `lastRegisteredAt` to handle the re-registration race; 24h-fresh fire-state guards in-flight pushes. APNs SecureString params at `/flux/apns/*`. Out of scope: rising-edge alerts, iCloud rule sync, rule sharing between users, snooze/mute, rich push payloads, per-rule re-fire policy. T-1288.

- [decision_log.md](soc-alerts/decision_log.md)
- [design.md](soc-alerts/design.md)
- [prerequisites.md](soc-alerts/prerequisites.md)
- [requirements.md](soc-alerts/requirements.md)
- [tasks.md](soc-alerts/tasks.md)

## iPad Adaptive Layout

iPad-tailored shell that swaps the stretched iPhone V5 tab-bar layout for a `NavigationSplitView(.balanced)` with sidebar at regular horizontal size class. Compact widths (Slide Over, narrow Split View) fall back to the existing `FluxiOSRoot` + `FluxTabBar` shell verbatim. The shell branch in `AppNavigationView.iOSRoot` gates on `userInterfaceIdiom == .pad && hSizeClass == .regular` so iPhone Plus/Max landscape stays on the iPhone shell. Dashboard, History, and Day Detail gain multi-column layouts above 700 / 1000 pt detail widths via a new `AdaptiveColumnsLayout` helper; the helper drops one column at `.accessibility4`+ so the existing AC 8.1 / AC 8.2 readability rules hold. Dashboard, History, and the Today `DayDetailViewModel` are hoisted into `AppNavigationView` as `@State` so cached state survives size-class transitions (AC 6.4). Midnight rollover on the Today sidebar entry is handled by a new `DayDetailViewModel.setDate(_:)` method on the hoisted instance rather than a `.id(today)` rebuild — chart highlight, scroll position, and note-editor state are preserved. `reloadDependencies()` rebuilds the three hoisted VMs when the API client identity changes after a credentials edit. A pure `syncedState(selected: Screen?, tab: FluxTab)` reducer drives two `onChange` handlers in `AppNavigationView` so sidebar selection and `FluxiOSRoot` tab stay coherent through size-class transitions. Settings stays as a sheet (no sidebar entry; reached via the existing per-screen gear and a new iPad toolbar `ToolbarItem(.primaryAction)` gear). The iPhone V5 shell and the macOS `NavigationSplitView` shell are unchanged. T-1150.

- [decision_log.md](ipad-adaptive-layout/decision_log.md)
- [design.md](ipad-adaptive-layout/design.md)
- [implementation.md](ipad-adaptive-layout/implementation.md)
- [requirements.md](ipad-adaptive-layout/requirements.md)
- [tasks.md](ipad-adaptive-layout/tasks.md)

## Battery Can't Empty Before Off-Peak

Dashboard hero shows a "won't empty before HH:MM" indicator when, even at the 5 kW max sustained discharge rate, the battery cannot reach the 5 % cutoff before the next off-peak window opens. New `pbat`-independent boolean `cantEmptyBeforeOffpeak` on `battery` in `/status`, computed inside the existing `liveFresh` branch and reusing `derivedstats.ParseOffpeakWindow`. Swift `BatteryInfo` gains an optional `Bool` with a defaulted memberwise init so existing constructors keep compiling; `DashboardHeroPanel` swaps in a new `secondaryText` subview (no `Mode` enum changes) with a literal VoiceOver label `"Battery won't empty before off-peak at <HH:MM>"`. iOS+macOS shared via FluxCore. Inherits the no-midnight-spanning-window limit from `ParseOffpeakWindow`. T-1327.

- [decision_log.md](battery-cant-empty-before-offpeak/decision_log.md)
- [design.md](battery-cant-empty-before-offpeak/design.md)
- [requirements.md](battery-cant-empty-before-offpeak/requirements.md)
- [tasks.md](battery-cant-empty-before-offpeak/tasks.md)

## Daily Costs

New Day Detail "costs" card (peak imports cost / solar feed-in income / net / off-peak savings) and a new History "Period costs" card with four aggregate tiles. Backed by a single shared tenant-wide pricing configuration: new `flux-pricing` DynamoDB table with a singleton sentinel row (`pricingId = "__open_ended"`) enforcing AC 1.9 atomicity via `TransactWriteItems` — first use of transactional writes in the repo. Five Lambda CRUD endpoints (`GET/POST/PUT/DELETE /pricing` + `POST /pricing/replace-open-ended`) behind the existing bearer-token auth. Pricing periods carry a required `startDate`, optional `endDate`, and three rates (peak grid, solar feed-in, off-peak savings) in AUD per kWh; rates stored as Float64 with strict 4-dp normalisation on write and bounded to [0, $10/kWh]. Validation order is `inverted_dates → overlap → rate_precision → rate_out_of_range → second_open_ended`; closed-period overlap is best-effort under concurrency, AC 1.9 is fully race-safe via the sentinel. Pricing dates are `YYYY-MM-DD` strings interpreted in `Australia/Melbourne` and day-membership is lexicographic string compare. Cost computation is client-side in FluxCore: `DaySummary.costs(forDate:in:)` (Day Detail), `DayEnergy.costs(in:)` (History), `PeriodCosts.compute(days:pricing:)` — no `/day` or `/history` response changes. When the off-peak split is `nil` for a day, peak imports kWh falls back to `eInput` and off-peak savings is $0.00 (Decision 23). A day is "priced" iff a pricing period covers it AND the daily-energy aggregate row exists; zero kWh values are legitimate (overcast, full-self-consumed). `@Observable @MainActor` `PricingService` injects into `DayDetailViewModel`, `HistoryViewModel`, and the new Settings flow; refreshes on Settings open, Day Detail load, History range change, and after any mutation (fold response + fire-and-forget refetch). iOS + macOS Settings UI mirrors the SoC Alerts pattern (list, editor sheet, swipe-to-delete) with a one-tap overlap-remediation button that invokes the atomic close-and-create endpoint. History Period Overview gets a separate "Period costs" card below the existing 8-tile overview rather than extending it, so the partial-coverage caption ("N of M days priced") has a natural home and the card disappears cleanly when no pricing exists. Out of scope: per-user pricing, multi-currency, shoulder/peak-window tariff bucket beyond the existing off-peak SSM window, frozen cost snapshots, cost figures on Dashboard / widgets / Control Center / What's New, CSV/PDF export. T-1326.

- [decision_log.md](daily-costs/decision_log.md)
- [design.md](daily-costs/design.md)
- [requirements.md](daily-costs/requirements.md)
- [tasks.md](daily-costs/tasks.md)
