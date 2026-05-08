# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Changed

- **Battery cutoff threshold lowered from 10% to 5%** to match a hardware-side change to the AlphaESS minimum discharge setting. The Lambda API's `cutoffPercent` constant and `battery.cutoffPercent` response field now report 5; the dashed cutoff line on the SOC and combined battery charts in Day Detail shifts down accordingly. The V1 product spec is updated to match.

### Added

- **Dashboard "Energy left" row** (T-1163). The Dashboard's `BatteryBlock` now shows the usable kWh remaining at the current SOC: `(soc − cutoffPercent) / 100 × capacityKwh`, clamped at 0. The figure hits 0 at the same point as the existing "empty by HH:MM" hero subline. Day Detail / History callsites omit the new input so the row stays hidden where there is no live SOC. Shared math + cutoff constant live in `FluxCore.BatteryEnergy`, also referenced by the Day Detail SOC and combined battery charts so the dashed cutoff line tracks the same value.
- **What's New sheet** (T-1112). New `WhatsNew` module in `FluxCore` with a typed catalogue, a `Comparable` `WhatsNewVersion` parser (`1.10 > 1.9`, `1.2 == 1.2.0`), a pure `WhatsNewCoordinator` mapping `(catalogue, installed, lastSeen, hasAnyFluxPref)` to `present | silentSet | skip`, and a SwiftUI `WhatsNewSheet` grouping highlights into New / Improved / Fixed. `AppNavigationView` auto-presents the sheet once per cold launch on a version bump; `SettingsView` exposes a "What's New" row that re-opens the latest entry. `lastSeenWhatsNewVersion` and `hasAnyFluxPreferenceWritten` added to `UserDefaults+Settings`. Coordinator/sheet tests cover every row of the design's decision table; sheet tests smoke three fixtures via `UIHostingController` / `NSHostingController`. `specs/whats-new/implementation.md` documents the design at three levels.

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
