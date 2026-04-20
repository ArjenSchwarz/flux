# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added

- Spec for T-843 "Add widgets to the app" under `specs/add-widgets/`: EARS requirements, design (new `FluxCore` local Swift Package + widget extension target + App Group cache + Keychain accessibility migration + flux:// deep link), 20-entry decision log, 43-task implementation plan across 5 phases / 3 streams, and a prerequisites doc covering the one-sitting Xcode setup
- `FluxCore` local Swift Package skeleton at `Flux/Packages/FluxCore/` (task 1): `Package.swift` (swift-tools 6.2, iOS 26), empty `Sources/FluxCore/{Models,Networking,Security,Helpers,Widget}` and `Tests/FluxCoreTests/` directories with placeholder files so the package compiles
- `FluxWidgetsExtension` widget-extension target in Xcode with App Groups + Keychain Sharing capabilities (shares `group.me.nore.ig.flux` with the main app), plus Xcode-generated template files as placeholders to be replaced in Phase 4
- `FluxCore` Phase 1 migration (T-843 tasks 2–7): moved shared models (`StatusResponse`, `LiveData`, `BatteryInfo`, `Low24h`, `RollingAvg`, `OffpeakData`, `TodayEnergy`, `HistoryResponse`, `DayEnergy`, `DayDetailResponse`, `TimeSeriesPoint`, `DaySummary`, `PeakPeriod`, `APIErrorResponse`), `FluxAPIError`, `FluxAPIClient`, `URLSessionAPIClient`, `KeychainService`, `DateFormatting`, `PowerFormatting`, `BatteryColor`, `GridColor`, `CutoffTimeColor`, and `ColorTier` from the main app target into the `FluxCore` package with `public` access, deleted the originals, and added `import FluxCore` to every consuming main-app file
- `ColorTier+Color.swift` SwiftUI extension in the main app target so views keep using `tier.color` while the package itself stays SwiftUI-free
- `FluxCore` Phase 2 widget-support types (T-843 tasks 8–19, TDD): `StatusSnapshotEnvelope` (versioned Codable envelope of `StatusResponse` + `fetchedAt`), `WidgetSnapshotCache` (App Group `UserDefaults` persistence with strict `>` `writeIfNewer` semantics and schema-version validation), `StalenessClassifier` (pure fresh/stale/offline bucketing at 45 min / 3 h boundaries with `nextTransition`), `WidgetDeepLink` (`flux://dashboard` parse + canonical URL), `SettingsSuiteMigrator` (idempotent one-shot copy of `apiURL`/`loadAlertThreshold` from `UserDefaults.standard` into the App Group suite, gated on a `settingsMigrationVersion` flag), and `KeychainAccessibility` enum plus `KeychainService.readAccessibility()` / `updateAccessibility(_:)` (atomic `SecItemUpdate`, no delete-then-add; `KeychainService.init` gains an `accessibility` parameter defaulting to `.afterFirstUnlockThisDeviceOnly`) — each backed by Swift Testing unit tests in `FluxCoreTests`
- Phase 3 main-app integration (T-843 tasks 20–29): `KeychainAccessibilityMigrator` in `Flux/WidgetBridge/` — crash-safe in-place accessibility migration via `SecItemUpdate` with a read+save fallback if update fails; `FluxApp.init` now runs `SettingsSuiteMigrator.run()` then `KeychainAccessibilityMigrator.run()` on every launch; `flux://` URL scheme registered in `Flux/Info.plist`; `DeepLinkHandler` + `AppNavigationView.onOpenURL` route `flux://dashboard` to the Dashboard screen (unknown hosts / other schemes are no-ops); `DashboardViewModel` writes a `StatusSnapshotEnvelope` to the App Group cache after every successful `/status` refresh and calls `WidgetCenter.shared.reloadTimelines(ofKind:)` for `me.nore.ig.flux.widget.battery` and `…accessory` — gated on `writeIfNewer` returning true and debounced to at most one reload per 5 minutes; new Swift Testing coverage in `FluxTests` for the migrator, deep-link handler, App Group `UserDefaults`, and the cache-write + debounced-reload paths

### Changed

- `UserDefaults+Settings` exposes a new static `UserDefaults.fluxAppGroup` accessor backed by `group.me.nore.ig.flux`; `apiURL` and `loadAlertThreshold` read/write that suite by default and transparently fall back to `UserDefaults.standard` when the suite is empty (covers the transient state before `SettingsSuiteMigrator` has run on upgrade). Call sites in `SettingsViewModel`, `PowerTrioView`, and `AppNavigationView.makeAPIClient` switched from `UserDefaults.standard` to `UserDefaults.fluxAppGroup`

### Changed

- `Flux/Packages/FluxCore/Package.swift` pins both `FluxCore` and `FluxCoreTests` targets to Swift language mode 5 to match the main app's `SWIFT_DEFAULT_ACTOR_ISOLATION = MainActor` behaviour and let the migrated formatter singletons compile
- Migrated `APIModelsTests`, `DateFormattingTests`, `ColoringTests`, `KeychainServiceTests`, `URLSessionAPIClientTests` into `FluxCoreTests` (with `import FluxCore` instead of `@testable import Flux`); `ColoringTests` carries a private SwiftUI `ColorTier.color` extension so the existing assertions still cover the colour mapping
- Remaining `FluxTests` view-model test files pick up `import FluxCore` for the now-migrated types
- `.gitignore` ignores `.build-xc/` so xcodebuild derived-data spillover stays out of commits
- Decision log: added Decision 19 (consolidate widget-testable logic into `FluxCore`, no separate `FluxWidgetsTests` target — Xcode doesn't allow app-extension-hosted test bundles) and Decision 20 (accept Xcode's auto-appended `Extension` suffix on the target name)
- `specs/add-widgets/design.md` and `tasks.md` updated to reflect the consolidation: `RelevanceScoring`, `WidgetAccessibility`, and `StatusTimelineLogic` now live in `FluxCore`; widget extension is a thin WidgetKit shim
- `prerequisites.md` rewritten as a record of the completed Xcode setup with the actual paths Xcode used (entitlements at `Flux/FluxWidgetsExtension.entitlements`, not `Flux/FluxWidgets/FluxWidgets.entitlements`)
- `.gitignore` excludes `.build/` and `Packages/**/.swiftpm/` for the local Swift Package

### Changed

- `PeakUsageCard` displays period energy as kWh (one decimal) instead of Wh, and adds a caption header row ("Timespan" / "Average · Total") above the entries

### Fixed

- `/status` no longer returns an `estimatedCutoffTime` (in either `battery` or `rolling15min`) when the extrapolated cutoff falls at or after the next off-peak window — the battery will be charged during that window so the predicted cutoff is misleading. Implemented via a new `nextOffpeakStart` helper that returns today's window start when `now < todayEnd`, otherwise tomorrow's window start (T-827).
- Cleared 43 pre-existing SwiftLint `--strict` violations across iOS app, test, and UI test targets. Production fixes include renaming `kw` → `kilowatts` in `PowerFormatting`, extracting locals in `BatteryPowerChartView` to shorten lines, wrapping argument lists in `SettingsView`/`HistoryChartView`, and a single scoped `force_try` suppression in the `HistoryView` Preview. Test fixes are either structural (wrapping long initializers, replacing a 4-tuple with a named struct in `ColoringTests`, dropping a `try!`) or rule-scoped suppressions on XCTest/URLProtocol class overrides and on cohesive test suites where splitting would reduce readability.

### Changed

- `findPeakPeriods` precomputes an off-peak mask instead of calling `isOffpeak` twice per reading, halving timezone conversions on the `/day` hot path
- `findPeakPeriods` uses `slices.SortFunc` with `cmp.Compare` in place of `sort.Slice` (per Go modernisation rules) and adds capacity hints to clusters/filtered/results slices
- `findPeakPeriods` always returns a non-nil `[]PeakPeriod{}`; `handleDay` no longer re-assigns a defensive nil-to-empty local variable
- Additional `findPeakPeriods` unit tests for single-reading input and the AEDT→AEST DST transition day (2026-04-05)

### Added

- `PeakUsageCard` SwiftUI view on day detail screen showing top peak usage periods with time range (24h format), average load (kW), and energy (Wh with grouping separator), styled to match existing summary card
- Peak periods wired into `/day` endpoint — `handleDay` calls `findPeakPeriods` on raw readings and returns results in `peakPeriods` field (always `[]`, never null)
- Integration tests for `/day` endpoint verifying `peakPeriods` presence across normal, fallback, no-data, and known-peak scenarios
- `peakPeriods` property on iOS `DayDetailViewModel` with nil-coalescing from response and error-path reset
- ViewModel unit tests for peak periods population, nil handling, error clearing, and backwards-compatible JSON decoding
- `DateFormatting.clockTime24h(from:)` — locale-independent 24-hour time formatter (HH:mm) for Sydney timezone
- `findPeakPeriods` algorithm in `compute.go` — 5-step threshold-based clustering that identifies top 3 peak household load periods from raw readings, excluding off-peak windows, with trapezoidal energy integration and configurable merge/duration/gap thresholds
- Unit tests for `findPeakPeriods` covering 16 scenarios: empty input, off-peak exclusion, uniform load, single/multiple peaks, cluster merging within 5min, separate clusters >5min, short period filtering (<2min), top-3 ranking, 60s gap handling, off-peak boundary behaviour, transitive merging, zero-energy sparse periods, invalid off-peak parsing, negative Pload clamping, and unrounded energy ranking
- Property-based tests for `findPeakPeriods` using `pgregory.net/rapid` verifying 6 invariants: result count ≤3, periods outside off-peak, non-overlapping, positive energy, descending energy order, and duration ≥2 minutes
- Benchmark for `findPeakPeriods` with 8640 readings (full day at 10s intervals)
- `pgregory.net/rapid` dependency for property-based testing
- Named constants for peak period computation: merge gap (5min), minimum duration (2min), max pair gap (60s), max periods (3)
- `PeakPeriod` model in iOS app (Codable, Sendable, Identifiable) with optional `peakPeriods` on `DayDetailResponse` for backwards compatibility
- Sample peak period data in iOS mock API client for previews
- Feature spec for Peak Usage Periods: requirements (19 acceptance criteria in EARS format), design (5-step threshold-based clustering algorithm for backend, PeakUsageCard for iOS), decision log (11 decisions), and task list (13 tasks across 2 parallel streams)
- Implementation explanation for Peak Usage Periods at three expertise levels with completeness assessment

### Changed

- Battery hero cutoff estimate now uses the 15-minute rolling average instead of instantaneous power, reducing fluctuation in the displayed cutoff time
- Day detail page chart order: power flow and battery load charts now appear above the battery percentage (SOC) chart

### Removed

- Unused CloudKit/iCloud entitlements (`icloud-container-identifiers`, `icloud-services`) from iOS app — fixes App Store Connect upload error (code 90046) for empty `icloud-container-environment` value

### Added

- `computeTodayEnergy()` function in Lambda API compute layer — integrates power readings from midnight using trapezoidal integration with clamped directional values, gap detection (>60s skip), and Wh-to-kWh conversion
- `reconcileEnergy()` function in Lambda API compute layer — per-field max of computed (from readings integration) and stored (from DynamoDB daily energy) values
- Unit tests for `computeTodayEnergy()` (7 cases: empty, single, normal, midnight boundary, gap skip, mixed sign, rounding) and `reconcileEnergy()` (5 cases: both nil, one nil, per-field max, mixed)
- Integration tests for computed energy in `handleStatus`: computed-only (no DailyEnergyItem), reconciled (per-field max of computed vs stored), and fallback to DailyEnergyItem when fewer than 2 readings
- iOS build, test, lint, device install, and App Store distribution targets to Makefile (`ios-build`, `ios-test`, `ios-install`, `ios-run`, `ios-archive`, `ios-upload`, etc.) with xcbeautify pipe, device auto-detection, and `make help` overview
- Shell safety flags (`pipefail`, `errexit`) to Makefile for reliable error propagation
- Battery charge/discharge rate display in `BatteryHeroView` status line (e.g. "Charging at 3.42 kW")
- Shared `PowerFormatting.format()` helper for consistent watt/kW display across dashboard views (W below 1000, kW with 2 decimal places above)
- `PowerFormatting.formatAxis()` for chart y-axis labels in kW
- "Today detail" button on dashboard linking to today's day detail page
- Tap/drag selection on all three day detail charts (Battery %, Power Flows, Battery Load) showing values at the selected point with a dashed vertical line and colored dots
- `nearestReading(to:)` helper on `[ParsedReading]` for chart selection lookup
- Off-peak window highlight (11:00-14:00) as yellow background band on all day detail charts
- `DayChartDomain.offpeakRange(for:)` helper for computing off-peak time range

### Changed

- `dailyEnergyInterval` reduced from 6 hours to 1 hour in poller for more frequent AlphaESS energy reconciliation
- `handleStatus` now computes today's energy from power readings via `computeTodayEnergy()` and reconciles with `DailyEnergyItem` via `reconcileEnergy()`, replacing the DailyEnergyItem-only approach
- iOS deployment target lowered from 26.4 to 26.0 for device compatibility
- `URLSessionAPIClient` now uses a no-cache `URLSession` instead of `.shared`, preventing stale HTTP responses for real-time polling data
- HTTP 403 responses now map to `FluxAPIError.unauthorized` instead of `unexpectedStatus`, matching bearer token auth semantics
- `PowerTrioView` grid column now shows direction in header ("Grid (import)" / "Grid (export)") instead of a separate detail row, reducing card height
- `PowerTrioView` values now use `PowerFormatting` (kW with 2 decimals for 1000+) instead of raw integer watts
- `TodayEnergyView` condensed from 5 rows to 3: solar, "Grid (import/export)", and "Battery (+/-)" with paired values
- History screen now defaults to today instead of the oldest day
- Day detail summary card uses paired rows matching dashboard layout: "Grid (import/export)" and "Battery (+/-)"
- "SOC low" renamed to "24h low" in day detail summary
- "Battery SOC" chart renamed to "Battery %"
- "Battery Power" chart renamed to "Battery Load"
- Power Flows and Battery Load chart y-axis labels now display in kW instead of raw watts
- Power Flows chart uses named legend series (Solar, Load, Grid) instead of unnamed colors
- Dashboard navigation changed from single "View history" link to side-by-side "Today detail" and "History" buttons
- Dashboard now shows empty placeholder layout on initial load instead of error card, with data filling in once fetched
- `DashboardViewModel.startAutoRefresh()` no longer cancels in-flight requests on repeated calls from view lifecycle
- `DashboardViewModel.refresh()` ignores cancellation errors from view lifecycle instead of storing them as error state
- Removed "Secondary Stats" heading from secondary stats card
- 15m avg load now uses `PowerFormatting` for kW display

### Added

- `APIModelsTests.swift` with 14 JSON decoding tests covering full status response, null/missing optional fields, partial summaries, empty history, error response, and `Identifiable` conformance for all API models
- `ParsedReading` struct in `DayDetailViewModel` that pre-parses timestamps once after fetch, replacing per-chart-view parsing that ran 3x per render cycle (up to 864 `DateFormatter` calls per Day Detail render)
- `OffpeakData.defaultWindowStart` / `.defaultWindowEnd` static constants for off-peak window fallback values, replacing duplicated `"11:00"` / `"14:00"` string literals in `PowerTrioView` and `SecondaryStatsView`

### Changed

- Day Detail chart views (`SOCChartView`, `PowerChartView`, `BatteryPowerChartView`) now accept `[ParsedReading]` instead of `[TimeSeriesPoint]`, eliminating per-view timestamp parsing
- `HistoryViewModel.cacheHistoricalDays` now scopes its SwiftData fetch with a `#Predicate` filtering to only the incoming dates, instead of loading the entire cache table
- `HistoryViewModel.loadCachedDays` now uses `fetchLimit` on the `FetchDescriptor` instead of fetching all records and slicing in memory
- `specs/ios-app/implementation.md` rewritten with three-level explanation (beginner/intermediate/expert) and completeness assessment

### Removed

- Dead Xcode template files `ContentView.swift` and `Item.swift` that were unreferenced after the app was implemented

### Added

- Shared `MockFluxAPIClient` preview service in `Flux/Flux/Services/MockFluxAPIClient.swift` with static `/status`, `/history`, and `/day` sample payloads, then wired SwiftUI previews across dashboard/history/day-detail views (including SOC/power/battery chart views) to use centralized mock data instead of per-file preview actors
- iOS settings and root navigation implementation: `SettingsView` form with backend/display sections and validation-driven dismiss flow, plus `Navigation/AppNavigationView`, `SidebarView`, and `Screen` to power `NavigationSplitView` routing with automatic redirect to Settings when API configuration is missing
- iOS History and Day Detail UI implementation in `Flux/Flux/History/` and `Flux/Flux/DayDetail/`, including grouped 5-metric history bars with day selection, 7/14/30 range picker, day summary card with drill-down navigation, SOC/power/battery charts, day-to-day navigation, and fallback SOC-only handling when power data is unavailable
- Shared day-axis domain helper (`DayChartDomain`) and new chart views (`HistoryChartView`, `SOCChartView`, `PowerChartView`, `BatteryPowerChartView`) with Sydney-date alignment and 3-hour x-axis tick marks for consistent 00:00–00:00 rendering
- History/day detail flow wiring from the dashboard “View history” link to the real `HistoryView` screen instead of placeholder content

- iOS dashboard UI building blocks in `Flux/Flux/Dashboard/`: `BatteryHeroView`, `PowerTrioView`, `SecondaryStatsView`, `TodayEnergyView`, and `DashboardView` with pull-to-refresh, 10-second auto-refresh lifecycle hooks, scene phase handling, stale-data banner, and placeholder navigation/actions for History and Settings
- iOS view models for Dashboard, History, Day Detail, and Settings in `Flux/Flux/` with `@MainActor @Observable` state, async loading/refresh flows, Sydney-time `isToday` handling, fallback day-power detection, and settings validation via `URLSessionAPIClient(baseURL:token:)`
- iOS settings persistence support via `UserDefaults` extensions for `apiURL` and `loadAlertThreshold` (3000W default)
- iOS unit tests in `Flux/FluxTests/` for `DashboardViewModel`, `HistoryViewModel`, `DayDetailViewModel`, and `SettingsViewModel`, including refresh concurrency guards, auto-refresh lifecycle, cache fallback behavior, fallback power-data detection, and settings save/load validation
- iOS helper utilities in `Flux/Flux/Helpers/`: `DateFormatting` (Sydney timezone-safe parsing/formatting, off-peak window checks), `BatteryColor`, `GridColor`, and `CutoffTimeColor` for shared dashboard color logic
- iOS unit tests in `Flux/FluxTests/`: `DateFormattingTests` for timezone and off-peak boundary behavior, plus `ColoringTests` covering SOC thresholds, grid import/export rules, and cutoff color states
- iOS service foundation in `Flux/Flux/Services/`: `FluxAPIClient` protocol, `KeychainService` with App Group-aware Security framework storage, and `URLSessionAPIClient` with bearer-token request building plus typed `FluxAPIError` mapping for HTTP, network, and decoding failures
- iOS unit tests for service layer in `Flux/FluxTests/`: `KeychainServiceTests` for token persistence lifecycle and `URLSessionAPIClientTests` using a `URLProtocol` mock to verify endpoint URLs, auth headers, validation-token initializer behavior, and typed error handling
- iOS foundation model layer in `Flux/Flux/Models/`: backend-aligned `Codable & Sendable` response structs (`/status`, `/history`, `/day`), typed `FluxAPIError`, and SwiftData `CachedDayEnergy` cache model with unique `date` key plus conversion helpers
- iOS app spec: requirements document with 13 sections and 57 acceptance criteria covering platform/architecture, API client, authentication/settings, dashboard (battery hero, power readings, secondary stats, today's energy), refresh behaviour, history screen, day detail screen, caching, error states, and navigation
- iOS app spec: design document with MVVM architecture using `@MainActor @Observable` view models, NavigationSplitView with adaptive layout, FluxAPIClient protocol with URLSessionAPIClient (token provider pattern for settings validation), SwiftData caching for history, Keychain with App Group, SwiftUI Charts (BarMark/LineMark/AreaMark/RuleMark), DateFormatting utility with Sydney timezone, conditional colouring helpers, and file layout mapped to Xcode project structure at `Flux/Flux/`
- iOS app spec: decision log with 9 decisions (adaptive layout from start, no third-party dependencies, SwiftData for history caching only, Keychain with App Group, 10-second auto-refresh, spec scope excludes Xcode project, Sydney timezone for all date operations, token provider pattern for settings validation, fallback data detection via heuristic)
- iOS app spec: task list with 31 implementation tasks across 7 phases and 2 parallel streams, TDD-ordered with dependency tracking and requirement traceability
- iOS app spec: prerequisites document listing Xcode project setup steps (App Group capability still needed)
- Xcode project for iOS app (`Flux/`) with iOS 26 deployment target, SwiftUI + SwiftData template, entitlements for CloudKit and push notifications, and unit/UI test targets
- Xcode-specific entries to `.gitignore` (`xcuserdata/`, `*.xcuserstate`)
- App Group entitlement (`group.me.nore.ig.flux`) for Keychain sharing with future widget extension
- App category set to Utilities in Xcode project

- Lambda entry point (`cmd/api/main.go`) with cold-start initialisation: AWS SDK config loading, SSM parameter fetching (api-token, serial) with decryption, environment variable validation, DynamoReader and Handler creation, and `lambda.Start` invocation
- `time/tzdata` import in Lambda entry point for timezone embedding on `provided.al2023` runtime
- JSON structured logging via `slog.NewJSONHandler` for CloudWatch compatibility
- `build-api` Makefile target for cross-compiling Lambda binary (`CGO_ENABLED=0 GOOS=linux GOARCH=arm64`)
- `aws-sdk-go-v2/service/ssm` dependency for SSM Parameter Store access
- `/status` endpoint handler (`internal/api/status.go`) with concurrent DynamoDB queries via errgroup (readings 24h, system, offpeak, daily energy), in-memory computation for live data, battery info with fallback capacity (13.34 kWh), rolling 15-minute averages, sustained grid detection, cutoff estimates, off-peak deltas, and today's energy totals
- `/history` endpoint handler (`internal/api/history.go`) with days parameter validation (7/14/30, default 7), date range computation in configured timezone, and energy value rounding
- `/day` endpoint handler (`internal/api/day.go`) with date validation, flux-readings query with fallback to flux-daily-power (cbat→soc, power fields→0), 5-minute bucket downsampling, socLow computed from raw data before downsampling, and conditional summary assembly
- `nowFunc` field on Handler for testable time capture — defaults to `time.Now`, overridable in tests
- Unit tests for `/status` endpoint covering all data present, no readings, offpeak pending/complete, no today energy, system missing/zero cobat fallback, DynamoDB errors in each Phase 1 operation, and single now capture verification
- Unit tests for `/history` endpoint covering default/explicit days, invalid days parameter, no data, ascending order, energy rounding, and DynamoDB errors
- Unit tests for `/day` endpoint covering normal case, fallback to daily power, no data from either source, readings without daily energy, date validation, socLow from raw not downsampled, and DynamoDB errors
- `golang.org/x/sync` dependency for errgroup
- Lambda API handler (`internal/api/handler.go`) with GET-only method check, bearer token auth using constant-time comparison, path routing (`/status`, `/history`, `/day`, 404), structured request logging (method, path, status, duration), and JSON error response helpers
- Handler tests (`internal/api/handler_test.go`) covering method validation, auth (valid/missing/wrong/malformed tokens), auth-before-routing ordering, path routing, and error response format verification
- `aws-lambda-go` dependency for Lambda Function URL request/response types
- Lambda API response structs (`internal/api/response.go`) with JSON tags matching V1 plan contract: `StatusResponse`, `HistoryResponse`, `DayDetailResponse` and nested types, using pointer types for nullable fields
- Lambda API compute functions (`internal/api/compute.go`): cutoff time estimation via linear extrapolation, rolling averages, sustained grid detection (pgrid > 500 for 3+ consecutive readings within 30s gaps), 5-minute bucket downsampling (288 buckets/day), min SOC finder, and energy/power rounding helpers
- Unit tests for all compute functions covering happy paths and edge cases (empty input, boundary conditions, nil guards)
- DynamoDB read layer (`internal/dynamo/reader.go`) with `Reader` interface (6 methods: `QueryReadings`, `GetSystem`, `GetOffpeak`, `GetDailyEnergy`, `QueryDailyEnergy`, `QueryDailyPower`) and `DynamoReader` implementation for the Lambda API
- `ReadAPI` client interface (Query, GetItem) separate from the existing write-focused `DynamoAPI` to maintain interface segregation between poller and API concerns
- Generic `getItem[T]` and `queryAll[T]` helpers for DynamoDB GetItem/Query operations with pagination, shared between `DynamoStore` and `DynamoReader`
- Unit tests for all `DynamoReader` methods covering success, not-found/empty, pagination, and error wrapping
- Lambda API spec: requirements document with 12 requirement groups and 74 acceptance criteria covering implementation constraints, authentication, status/history/day endpoints, response format, runtime configuration, and observability
- Lambda API spec: design document with architecture diagram, component interfaces (Handler, Reader, DynamoReader), response types, pure compute functions (cutoff estimation, rolling averages, sustained grid detection, downsampling), DynamoDB query patterns with pagination, concurrency model (errgroup Phase 1/Phase 2), and testing strategy
- Lambda API spec: decision log with 14 decisions (SSM caching, computation location, sustained grid threshold, day data resolution, cutoff estimation method, error format, single system, timezone, downsampling algorithm, low24h data source, float precision, time-to-full deferral, read layer design, query optimisation)
- Lambda API spec: task list with 17 implementation tasks across 5 phases and 2 parallel streams, TDD-ordered with dependency tracking and requirement traceability
- Lambda API spec: three-level explanation (beginner/intermediate/expert) with validation findings
- Implementation explanation (`specs/poller/implementation.md`) at beginner, intermediate, and expert levels with completeness assessment
- Poller orchestrator (`internal/poller/poller.go`) with 4 independent polling goroutines (10s live data, 1h daily power, 6h daily energy, 24h system info), immediate first poll on startup, two-context graceful shutdown pattern (25s drain timeout), and dry-run API response logging
- Off-peak scheduler (`internal/poller/offpeak.go`) with snapshot capture at window boundaries, 3-attempt retry with 10s intervals, delta computation for 6 energy fields + battery SOC, pending record persistence for crash recovery, mid-window startup recovery via DynamoDB query, and daily scheduling loop with DST-safe wall-clock times
- Midnight energy finalizer goroutine that captures previous day's final energy totals at 00:05 local time using DST-safe `time.Date` scheduling
- `APIClient` interface in poller package for testability of AlphaESS client dependency
- GitHub Actions CI workflow (`.github/workflows/poller.yml`) triggered on push to main, running `go vet` and `go test`, then building and pushing ARM64 container image to GHCR with `latest` and short SHA tags
- DynamoDB table name environment variables (`TABLE_READINGS`, `TABLE_DAILY_ENERGY`, `TABLE_DAILY_POWER`, `TABLE_SYSTEM`, `TABLE_OFFPEAK`) and `TZ=Australia/Sydney` to ECS container definition in CloudFormation template
- `dynamodb:DeleteItem` permission to TaskRole IAM policy for off-peak pending record cleanup
- DynamoDB storage layer (`internal/dynamo`) with `Store` interface, `DynamoStore` (production), and `LogStore` (dry-run) implementations
- DynamoDB item models: `ReadingItem`, `DailyEnergyItem`, `DailyPowerItem`, `SystemItem`, `OffpeakItem` with `dynamodbav` struct tags and `Status` field for off-peak record lifecycle
- Transformation functions (`NewReadingItem`, `NewDailyEnergyItem`, `NewDailyPowerItems`, `NewSystemItem`) mapping AlphaESS API types to DynamoDB items with 30-day TTL computation
- `DynamoStore` with `BatchWriteItem` chunking (max 25) and one retry for unprocessed items, contextual error wrapping on all operations
- `DynamoAPI` interface for DynamoDB client to enable unit testing without AWS
- `LogStore` dry-run implementation that logs table name and item JSON for each write operation
- AWS SDK v2 dependencies (`dynamodb`, `attributevalue`, `config`)
- AlphaESS API client (`internal/alphaess`) with SHA-512 request signing, 4 endpoint methods (`GetLastPowerData`, `GetOneDayPower`, `GetOneDateEnergy`, `GetEssList`), API envelope parsing, serial number filtering for `GetEssList`, and contextual error wrapping with endpoint names
- AlphaESS API response models (`internal/alphaess/models.go`): `PowerData`, `EnergyData`, `PowerSnapshot`, `SystemInfo` structs with JSON tags, and `apiResponse` envelope with `json.RawMessage` data field
- Configuration package (`internal/config`) with `Load()` function that reads all settings from environment variables, validates offpeak HH:MM times and timezone, collects all errors before reporting, and relaxes AWS/DynamoDB requirements in dry-run mode
- `testify` test dependency for assertions
- Go module (`github.com/ArjenSchwarz/flux`) with Go 1.26 and project directory structure (`cmd/poller/`, `internal/alphaess/`, `internal/config/`, `internal/dynamo/`, `internal/poller/`)
- Poller entrypoint (`cmd/poller/main.go`) with `os.Args` dispatch for `healthcheck` subcommand, slog JSON handler with `ReplaceAttr` (time→timestamp, lowercase levels), config loading, store creation (DynamoStore or LogStore), signal handling (SIGTERM/SIGINT), and poller startup/shutdown logging
- Health check (`cmd/poller/healthcheck.go`) querying `flux-readings` for the most recent item, returning exit 0 if reading is ≤60s old, exit 1 otherwise; dry-run mode always returns healthy; `healthQueryAPI` interface for testability
- Multi-stage Dockerfile: `golang:1.26-alpine` builder with ARM64 cross-compilation (`-trimpath -ldflags="-s -w"`), `gcr.io/distroless/static:nonroot` runtime with binary at `/poller`
- Embedded timezone data via `time/tzdata` blank import for distroless container compatibility
- Makefile with targets: build, test, fmt, vet, lint, modernize, check, docker-build, docker-dry-run, deps-tidy, deps-update
- `.dockerignore` excluding `.git/`, `specs/`, `docs/`, `infrastructure/`, `.github/`
- Poller spec: requirements document with 13 sections and 91 acceptance criteria covering Go application setup, AlphaESS API client, multi-schedule polling, DynamoDB storage, configuration, off-peak energy calculation, health check, process lifecycle, logging, Dockerfile, GitHub Actions CI, dry-run mode, and infrastructure update
- Poller spec: design document with architecture diagram, 9 component designs (entrypoint, config, AlphaESS client, Store interface with DynamoStore/LogStore, DynamoDB models, poller orchestrator, off-peak scheduler), two-context graceful shutdown pattern, error handling strategy, and testing strategy
- Poller spec: decision log with 16 decisions (log-and-skip error handling, structured logs only, off-peak SOC tracking, pgridSustained in Lambda, internal/ packages, os.Args over cobra, Store interface for dry-run, distroless base image, two-context shutdown, off-peak status field)
- Poller spec: task list with 23 tasks across 8 phases and 2 parallel streams, TDD-ordered with dependency tracking and requirement traceability
- CLAUDE.md project instructions for the flux repository
- VPC (`10.0.0.0/24`) with DNS support and two public subnets across availability zones (`10.0.0.0/25`, `10.0.0.128/25`)
- Internet Gateway with VPC attachment
- Route table with default route to IGW, associated with both subnets
- DynamoDB and S3 Gateway VPC endpoints attached to route table
- Security group allowing all egress and no ingress for Fargate tasks
- CloudWatch log groups for poller (`/flux/poller`) and API (`/flux/api`) with 14-day retention and DeletionPolicy Delete
- IAM roles: `TaskExecutionRole` (SSM read, CloudWatch Logs write), `TaskRole` (DynamoDB read/write on all 5 tables), `LambdaExecutionRole` (DynamoDB read, SSM read, CloudWatch Logs write) — all least-privilege, ARN-scoped
- 5 DynamoDB tables: `flux-readings` (TTL), `flux-daily-energy`, `flux-daily-power` (TTL), `flux-system`, `flux-offpeak` — all PAY_PER_REQUEST with DeletionPolicy Retain
- SSM parameters for app-id, serial, offpeak-start, and offpeak-end (String type, stack-managed via `SSMPathPrefix`)
- ECS cluster, Fargate task definition (ARM64, 256 CPU, 512 MB) with SSM secrets injection, health check (`/poller healthcheck`), and awslogs log driver
- ECS service (`flux-poller`) with Fargate launch type, both subnets, public IP enabled
- Lambda function (`flux-api`) with `provided.al2023` runtime, ARM64, 128 MB, 10s timeout, environment variables for SSM paths and DynamoDB table names
- Lambda Function URL (auth type NONE) with public invoke permission
- `.gitignore` with entries for `lambda/bootstrap` and `infrastructure/packaged.yaml`
- CloudFormation template skeleton (`infrastructure/template.yaml`) with 6 parameters (ContainerImageUri, AlphaESSAppId, SystemSerialNumber, OffPeakWindowStart, OffPeakWindowEnd, SSMPathPrefix) and 3 outputs (FunctionUrl, EcsClusterName, EcsServiceName)
- Infrastructure spec: requirements document with 8 requirement groups and 42 acceptance criteria covering VPC, ECS Fargate, DynamoDB, Lambda, SSM, IAM, CloudFormation deployment, and CloudWatch Logs
- Infrastructure spec: design document with full architecture diagram, CloudFormation resource definitions, IAM policies, health check design, deployment procedure, and testing strategy
- Infrastructure spec: decision log with 11 documented decisions (single template, ARM64, bearer token auth, on-demand DynamoDB, manual SecureString creation, cfn package deploy, DynamoDB health check, etc.)
- Infrastructure spec: task list with 12 implementation tasks across 6 phases, dependency-ordered with requirement traceability
- Infrastructure spec: prerequisites document listing manual setup steps required before deployment
- Deployment README (`infrastructure/README.md`) with prerequisites, SecureString setup commands, build/package/deploy workflow, and update procedures for Lambda code, container image, configuration, and infrastructure changes
- Infrastructure spec: implementation explanation at beginner, intermediate, and expert levels with completeness assessment

### Breaking Changes

- `OffPeakWindowStart` and `OffPeakWindowEnd` CloudFormation parameters no longer have defaults and must be supplied explicitly via the parameters file or `--parameter-overrides` on every deploy

### Fixed

- iOS spec-validation hardening across dashboard/history/day detail: first-load dashboard failures now render an explicit error card with retry/settings actions, history shows inline retry/settings when network fails and cache is empty, day detail auth/config failures now provide settings recovery, and SOC low chart annotation now includes low-time text
- Shared iOS date and error handling logic to reduce duplication and improve consistency: centralized day parsing/formatting/calendar usage via `DateFormatting`, unified error coercion with `FluxAPIError.from(_:)`, and updated history cache writes to upsert existing `CachedDayEnergy` records instead of repeatedly inserting duplicate dates
- `DayDetailViewModelTests.navigatePreviousAndNextUpdateDateString` now uses a deterministic non-today reference date, preventing timezone-dependent false failures
- Off-peak window parameters interpreted as integers (`11:00` → `660`, `14:00` → `840`) due to YAML 1.1 sexagesimal parsing after `aws cloudformation package` re-serializes the template and strips quotes. Defaults removed and `AllowedPattern` added for deploy-time validation.
- Lambda `Code` path corrected from `./lambda/` to `../lambda/` (relative to template location)
- Lambda Function URL returning 403 — added `lambda:InvokeFunction` permission alongside `lambda:InvokeFunctionUrl` in the resource policy, both are required for public access with `AuthType: NONE`
- `computeCutoffTime` now guards against NaN/Inf results from very small `pbat` values and rejects `capacityKwh <= 0`, preventing unreasonable cutoff times from reaching the API response

### Changed

- Lambda MemorySize increased from 128MB to 256MB for 24h query headroom
- Lambda environment variables: added `TZ: Australia/Sydney` to match poller timezone for date-based operations
- `DynamoStore.GetOffpeak` refactored to use shared `getItem[T]` helper, eliminating implementation divergence with `DynamoReader.GetOffpeak`
- `/day` endpoint now queries readings and daily energy concurrently via errgroup, matching the `/status` endpoint pattern
- `time.LoadLocation("Australia/Sydney")` extracted to package-level `sydneyTZ` variable with fail-fast error handling, replacing 4 repeated calls (including one inside a loop)
- `errorResponse` now uses `json.Marshal` instead of `fmt.Sprintf` with `%q` to produce valid JSON escaping for all inputs
- Removed unnecessary `sort.Slice` in `downsample` — output is already chronological since buckets are iterated 0..287
- Go module version updated from 1.25 to 1.26 to match spec requirements
- Exported `config.FormatHHMM` and removed duplicate in `cmd/poller/main.go`
- Off-peak status values use `dynamo.OffpeakStatusPending` and `dynamo.OffpeakStatusComplete` constants instead of raw strings
- `timePosition` returns typed `windowPosition` instead of raw strings
- Extracted `pollLoop` helper to eliminate repeated poll goroutine pattern across 4 schedules
- Extracted `handleEndOrCleanup` helper to eliminate 3x repeated off-peak end-failure cleanup block
- Makefile `modernize` target now uses `go mod tidy -compat=1.26` per spec [1.6]
- CLAUDE.md corrected `flux-daily-power` TTL from 7d to 30d (per Decision 10)

### Fixed

- `explanation.md` incorrectly referenced `Australia/Melbourne` instead of `Australia/Sydney` in the expert section
- Makefile `docker-dry-run` target used `TZ=Australia/Melbourne` instead of `TZ=Australia/Sydney`
- CHANGELOG.md had duplicate `### Added` and `### Changed` section headers — consolidated into single sections
- AlphaESS API client now uses GET requests with query parameters instead of POST with JSON body, matching the actual API specification (fixes HTTP 405 errors)
- AlphaESS API envelope success check now accepts both code 0 and code 200, matching the API's actual success response format
- Duplicate `dynamodb:DeleteItem` permission removed from TaskRole IAM policy in CloudFormation template
- CloudWatch Logs IAM policies now use `:*` suffix on log group ARNs for `TaskExecutionRole` and `LambdaExecutionRole`, required for `logs:CreateLogStream` and `logs:PutLogEvents` to match log stream resources
