# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Changed

- **Time-of-use pricing — app** (T-1890, T-1891). Fourth implementation phase of the band-based pricing spec, and the first with user-visible change. Settings ▸ Pricing now edits band-based plans, and costs are computed from them.
  - Settings ▸ Pricing lists plans rather than periods. Each row shows its date range and the bands its rates apply over — "Free 10:00–15:00 · $0.2800 01:00–06:00 · $0.3500 default" — with feed-in on its own line. A closed plan reads "2026-01-01 until 2026-08-01" rather than a dash range, because the end date is exclusive and that day belongs to the successor.
  - The plan editor replaces the three rate fields with a default rate, a feed-in rate, a savings reference rate (shown only once a free window exists), and a Windows section: per row a start and end picker, a Free toggle, and a rate field when the window isn't free. Times outside every window take the default rate, so a plan cannot be entered with a gap. Client-side validation mirrors the server's band rules, so a plan the editor accepts is not rejected for something it could have caught.
  - The succession affordance now says what it does: it ends the open-ended plan **on** the new plan's start date and starts the successor that same day, rather than the day before. Both rows carry the same literal date.
  - Day costs resolve in three tiers, matching the backend figure for figure: the stored per-band split when its geometry matches the plan and the free band's import is resolvable; otherwise the pre-band single-rate formula, which is what keeps every historical day's cost unchanged; otherwise all import at the plan's highest rate with no savings. Both cost helpers are pinned to the same cross-language vectors the Go implementation is tested against. The cost card and History totals keep their existing layout.
  - Day Detail chart shading takes the free window from the plan pricing that day instead of a hardcoded 11:00–14:00, so it moves on the switch date. A day with no plan, or whose plan has no free band, gets no shading rather than a misleading band — and the widgets no longer substitute the legacy window when the API reports none.
  - `/day` and `/history` now also report the window and integration provenance of the off-peak row each day's off-peak import came from. Without it the app could not tell a split captured under the current free window from a stale one, and every day priced by the new plan would have fallen back to the highest-rate estimate. The History cache stores it too, so an offline day prices identically to an online one.

### Internal

- **Time-of-use pricing — poller and operator tools** (T-1890, T-1891). Third implementation phase of the band-based pricing spec. The poller now takes its off-peak window from the plan pricing each day, captures the durable per-band split, and the operator tools follow suit.
  - New `PlanSource` gives the poller a read-through view of the pricing table with a last-good cache. A failed read is served from the cache with a warning and is never resolved as "no plan" — that would silently strip a day of its free window and its band split. A cold start with an unreachable table retries with backoff before giving up.
  - The off-peak scheduler's daily cycle is re-anchored to local midnight: it wakes, refreshes the plans, resolves that day's free window, and sleeps to its start. Plans only change behaviour at midnight, so one refresh per day is exactly enough and a plan switch changes the window with no reconfiguration. A day whose plan has no free band sleeps through to the next midnight; an unreadable pricing table retries within the day rather than writing the day off.
  - Window-end finalisation no longer requires a pending row or a start snapshot. The five deltas have come from integrating readings since T-1341 — the snapshot is diagnostics only — so a plan-read failure at window start now costs forensics rather than the whole day, and a restart or a late plan load still closes the window. Finalised rows record the window geometry they were integrated under, so a later plan edit is detectable instead of silently repricing the day.
  - The summarisation pass replaces its single unresolved-window early return with per-outcome gating. A plan read failure writes nothing and is retried; a plan with a free band runs every block; a plan without one runs the window-independent blocks with the whole day rated; a day no plan prices still gets its lowest-SoC figure, with the band sentinel left unset so a backfill can capture the split later. The `skipped-ssm-unresolved` metric dimension is gone.
  - The pass gains the rated-band capture: `bandImports` and `peakGridImportKwh` now come from one integration over the plan's rated segments, so the two can no longer disagree. Segment boundaries come from wall-clock time, which also retires the latent hour-off bug the peak block carried on DST days.
  - `cmd/backfill-grid` and `cmd/backfill-solar` resolve each day's window from the pricing table instead of `--offpeak-start`/`--offpeak-end`. A backfill spanning a plan switch needs a different window on either side; static flags would have silently misattributed every day on the wrong side. `backfill-grid` additionally rewrites the day's rated `bandImports` and the off-peak row's window geometry. The two writes go to different tables in separate calls — non-atomic, but idempotent and re-runnable.
  - New `cmd/migrate-pricing` converts the legacy three-rate rows to the band shape once. It prices every retained day under the pre-migration rules using an independently written copy of the three-rate formula, transforms the rows, prices every day again under the band model, and aborts before a single write if any day differs. Dry-run by default; `--apply` writes full items preserving row ids and leaving the sentinel alone; rows already in the band shape are skipped, so re-running is a no-op.
  - `OFFPEAK_START`/`OFFPEAK_END` and the `OffPeakWindowStart`/`OffPeakWindowEnd` CloudFormation parameters are gone from both containers. The poller container gains `TABLE_PRICING` and read-only access (`Scan`/`GetItem`/`Query`) to the pricing table; the Lambda keeps sole write access. Deploying this version leaves `/flux/offpeak-start` and `/flux/offpeak-end` behind as orphaned SSM parameters that nothing reads.

