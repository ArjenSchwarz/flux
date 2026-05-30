# Bugfix Report: Today's peak grid import degraded/missing on Day Detail and History

**Date:** 2026-05-30
**Status:** Fixed

## Description of the Issue

Today's peak grid import was not shown accurately — and frequently not at all — on the Day Detail and History screens. The iOS rendering of the peak/off-peak grid-import split was parasitic on the off-peak split being present, and today's off-peak split only exists once the `flux-offpeak` row is created at the 11:00 window start.

**Reproduction steps:**
1. Open the History (or Day Detail) screen for today, before 11:00 local (Australia/Sydney).
2. Observe the grid import card: History omits today's grid entry entirely; Day Detail collapses to a single combined "Grid in" row — no peak/off-peak breakdown.
3. After 11:00, observe that the peak figure is the `eInput − offpeak` residual, which is a cross-source value (today's `eInput` is `max(stored AlphaESS counter, readings integration)` while off-peak is a pure readings integration), so it can be inflated.

**Impact:** User-visible on the two history screens (and the Dashboard, tracked as T-1421) for all of "today" — the most-viewed day. Severity: moderate; data was misleading or absent but not corrupt.

## Investigation Summary

Routed from `/transit` as a bug; investigated with two read-only exploration passes (backend Go, iOS Swift) and direct reads of the live-compute paths.

- **Symptoms examined:** missing grid breakdown before 11:00; residual (not integrated) peak after 11:00.
- **Code inspected:** `internal/api/{compute,status,day,history,response}.go`, `internal/derivedstats/integrate_offpeak.go`; iOS `SummaryBlock.swift`, `HistoryDerivedState.swift`, `APIModels.swift`, `DashboardView.swift`.
- **Hypotheses tested:** initially framed as two defects (accuracy + rendering). Mid-investigation the user pulled PR #58 ("Peak From Readings"). Verified #58 added a server-computed `peakGridImportKwh` for **past days only** (its commit and Decision 4a explicitly exclude a real-time path for today) and did **not** touch the off-peak rendering gate. Confirmed both the live-today accuracy gap and the rendering gate remained.

## Discovered Root Cause

Two independent defects, both scoped to "today":

1. **Rendering gate (the visible bug).** `HistoryDerivedState.gridEntry` guarded `guard let offpeakImport = day.offpeakGridImportKwh else { return nil }`, and `SummaryBlock.gridInRows` rendered the split only `if offpeakGridImport != nil`. Today's off-peak split does not exist before the 11:00 window opens, so the breakdown was dropped.
2. **Cross-source residual (the accuracy bug).** No server-side live peak existed for today; iOS derived peak as `max(0, eInput − offpeak)`. For today, `eInput = reconcileEnergy(computed, stored) = max(readings, AlphaESS snapshot)` while `offpeak` is a pure readings integration, so subtracting the two mixes sources and inflates peak when the snapshot wins the reconcile.

**Defect type:** Logic/coupling error (presentation coupled to an unrelated data dependency) + missing computation path.

**Why it occurred:** Off-peak-from-readings introduced the live off-peak split at the window start and the residual-for-today peak; the iOS card was wired to off-peak presence as the proxy for "do we have a split." Peak-from-readings (#58) then added an accurate server peak but deliberately deferred today (Decision 4a) and the Dashboard (Decision 8).

**Contributing factors:** `derivedstats.IntegratePeakGridImportKwh` requires **both** windows bracketing off-peak to pass the usability gate, which is wrong for the partial "today so far" case (the evening window is empty before 14:00), so it could not be reused as-is.

## Resolution for the Issue

Built a single server-side live-peak path consumed by all three "today" surfaces, and decoupled the iOS rendering from off-peak presence. Per the new CLAUDE.md "Data Consistency" rule (today's peak must read identically on Dashboard, Day Detail, History) the work folds in T-1421.

**Changes made:**
- `internal/api/compute.go` — new `livePeakGridImport(readings, now, offpeakStart, offpeakEnd) (float64, bool)`: integrates `max(pgrid,0)` over morning `[00:00, min(now, opStart))` + evening `[opEnd, min(now, dayEnd))`, gated on the morning window only (evening additive-when-usable), independent of `reconcileEnergy`. Trims to the Sydney day internally so `/status` (24h slice) and `/day`/`/history` (today-only) yield identical values.
- `internal/api/status.go` — set new `resp.PeakGridImportKwh` from `livePeakGridImport`.
- `internal/api/response.go` — new `StatusResponse.PeakGridImportKwh *float64 json:"peakGridImportKwh,omitempty"`.
- `internal/api/day.go` / `internal/api/history.go` — for today, set `peakGridImportKwh` from `livePeakGridImport`; past days keep the stored value.
- `Flux/Packages/FluxCore/Sources/FluxCore/Models/APIModels.swift` — decode `StatusResponse.peakGridImportKwh`.
- `Flux/Flux/Helpers/SummaryBlock.swift` — render the split whenever a peak value is present (`serverPeakGridImport != nil || offpeakGridImport != nil`), off-peak shown as `0` before the window; combined row only when neither exists. New `todayEnergy:` init accepts `serverPeakGridImport`.
- `Flux/Flux/History/HistoryDerivedState.swift` — `gridEntry` prefers the server peak, falls back to the residual, omits only when neither peak nor off-peak is known.
- `Flux/Flux/Dashboard/DashboardView.swift` — pass `status.peakGridImportKwh` into `SummaryBlock`.

**Approach rationale:** Computing the live peak once server-side and consuming it everywhere is exactly what the Data Consistency rule mandates. Direct integration carries peak's own ~1.5% sampling artifact rather than stacking the day's reconcile mismatch onto the residual. The server-peak-presence signal cleanly separates "off-peak genuinely 0 (window not open)" from "off-peak unknown" so a missing split is never shown as a misleading off-peak `0`.

**Alternatives considered:**
- iOS rendering decouple only (keep the residual) — rejected: leaves today's peak cross-source so the three screens can still disagree.
- Reuse `IntegratePeakGridImportKwh` — rejected: its both-windows gate can't produce a partial-day value before 14:00.

## Regression Test

**Test files:** `internal/api/compute_test.go`, `internal/api/peak_grid_import_test.go`, `Flux/FluxTests/HistoryViewModelTests.swift`, `Flux/Packages/FluxCore/Tests/FluxCoreTests/APIModelsTests.swift`

**Key test names:**
- Go: `TestLivePeakGridImport`, `TestLivePeakGridImportGates`, `TestHandleDayTodayLivePeakGridImport`, `TestHandleHistoryTodayLivePeakGridImport`, `TestHandleStatusLivePeakGridImport`, `TestTodayPeakGridImportConsistentAcrossEndpoints` (asserts `/status` == `/day` == `/history` for today — the Data Consistency guarantee — with pre-midnight readings to prove the internal day-trim)
- Swift: `gridSeriesIncludesTodayWithServerPeakBeforeOffpeakWindow`, `gridSeriesOmitsDayWithNeitherPeakNorOffpeak`, `decodeFullStatusResponse` (extended)

**What they verify:** the morning/during/after-off-peak integration semantics and gates; that `/status`, `/day`, `/history` expose today's live peak independent of the off-peak split; that History renders today's grid entry (off-peak shown as 0) before 11:00 and still omits a genuinely-split-less past row.

**Run command:** `go test ./...` and `make macos-test`

## Affected Files

| File | Change |
|------|--------|
| `internal/api/compute.go` | New `livePeakGridImport` |
| `internal/api/status.go` | Populate `peakGridImportKwh` |
| `internal/api/response.go` | New `StatusResponse.PeakGridImportKwh` |
| `internal/api/day.go` | Today uses live peak; past uses stored |
| `internal/api/history.go` | Today row uses live peak; past uses stored |
| `internal/api/compute_test.go` | `livePeakGridImport` unit tests |
| `internal/api/peak_grid_import_test.go` | Today live-peak handler tests |
| `Flux/Packages/FluxCore/Sources/FluxCore/Models/APIModels.swift` | Decode `peakGridImportKwh` on `StatusResponse` |
| `Flux/Flux/Helpers/SummaryBlock.swift` | Decouple split from off-peak presence; accept server peak |
| `Flux/Flux/History/HistoryDerivedState.swift` | `gridEntry` decouple |
| `Flux/Flux/Dashboard/DashboardView.swift` | Pass `/status` peak into `SummaryBlock` |
| `Flux/.../APIModelsTests.swift`, `Flux/FluxTests/HistoryViewModelTests.swift` | Regression tests |
| `specs/peak-from-readings/decision_log.md` | Decision 9 (supersedes 4a + Decision 8 deferral) |

## Verification

**Automated:**
- [x] Regression tests pass (new Go + Swift tests green)
- [x] Full Go suite passes (`go test ./...` exit 0); macOS build succeeds; Swift peak/grid/status/dashboard tests pass
- [x] Linters pass for changed code: `golangci-lint` 0 issues; SwiftLint reports no violations in changed files (the `HistoryDerivedState.swift` `file_length` flag under `--strict` is pre-existing — origin/main is already 401 lines, and origin/main has 73 such `--strict` violations, so the project does not enforce `--strict`)

**Known unrelated test failures (pre-existing, environmental):**
- `KeychainAccessibilityMigratorTests` (2 cases) fail deterministically in the local test host (keychain class-migration under "Sign to Run Locally"); keychain code is byte-identical to origin/main.
- `DashboardViewModel*` concurrency/tier tests are flaky under full-suite contention; all pass in isolation.

**Manual verification:** Not run on-device; behaviour is covered by the handler + view-model tests. The data path is deterministic given `now` and the readings slice.

## Prevention

- Avoid coupling presentation to an indirect data dependency (here, gating the peak row on off-peak presence). Render a value when *that* value is available.
- When adding a derived metric shown on multiple screens, compute it once (server-side where possible) per the CLAUDE.md Data Consistency rule, rather than re-deriving per screen.
- When a shared integrator has a usability gate spanning multiple windows, provide a partial-window variant for live "so far today" rather than overloading the all-windows one.

## Related

- Transit: T-1420 (this bug), T-1421 (Dashboard real-time peak, folded in)
- Spec: `specs/peak-from-readings/` — Decision 1, 4, 8, and new **Decision 9**
- PR #58 "Peak From Readings" (server peak for past days; this builds the today/live path it deferred)
