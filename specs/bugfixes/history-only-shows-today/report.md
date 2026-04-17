# Bugfix Report: History only shows today

**Date:** 2026-04-18
**Status:** Fixed

## Description of the Issue

The iOS History screen renders only one bar — for today. Bars for previous days in the 7/14/30-day range are absent.

**Reproduction steps:**
1. Open the Flux iOS app
2. Tap the History tab
3. Observe the chart shows a single bar at the right edge (today) and nothing for preceding days

**Impact:** Users cannot see any historical energy data in the app. High severity for a monitoring tool whose main purpose is trend visibility; two merged "fixes" previously targeted iOS rendering (PR #13) and did not address the real cause.

## Investigation Summary

Applied the systematic-debugger methodology (modified Fagan inspection) after a prior speculative iOS-side fix (PR #13) failed to resolve the bug.

- **Symptoms examined:** Chart has one visible bar (today). Summary card only ever shows today's values.
- **Code inspected:** `internal/api/history.go`, `internal/dynamo/reader.go`, `Flux/Flux/Services/URLSessionAPIClient.swift`, `Flux/Flux/History/HistoryViewModel.swift`, `Flux/Flux/History/HistoryChartView.swift`, `internal/poller/poller.go`, `internal/alphaess/client.go`.
- **Hypotheses tested and ruled out:**
  - *iOS chart axis type* — PR #13 switched x-axis from `Date` to `String`; bug persists. Reverted in PR #15.
  - *DynamoDB missing historical rows* — rows exist for past days.
  - *Backend filtering* — `/history` correctly returns all rows in the range.
- **Empirical verification:** Called the deployed `/history?days=7` endpoint. Response contained rows for 2026-04-15, 2026-04-16, 2026-04-17. Metric values for 2026-04-15 and 2026-04-16 were all zero; today's row was non-zero. SwiftUI Charts was rendering zero-height (invisible) bars for the historical days — behaving correctly, given the data.
- **AlphaESS probe:** Queried `getOneDateEnergyBySn` directly for the same three dates on 2026-04-18:
  - 2026-04-15 → real values (e.g. `epv=8.2`, `eCharge=11.4`)
  - 2026-04-16 → real values (e.g. `epv=18`, `eCharge=10.2`)
  - 2026-04-17 → **all zero**

The pattern is definitive: AlphaESS returns **zero for "yesterday"** but real values for days two or more prior. AlphaESS has a day-finalisation latency that exceeds 5 minutes past Sydney midnight.

## Discovered Root Cause

The poller's `midnightFinalizer` (`internal/poller/poller.go:121`) runs at Sydney 00:05 each night and calls `fetchAndStoreDailyEnergy(yesterday)`. At that moment the hourly poll has already accumulated yesterday's running-total row in DynamoDB. The finalizer fetches from AlphaESS — which returns all-zero because it has not yet finalised the previous day — and the poller **unconditionally writes the zero-valued response over the real row**, corrupting historical data.

**Defect type:** Missing validation / unsafe overwrite.

**Why it occurred:** `fetchAndStoreDailyEnergy` treats any non-error response from AlphaESS as authoritative. It does not distinguish between "no data yet" (all zeros) and "final totals". There was no prior empirical evidence that AlphaESS returns zeros for yesterday, and the midnight finalizer design implicitly assumed AlphaESS would have a final yesterday total available within 5 minutes of local midnight.

**Contributing factors:**
- The hourly poller writes to the *same* DynamoDB row using today's date in Sydney time, so the window between "hourly poll wrote real running totals" and "midnight finalizer overwrites with zeros" is just ~1 hour — making the corruption near-guaranteed every night.
- No alerting or sanity checks on the daily-energy write path.
- PR #13's regression tests verified ViewModel data-shape only, not SwiftUI rendering — so they passed while the UI was still broken.

## Resolution for the Issue

**Changes made:**
- `internal/poller/poller.go` — In `fetchAndStoreDailyEnergy`, skip the write and log a warning when every energy field in the AlphaESS response is zero. No legitimate day on an operating battery system produces all-zero totals; treating such a response as "not-yet-finalised" is safe.
- `internal/poller/poller_test.go` — New regression test `TestFetchAndStoreDailyEnergy_AllZero_SkipsWrite` locks in the guard.

**Backfill:** A one-off invocation re-queried AlphaESS for 2026-04-15 and 2026-04-16 and wrote the real values to `flux-daily-energy`, replacing the zero rows.

**Approach rationale:** A pure data-layer defensive guard. The poller has no reliable way to distinguish "AlphaESS hasn't finalised yesterday yet" from "the day really had zero activity", but for a working battery system the latter cannot happen. Filtering by the all-zero predicate protects against this issue and any similar AlphaESS glitches without depending on timezone assumptions.

**Alternatives considered:**
- *Shift the midnight finalizer later (e.g., to T+48h)* — Rejected as primary fix because we don't know AlphaESS's exact finalisation window; a guard is still required to be safe. May be layered on later as an optimisation.
- *Remove the midnight finalizer entirely* — Rejected because the hourly poll's last-of-day value may be a few minutes stale relative to AlphaESS's actual final. The finalizer (with the zero-guard) still serves a purpose once AlphaESS's finalisation propagates.
- *Fix SwiftUI Charts to show zero-value bars visibly* — Rejected. That masks a backend data integrity bug.

## Regression Test

**Test file:** `internal/poller/poller_test.go`
**Test name:** `TestFetchAndStoreDailyEnergy_AllZero_SkipsWrite`

**What it verifies:** When `GetOneDateEnergy` returns a non-error response with every energy field equal to zero, the poller does not call `WriteDailyEnergy` and logs a warning naming the date. A second sub-test confirms that a response with at least one non-zero field still writes normally.

**Run command:** `go test ./internal/poller/ -run TestFetchAndStoreDailyEnergy`

## Affected Files

| File | Change |
|------|--------|
| `internal/poller/poller.go` | Added `isAllZeroEnergy` guard in `fetchAndStoreDailyEnergy`. |
| `internal/poller/poller_test.go` | Added regression test for the zero-overwrite guard. |

## Verification

**Automated:**
- [x] Regression test passes
- [x] Full Go test suite passes
- [x] `make lint` passes

**Manual verification:**
- Called `/history?days=7` and confirmed 2026-04-15 and 2026-04-16 now return real values after backfill.
- Confirmed iOS History screen renders bars for all days in range (visual inspection on device).

## Prevention

**Recommendations to avoid similar bugs:**
- When integrating an external API, test behaviour at day boundaries in the API's own timezone, not just the local timezone. The finalisation latency pattern seen here is not unique to AlphaESS.
- For any write that can overwrite existing data, include a "does the new value look plausible" check. The cost is small; the cost of silent corruption is large.
- Unit tests for SwiftUI rendering (snapshot tests, or at minimum one human-verified screenshot per PR touching charts) would have caught PR #13's failure before merge.
- The `/fix-bug` skill requires a `specs/bugfixes/<bug-name>/report.md` as part of the PR. The three PRs merged in the earlier bug-blitz (#12, #13, #14) all skipped this step; PR-review automation should block merges when the report is missing.

## Related

- Transit ticket: T-841
- Previous (wrong) fix: PR #13, reverted in PR #15
- Related discrepancy PR: #11 (T-828) — different symptom, same data-layer neighbourhood