- **Time-of-use pricing — Lambda API** (T-1890, T-1891). Second implementation phase of the band-based pricing spec; the API now speaks the band shape and derives the off-peak window from plans.
  - `/pricing` CRUD and `replace-open-ended` take and return the band shape (`defaultRate` + `windows` + `savingsReferenceRate`), with the exclusive end date stored exactly as sent. Validation reports the violated band rule (`band_window_invalid`, `band_overlap`, `multiple_free_bands`, `savings_rate_missing`, `no_rated_band`) alongside the existing rate and date rules, and date-range overlaps now name the conflicting plan. A pre-migration three-rate payload is rejected with `legacy_shape`, detected on the raw JSON keys because `encoding/json` silently drops unknown fields and would otherwise decode it as a zero-rate band plan.
  - `/status`, `/day`, and `/history` resolve the off-peak window from the plan pricing the day in question instead of the `OFFPEAK_START`/`OFFPEAK_END` environment variables, which are gone from the Lambda. Cutoff suppression and charge projection take the window from the plan pricing the day the *next* window falls on, so on the eve of a plan switch they follow the successor's window rather than the outgoing one.
  - `/status.offpeak` is now nullable: a day whose plan has no free band — or that no plan prices — serialises `null` rather than emitting a window, so clients render "no window" instead of substituting a default. Off-peak and peak values on such a day are absent, never zero. A pricing read failure fails the request rather than resolving as "no plan".
  - `/day` and `/history` gain a nullable `bandImports` array (rated bands only; the free band's import stays in `offpeakGridImportKwh`). Past days serve the split captured at day close; today's is integrated live from readings through a single shared helper both endpoints call, so the two screens cannot disagree. A band the clock has not reached reads zero, but a started band that cannot be integrated makes the whole split unavailable. `/status` does not carry the split.

- **Time-of-use pricing — Go plan domain and data layer** (T-1890, T-1891). First implementation phase of the band-based pricing spec; no user-visible change yet.
  - New leaf package `internal/plan` holding the plan domain: a default rate plus exception windows, the derived full-day segmentation, per-date plan selection, free-window resolution, and DST-correct wall-clock segment bounds. Band boundaries use a new parser that accepts `24:00` as end-of-day — `derivedstats.ParseOffpeakWindow` rejects hours above 23 and would reject every plan. Validation covers the band rules (invalid window, overlap, multiple free bands, missing savings rate, no rated band) alongside the existing rate bounds and precision rules.
  - Plan end dates are now **exclusive**: a plan ending on the date its successor starts hands that whole day to the successor, with no ±1 arithmetic in validation, succession, or display. `ReplaceOpenEnded` writes the same literal switch date to both rows and refuses a succession whose closing row is still the pre-migration shape.
  - Three-tier day-cost resolution in Go (stored band split → the pre-band single-rate formula verbatim → highest-rate fallback), pinned together with the segmentation to shared cross-language vectors in `internal/api/testdata/` that the FluxCore implementation will be held to. The single-rate tier is what keeps every historical day's cost identical after migration.
  - Storage moves to the band shape: `flux-pricing` rows store `defaultRate` + `windows` + `savingsReferenceRate`, `flux-daily-energy` gains a sentinel-gated `bandImports` group for the durable per-band split, and `flux-offpeak` rows snapshot the free-window geometry they were integrated under. Pre-migration rows are detected on the raw attribute map and converted on read, so band-aware services can deploy ahead of the migration run.
  - Property-based tests assert the segments always tile 00:00–24:00, that abutting same-rate segments stay separate, that at most one plan prices any date, and that per-segment grid-import integrals sum to the whole-day integral on 23-, 24-, and 25-hour Sydney days.

### Documentation

- **Time-of-use pricing spec** (T-1890, T-1891; `specs/time-of-use-pricing/`). Full spec for reworking pricing plans into daily time bands — a default rate plus exception windows (the incoming plan: free 10:00–15:00, cheaper 01:00–06:00, standard rate otherwise) — with same-day plan succession via exclusive end dates, and the active plan replacing the SSM off-peak window as the source of truth across the poller and API. Covers durable per-band import capture at day close (so banded costs outlive the 30-day readings TTL), a three-tier FluxCore cost resolution that keeps all historical costs identical, a one-time `cmd/migrate-pricing` CLI with golden-value verification, requirements, design, decision log (6 ADRs, 36 quick decisions), a 39-task TDD implementation plan in three parallel streams, and manual cutover prerequisites. Planning artifacts only — no code changes.
- **Architecture diagrams** (`docs/architecture-diagrams.md`). A reference document with eight Mermaid diagrams covering the system overview, build and deploy pipeline, AWS infrastructure, the poller's polling engine, the DynamoDB data model (11 tables), the Lambda API surface, the dashboard refresh sequence, and the SoC-alert push flow. Each diagram is drawn from the source of truth (`infrastructure/template.yaml`, the Go services, and the apps) and validated against the Mermaid parser. Intended both as documentation and as source material for a write-up about Flux.
- Corrected the DynamoDB table list in `CLAUDE.md` to enumerate all 11 tables with their retention policy (it previously listed only the original 5).

## [1.7] - 2026-06-15

### Added

- **Off-peak charge projection** (T-1533). While the battery is charging during the off-peak window, the Dashboard hero's charging line now shows the state-of-charge the battery is expected to reach by the window's end — e.g. `Charging · 4.50 kW · ~99% by 14:00`. The figure is computed server-side on `GET /status` from an idealised two-rate charge curve (4.5 kW up to 95%, then 500 W to 100%), clamped to the current SoC and 100%, and reuses the cutoff estimate's battery capacity so the two never disagree. It is a best-case estimate independent of live battery power, and appears only inside the window with fresh live data while charging. iOS + macOS.
- **History period navigation** (T-1497). The History screen's Wk and Mo ranges gain a period header — previous/next chevrons, a tappable label that opens a date picker (following the system's first day of week), and a Current button between the chevrons — for browsing past weeks and months, with ←/→ key navigation on macOS. Past periods are served from stored data via a new date-range form on `GET /history`; a partly-covered past period shows an "N of M days" subtitle, and an empty one shows a distinct no-data notice with the cards still in place. iOS + macOS.

