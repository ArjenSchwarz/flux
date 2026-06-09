# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added

- **Week-to-date and month-to-date History ranges** (T-1361). The History range control adds two calendar-anchored options — **Wk** (this week so far) and **Mo** (this month so far) — alongside the fixed 7/14/30-day ranges, giving a five-segment control in the order 7d / 14d / 30d / Wk / Mo with 7d still the default. Boundaries are computed against the same Sydney calendar that keys the stored data, with the week's first day taken from the device locale, and are recomputed on every load (range switch, pull-to-refresh, screen appearance). The `/history` endpoint now accepts any range from 1 to 31 days, so a full month-to-date on the 31st of a 31-day month is no longer clipped to 30. The offline cache fallback is bounded by the computed week/month start, so a gappy cache never surfaces a day before the period start, and it now auto-selects the newest cached day (matching the online behaviour) instead of the oldest.

### Changed

- **History overview hard numbers now include today.** Total solar, Total usage, Most usage, and Most solar (and the Battery card's discharged/charged totals) now count today's value, matching Peak imports, Exported, and Lowest SoC which already did. A range whose only data is today (e.g. Week-to-date on the week's first day) now shows real numbers and the daily-usage bar instead of em-dashes. Per-day averages (Solar/day, Battery/day, Daily-usage/day, Avg night) still exclude today's partial day so they aren't skewed.
- **Week-to-date and month-to-date charts reserve space for the full period.** A partly-elapsed week or month now draws its bars left-aligned with empty space held for the days not yet elapsed — Wk reserves the full 7-day week, Mo the full calendar month — so the layout stays consistent from the first day of the period instead of a lone bar stretching to fill the width. The fixed 7/14/30-day ranges are unchanged.
- **SwiftLint now passes `--strict` cleanly.** Added `Flux/.swiftlint.yml` (the project had none, so the linter was scanning test targets and `.build-xc/` artifacts): build directories (`.build`, `.build-xc`, `DerivedData`) and the `FluxTests` / `FluxUITests` / `FluxCore/Tests` targets are now excluded. The 26 remaining production violations were fixed rather than suppressed — short identifiers renamed to meaningful names (including the `registerDeviceIfNeeded` `tz:` → `timeZone:` label), three over-length lines reflowed, a scoped `line_length` exception around the release-note copy in `WhatsNewCatalogue`, and a per-file `file_length` disable on `HistoryDerivedState` matching the convention already used by `DayDetailView` and `APIModels`. No app behaviour changes.

## [1.5] - 2026-06-02

### Fixed

- **Dashboard live trio no longer wraps large grid values.** When Solar, House, or Grid power reached 10 kW or more (e.g. "11.51 kW"), the trailing digit wrapped onto a second line. Large values now use a slightly smaller font so the value and its unit stay on one line; values under 10 kW are unchanged.

## [1.4] - 2026-05-31

### Added

- **Daily Costs** (T-1326). Day Detail now shows a costs card — peak imports, solar feed-in income, net, and off-peak savings — and the History range shows a four-tile Period Costs card with an "N of M days priced" caption when coverage is partial. Manage time-of-use pricing in Settings → Pricing: add, edit, and delete bands with peak, feed-in, and off-peak savings rates, with validation banners for inverted, overlapping, or out-of-range entries.
- **iPad adaptive layout** (T-1150). On iPad at regular width (full-screen and ≥ ½ Split View), Flux now uses a sidebar — Dashboard / Today / History — with a detail column instead of the stretched iPhone layout, and each screen reflows into multiple columns as width allows. Settings opens as a sheet from a toolbar gear. Compact widths (Slide Over, narrow Split View) keep the tab-bar shell.
- **macOS interface refresh** (T-1342). The macOS app adopts the adaptive multi-column layout, moves Day Detail's date into the window title and its previous/next-day navigation into the toolbar, and opens at a comfortable default size with a sensible minimum.
- **Battery "won't empty before off-peak" indicator** (T-1327). The Dashboard hero now shows "Won't empty before HH:MM" — alongside the live charge/discharge rate — when the battery cannot reach the cutoff before the next off-peak window opens, even at maximum discharge.

### Fixed

- **Accurate peak and off-peak grid import** (T-1341, T-1420). The off-peak split and the daily peak grid import are now computed by integrating the 10-second readings series rather than diffing AlphaESS's lagging energy snapshots, which routinely misclassified the tail of grid-charging as peak (2026-05-18 reference: the meter reported 0.17 / 20.75 kWh peak/off-peak while Flux reported 1.74 / 18.95). Peak grid import is now a single server-computed value shown consistently on the Dashboard, Day Detail, and History — including today, which updates live through the day — and historical days are corrected within the 30-day readings window.

### Documentation

- Specs for the above: Daily Costs (T-1326), iPad adaptive layout (T-1150), Better macOS Interface (T-1342), Off-Peak From Readings (T-1341), and the Peak From Readings smolspec. See `specs/`.

## [1.3] - 2026-05-21

### Added

- **SoC Alerts** (T-1288). Push-notification alerts when the battery's state of charge crosses a threshold inside a time window you define. Each device manages up to 10 rules — threshold percent (1–99), HH:MM start/end window (cross-midnight supported), enabled toggle, and an optional 40-char label. The Go poller evaluates every enabled rule each 10 s cycle and fires at most once per (device, rule, window-start day) on a downward crossing. Pushes are dispatched via `sideshow/apns2` through a bounded 4-worker queue so a slow APNs RTT can't stall the poll cadence. Per-device APNs environment is honoured at registration time, so Xcode-dev sandbox tokens and TestFlight/App Store production tokens coexist on the same backend without the wrong host silently 400-ing every push. Three new DynamoDB tables back the feature: `flux-devices` (PITR), `flux-soc-rules` (PITR), `flux-soc-fire-state` (TTL 7 d). Lambda gains `POST /devices`, `GET/POST /devices/{id}/rules`, and `PUT/DELETE /devices/{id}/rules/{ruleId}` behind the existing bearer-token middleware. Rule mutations cascade-clean the affected fire-state rows so the next cycle re-arms under the new configuration, and a daily orphan GC drops devices that haven't registered for 30 days. The iOS/macOS Settings → Alerts screen lists, creates, edits, toggles, and deletes rules with banners for the denied-permission and backend-error cases; device registration is idempotent and the foreground hook replays pending registrations.
- **`cmd/backfill-readings` CLI** (T-1274). One-off tool that, for a date range, removes the all-zero `flux-readings` rows the overnight AlphaESS outage produced and replaces them with synthetic 5-minute readings derived from `getOneDayPowerBySn` snapshots (same field mapping the Day Detail past-date fallback uses). Supports `--dry-run`; defaults to the trailing 3 days.

### Fixed

- **Dashboard no longer shows "0% / 0 W everywhere" overnight** (T-1274). When AlphaESS goes quiet at night (`getLastPowerData` returns `code:200` with `data: null` or an all-zero object), the poller previously unmarshalled the missing payload into a zero-valued `PowerData` and wrote a fresh `ReadingItem` with every field zero, which `/status` then surfaced as the live readout. Three reinforcing fixes: `GetLastPowerData` now treats null/empty `data` as an error; the poller's `fetchAndStoreLiveData` refuses to persist every-field-zero readings and logs the raw values at warn level; and `/status` drops `live` (along with the cutoff times derived from it) when the most recent stored reading is older than 90 s, so the dashboard's existing "Awaiting live data" state surfaces instead of holding aged numbers.

### Documentation

- **SoC Alerts spec** (T-1288). Implementation spec for the SoC alert feature above. See `specs/soc-alerts/`.

## [1.2] - 2026-05-13

### Added

- **Zoom in on Graphs** (T-1215). All five chart cards across History (Solar, Grid Usage, Daily Usage) and Day Detail (Power, Combined battery) gain an `arrow.up.left.and.arrow.down.right` expand button in the top-trailing corner. Tapping it opens the chart enlarged: on iOS as a landscape full-screen cover with a 32 pt drag-handle pill above the title (drag down past 60 pt to dismiss) plus a Close button; on macOS as a separate resizable window (900×600 default, 720×480 minimum) keyed by chart kind so re-expanding the same chart brings the existing window forward. The enlarged view reuses the inline card body so styling, selection overlays, and accessibility behave identically. Selection state is shared between inline and enlarged Day Detail charts so opening the enlarged view in the middle of a drag doesn't lose the selected reading. Mid-interaction data refreshes are deferred (drag end on History; 400 ms quiet window on Day Detail) so the chart can't shift under the user's finger. VoiceOver / keyboard focus returns to the originating expand button on dismissal, and enlarged charts honour Reduce Motion (200 ms cross-fade instead of the system slide). iOS sheet state survives backgrounding via `@SceneStorage`; macOS windows are not restored on launch.
- **Stat Comparisons on Day Detail** (T-1161). New Compare toggle above the "Day in five blocks" panel adds an opt-in sub-line beneath every Day Detail summary value and every five-block value column, showing the absolute-kWh delta against the comparison day with an ▲ / ▼ / — arrow. Period chip switches between Yesterday and 7 days ago. Comparison covers Solar produced, House used, Grid in (peak/off-peak), Grid out, plus per-block solar and total on the morning peak / off-peak / afternoon peak rows. Toggle and period are remembered per-device. VoiceOver reads the delta inline with the value. Comparison data uses the existing `/day` endpoint — no backend changes required.

### Fixed

- **Day Detail combined battery chart**: SOC area is now visually distinct from the yellow off-peak window rectangle. `Palette.soc` shifts from cream (`#FFD089`) to coral (`#FFA866`) so the two layers no longer blend where they overlap.
- **Day Detail charts no longer cut off ~23:05–23:10 for past dates** (T-1206). The poller only ever asked AlphaESS for today's daily-power snapshots, so after Sydney midnight the readings between the last pre-midnight hourly tick and 23:55 were permanently lost from `flux-daily-power`. The poller now polls today AND yesterday hourly (mirroring the daily-energy fix from T-841), and a dedicated midnight-finalizer goroutine re-fetches yesterday's daily power and energy and re-runs the derived-stats summarisation 15 min after each Sydney midnight. Existing past dates can be patched with the new `cmd/backfill-daily-power` CLI.

## [1.1] - 2026-05-09

### Added

- **Solar by Block on Day Detail** (T-1162). The "Day in five blocks" panel now shows per-block solar generation (kWh) next to the load value on the morning peak, off-peak, and afternoon peak rows. Solar is rendered in amber and null is omitted cleanly so days predating the feature deploy don't show false zeros. Backend computes it by trapezoidal integration of `Ppv` (mirroring the existing `Pload` rule), persists it on `flux-daily-energy` via the same atomic `UpdateDailyEnergyDerived` write path, and ships a `cmd/backfill-solar` CLI to populate historical rows from still-resident readings.
- **Dashboard "Energy left" row** (T-1163). Dashboard `BatteryBlock` shows usable kWh remaining at the current SOC — `(soc − cutoffPercent) / 100 × capacityKwh`, clamped at 0 — hitting 0 at the same point as the existing "empty by HH:MM" subline. Shared math lives in `FluxCore.BatteryEnergy` so the Day Detail SOC chart's dashed cutoff line tracks the same value.
- **What's New sheet** (T-1112). New `WhatsNew` module in `FluxCore` auto-presents a sheet of New / Improved / Fixed highlights once per cold launch on a version bump, with a "What's New" row in Settings to re-open the latest entry. Catalogue is typed; `WhatsNewVersion` parses semver-ish strings so `1.10 > 1.9`. First-launch users see nothing.

### Changed

- **Battery cutoff threshold lowered from 10% to 5%** to match a hardware-side change to the AlphaESS minimum discharge setting. The Lambda API's `cutoffPercent` constant and `battery.cutoffPercent` response field now report 5; the dashed cutoff line on the SOC and combined battery charts in Day Detail shifts down accordingly.
- **Day Detail "Day in five blocks" row layout** now stacks the time range under the block name and uses a fixed-width trailing column so four-digit values like `11.12 kWh` align across rows without truncating.

## [1.0] - 2026-05-07

First public release. Flux is a personal AlphaESS battery monitoring system with three deployable surfaces — an AWS backend (Go ECS Fargate poller + Lambda API), a SwiftUI iOS 26+ app, and a native macOS 26+ app — plus a widget extension shared between iOS and macOS.

### Added

#### Backend

- **AWS infrastructure**: single CloudFormation stack provisioning a VPC with public subnets, ECS Fargate cluster running the ARM64 poller container, Lambda Function URL serving the API (ARM64, `provided.al2023`), six DynamoDB tables (`flux-readings`, `flux-daily-energy`, `flux-daily-power`, `flux-system`, `flux-offpeak`, `flux-notes`) on PAY_PER_REQUEST, SSM Parameter Store for credentials and configuration, and CloudWatch Logs with 14-day retention. All IAM least-privilege and ARN-scoped.
- **AlphaESS poller** (`cmd/poller`): polls AlphaESS on four schedules (10s live data, 1h daily power, 6h daily energy, 24h system info), stores results in DynamoDB, snapshots off-peak energy at window boundaries with retry + crash recovery, and runs an hourly summarisation pass that pre-computes per-day derived stats (`dailyUsage`, `socLow`, `peakPeriods`) for yesterday into `flux-daily-energy`. Two-context graceful shutdown, `SummarisationPassResult` CloudWatch metric with six result dimensions, and a `/poller healthcheck` subcommand for the ECS health probe.
- **Lambda API** (`cmd/api`): three GET endpoints — `/status`, `/history?days=N`, `/day?date=YYYY-MM-DD` — plus a `PUT /note` write endpoint for free-text day notes. Bearer token auth via constant-time comparison; SSM-backed credentials. Response shape includes battery info with cutoff prediction, rolling 15-minute averages, sustained grid detection, off-peak deltas, today's energy via trapezoidal integration of readings reconciled against stored daily totals, peak-period clusters, the five-block daily-usage breakdown, and an off-peak grid import/export split on `/day`.
- **Day notes**: per-date free-text notes (200 grapheme clusters, NFC-normalised, leading/trailing whitespace trimmed). Empty text deletes idempotently. Notes are bundled into the read responses via a soft-fail goroutine so a notes-table failure never cancels the core path. Identical grapheme rules on Swift and Go enforced by a shared `internal/api/testdata/note_lengths.json` fixture.

#### iOS app

- **Dashboard**, **History**, **Day Detail**, and **Settings** screens for iOS 26+, built in SwiftUI with `@Observable` view models on the main actor. Auto-refresh every 10 s on Dashboard with `scenePhase` pausing, pull-to-refresh, SwiftData caching of historical days, Keychain (App Group) for the API token.
- **V5 redesign**: custom three-segment tab bar above each screen (replacing `NavigationSplitView` on iOS), redesigned Liquid Glass panels, dashboard hero with selectable font and "discharging · X · empty by HH:MM" subline, live solar/house/grid trio panel, and a "Day in five blocks" stacked-bar panel pinned to the top of Day Detail.
- **Peak Usage card** showing the top three high-load periods of the day with time range, average load, and energy.
- **Daily Usage card** breaking each day into up to five chronological no-overlap blocks (Night / Morning Peak / Off-Peak / Afternoon Peak / Evening) with kWh, average kWh/h, percent of day, status, and an `≈ sunrise` / `≈ sunset` caption when boundaries are estimated rather than read from a real sun-up reading.
- **History Daily Usage card**: a multi-day stacked-bar variant of the five-block breakdown across the active 7/14/30-day range, with a chronologically-pinned palette and accessibility tree exposing one element per day.
- **History Usage Stats overview card** (T-896): eight tiles for the active range — Total usage, Total solar, Exported, Peak imports, Avg night, Most usage, Most solar, Lowest SoC — with day-record tiles tappable to drill into Day Detail.
- **BatteryBlock panel**: battery cycle, lowest SOC with timestamp, and an optional "Charged during off-peak" delta. Used on Dashboard and Day Detail.
- **Light / Dark / Follow System** theme picker in Settings, applied at the app root via `.preferredColorScheme(...)` and persisted in the App Group.
- **App-wide font picker** in Settings listing every font family installed on the device. The chosen family applies to every text element (Dashboard, Day Detail, History, Settings, navigation chrome) while preserving Dynamic Type via `Font.custom(_:size:relativeTo:)`. Tabular tokens (digits, time ranges) deliberately stay on the system monospaced font so columns stay aligned.
- **Day Notes** UI: a read-only `NoteRowView` on Dashboard, History, and Day Detail; tap on Day Detail to edit via a `NoteEditorSheet` with NFC normalisation, 200-grapheme cap, and a "remaining characters" counter. "Add note" affordance gated to today-or-earlier dates.
- **Household load row** (total usage) in the Dashboard, History day-summary, and Day Detail summary cards. Derived from the energy balance (`solar + grid_import + battery_discharge − grid_export − battery_charge`) with em-dash fallback when any input is missing.

#### macOS app

- **Native macOS 26+ build** (T-1081). Not Mac Catalyst, not "Designed for iPad". Shares `FluxCore`, views, and the widget extension with iOS. Dedicated `Settings { ... }` scene opened via ⌘,, single main window that quits on close, ⌘R refresh dispatched through a `FluxRefreshCoordinator`, ←/→ Day Detail navigation, `@SceneStorage`-persisted sidebar selection, Liquid Glass styling via `.scrollContentBackground(.hidden)` and `.backgroundExtensionEffect()`.
- **Activity tier**: 10 s refresh when the main window is key, 60 s when not, bound to `@Environment(\.appearsActive)`.
- **Credential sync**: API token via iCloud Keychain (`kSecAttrSynchronizable = true`, `kSecAttrAccessibleAfterFirstUnlock`), URL via `NSUbiquitousKeyValueStore` mirrored into the App Group `UserDefaults` so the widget reads the same value the host writes.
- **Control Center widget** for macOS using Apple's `ControlWidget` protocol with an `OpenURLIntent("flux://dashboard")` tap target.
- `make macos-build`, `make macos-test`, `make macos-lint` Makefile targets.

#### Widgets

- **Home-screen widgets** (`systemSmall`, `systemMedium`, `systemLarge`) and **lock-screen accessories** (`accessoryCircular`, `accessoryRectangular`, `accessoryInline`) showing battery state of charge, plus the solar / house / grid power trio where the family size allows.
- **Medium widget redesign**: SOC reading sits inside a circular progress ring; right column renders four matched pill rows (Solar / Load / Grid / battery state + rate). Cutoff-risk colouring tints the ring and battery row together via `CutoffTimeColor` (red < 2 h, orange before off-peak) with `BatteryColor.forSOC` as fallback.
- **Settings toggle "Widget icons instead of labels"** switches medium-widget row labels between text and SF Symbols (`sun.max.fill`, `house.fill`, directional grid arrows, battery-state glyph, `clock`); symbols inherit each row's semantic colour.
- **Empty-time row** on the medium widget showing the 15-minute rolling-average cutoff time when discharging, tinted by `CutoffTimeColor`.
- Widgets refresh independently from the host via the shared App Group cache (`group.me.nore.ig.flux`) plus a direct Lambda fetch with the shared Keychain bearer token. `flux://dashboard` deep link opens the app at the Dashboard.

### Changed

- **`battery.low24h`** is the lowest SOC since Sydney-local 00:00 on `now`'s date — "lowest today" semantics. Nil only briefly each day after midnight before the first reading lands. UI labels read "Lowest" everywhere (Dashboard, large widget, Day Detail).
- **`/day` endpoint** returns `offpeakGridImportKwh` and `offpeakGridExportKwh` so summary cards can split peak vs off-peak grid import without re-deriving from readings.
- **`/history` endpoint** serves pre-computed `dailyUsage`, `socLow`, `socLowTime`, and `peakPeriods` per day from storage (via the new poller summarisation pass), with live computation only for today. Avoids re-fetching ~258k readings per 30-day call.
- **iOS app architecture**: shared models, networking, Keychain, and formatting helpers live in a local `FluxCore` Swift Package consumed by the main app, the widget extension, and the macOS host. No third-party charting; no third-party dependencies.

### Fixed

- **Evening/Night stray-blip filter**: `findEveningNight` ignores `Ppv > 0` readings before `sunrise − 30 min` for both the night-end and evening-start slots, so a sensor blip at e.g. 01:30 no longer ends the Night block at 01:30 or starts a 22-hour "evening".
- **`/status` cutoff suppression inside off-peak**: `estimatedCutoffTime` is omitted when the extrapolated cutoff falls at or after the next off-peak window (battery will be charged during that window, so the prediction is misleading).
- **Dashboard "Charged during off-peak" row** is reserved with an em-dash even when the delta is nil (e.g. before today's window has produced data) instead of vanishing.
- **`computeCutoffTime`** guards against NaN/Inf from very small `pbat` values and rejects `capacityKwh <= 0`.

### Removed

- **30-day TTL on `flux-daily-power`**: the `TimeToLiveSpecification` is gone from the CloudFormation template and the poller no longer writes the `ttl` attribute. Day Detail's past-date fallback now works indefinitely instead of stopping at 30 days. Replaces an abandoned S3-archive plan — at ~21 MB/year the rows stay in DynamoDB cheaply.
- **Unused CloudKit/iCloud entitlements** from the iOS app, fixing App Store Connect upload error 90046.
- **"Full" status** removed from the dashboard hero, medium-widget battery row, and VoiceOver verbs at 100 % SOC; the views now reflect the raw `pbat` reading (charging / discharging / idle).

### Breaking Changes

- `OffPeakWindowStart` and `OffPeakWindowEnd` CloudFormation parameters no longer have defaults — supply them explicitly via the parameters file or `--parameter-overrides` on every deploy. (`AllowedPattern` validates the values at deploy time so YAML 1.1 sexagesimal parsing can no longer reinterpret `11:00` as `660`.)