### Internal

- Fixed a time-dependent failure in the SoC-alert evaluator tests that surfaced on 2026-06-15: the tests now pin the evaluator's clock to each reading instead of relying on wall-clock time. No app or backend behaviour change.

### Documentation

- Specs for the above: Off-peak charge projection (`specs/offpeak-charge-projection/`) and History period navigation (`specs/history-period-navigation/`).

## [1.6] - 2026-06-10

### Added

- **Week-to-date and month-to-date History ranges** (T-1361). The History range control gains two calendar-anchored options — **Wk** (this week so far) and **Mo** (this month so far) — alongside the fixed 7/14/30-day ranges, for a five-segment control (7d / 14d / 30d / Wk / Mo) with 7d still the default. Boundaries follow the Sydney calendar that keys the stored data, with the week's first day taken from the device locale, and are recomputed on every load. A partly-elapsed Wk or Mo draws its bars left-aligned with the remaining days held empty, so the layout stays stable from the first day of the period. The `/history` endpoint now accepts 1–31 days so a full month-to-date is never clipped, and the offline cache is bounded by the period start and auto-selects the newest cached day.
- **Dashboard Simulation** (T-1495). A new **Simulate** pill in the Dashboard hero applies a saved load preset — e.g. "Charge car" at 1.7 kW — as a clearly-labelled what-if, showing how house load, battery flow, and the "empty by" estimate would change without touching any device. Manage presets (label + watts) in **Settings → Simulation**. The simulation is computed on the backend so its figures stay consistent with the real Dashboard: the battery discharges up to its limit with any excess drawn from the grid, and on a sunny day the added load is taken from your solar export first. A distinct banner and tinted values make it obvious the numbers are simulated, and it clears when you turn it off or relaunch the app. iOS + macOS.

### Changed

- **History overview totals now include today.** Total solar, Total usage, Most usage, and Most solar (and the Battery card's discharged/charged totals) now count today's value, matching Peak imports, Exported, and Lowest SoC which already did — so a range whose only data is today shows real numbers and the daily-usage bar instead of em-dashes. Per-day averages still exclude today's partial day so they aren't skewed.

### Internal

- SwiftLint now passes `--strict` cleanly. Added `Flux/.swiftlint.yml` (the project had none) to exclude build artifacts and test targets, and fixed the 26 remaining production violations rather than suppressing them. No app behaviour changes.

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
