# Bugfix Report: Dashboard shows stale "live" data overnight

**Date:** 2026-05-19
**Status:** Fixed
**Transit:** T-1274

## Description of the Issue

From roughly 11 PM Sydney time onwards, the iOS Flux Dashboard becomes "useless" — it keeps rendering whatever battery / power / SoC numbers were last reported, with no indication that those numbers are hours old. The user expects either current values or a clear signal that current data is unavailable.

**Reproduction steps:**

1. Wait until late evening (around 11 PM Sydney local time) when AlphaESS appears to stop publishing fresh `getLastPowerData` updates for this serial.
2. Open the iOS Dashboard.
3. Observe that the SoC numeral, the live power trio (solar / house / grid), and the "discharging · X · empty by HH:MM" subline keep showing the same values as they did several hours earlier.
4. The eyebrow still reads "Now · HH:MM" using the device clock, so the dashboard *claims* to be current.

**Impact:** User-facing correctness and trust. The dashboard's primary job is to show what the battery is doing right now; presenting hours-old values as current makes it actively misleading.

## Investigation Summary

The dashboard's live readout comes from `StatusResponse.Live`, which the `/status` Lambda builds from the most recent row in `flux-readings` within the last 24 hours.

- `internal/poller/poller.go:138` — `pollLiveData` ticks every 10 s and calls `client.GetLastPowerData`. On error or timeout it logs and returns without writing anything (`fetchAndStoreLiveData` at line 169).
- `internal/api/status.go:78` — handler takes `latest := allReadings[len(allReadings)-1]` and populates `resp.Live` if any reading exists in the last 24 hours. There is no check on `latest.Timestamp`.
- `Flux/Flux/Dashboard/DashboardView.swift:113` — `DashboardHeroPanel(live: viewModel.status?.live, ...)`. If `live` is non-nil, the SoC and status line render normally; if nil, the panel falls back to "Awaiting live data". The view has no way to tell that a populated `live` is actually stale.
- `Flux/Flux/Dashboard/DashboardView.swift:109` — the staleness banner is only shown when `viewModel.error != nil`, i.e. when the API call itself failed. A successful `/status` that just happens to carry an old reading slips past this gate.

**Hypotheses tested:**

- *AlphaESS API errors silently at night.* Plausible — the poller's `slog.Error("fetch live data failed", ...)` path swallows the error and `latest.Timestamp` ages until the API recovers. This matches the observed symptom.
- *AlphaESS returns the same snapshot repeatedly.* Possible. The poller writes a fresh-timestamped row every 10 s either way, so this case is harder to detect from a single response. Out of scope for this fix.
- *DynamoDB TTL pruning.* Ruled out — TTL is 30 days on `flux-readings`.
- *iOS app filtering.* Ruled out — the dashboard renders whatever `Live` it receives.

## Discovered Root Cause

Neither the backend nor the frontend treats "the latest reading is too old" as a distinct signal. `live.timestamp` is already in the wire response but no client reads it; `/status` returns the latest reading regardless of age. As soon as `pollLiveData` stops succeeding (AlphaESS quiet hours, transient errors, container restart in a bad window, etc.), the dashboard keeps presenting the last good reading as if it were current.

**Defect type:** Missing freshness check on the data returned to clients.

**Why it occurred:** The original `/status` design assumed the poller's 10 s cadence would always produce a fresh row, so the latest reading was treated as authoritative without timestamp validation. Errors on the poller side are logged but not propagated to the API surface.

## Resolution for the Issue

`/status` now drops `Live` (and `battery.estimatedCutoffTime`, which is computed from the same latest reading) when the most recent reading is older than `liveDataStalenessThreshold` (90 s — nine missed 10 s polls is unambiguously broken). The existing "Awaiting live data" UI state in `DashboardHeroPanel` then surfaces naturally, telling the user the dashboard can't show a current reading instead of pretending an aged one is live.

**Changes made:**

- `internal/api/status.go` — add `liveDataStalenessThreshold` constant; gate the population of `resp.Live` and `battery.EstimatedCutoff` on the latest reading's age. `battery.low24h` and `rolling15min` continue to be derived from their own time-windowed reading subsets, which already produce correct results when nothing is recent.
- `internal/api/status_test.go` — add `TestHandleStatusStaleLatestReading_OmitsLive` and `TestHandleStatusStalenessBoundary` covering the bug, the threshold edge, and the just-past-threshold case.
- `CHANGELOG.md` — note the dashboard staleness fix under the next release.

**Approach rationale:** Keep the fix server-side and minimal. Centralising the staleness call in the API means every client (iOS, macOS, widgets) benefits from a single source of truth, and the existing "Awaiting live data" UI handles the nil case without any client changes. The 90 s threshold is generous enough to absorb a handful of transient timeouts but tight enough that a sustained outage is reflected promptly.

**Alternatives considered:**

- *Client-side staleness on `live.timestamp`.* Rejected — multiple clients (iOS Dashboard, macOS Dashboard, widgets, Control Center) would each need to reimplement the same logic. The widget already uses a different "staleness" concept based on `fetchedAt`, so adding a second one only on the iOS view increases inconsistency.
- *Detect repeated-identical readings in the poller.* Rejected for this fix — heuristic, can false-positive when the system is legitimately steady (no load + no solar + idle battery), and adds complexity in a hot path. Worth revisiting only if we observe AlphaESS returning stale-but-distinct snapshots in practice.
- *Add a new `live.stale` flag rather than omitting `Live`.* Rejected — wider API surface change, every client would need updating, and the existing "Awaiting live data" UI already conveys exactly the intended meaning.

## Regression Test

**Test file:** `internal/api/status_test.go`

**Test names:**

- `TestHandleStatusStaleLatestReading_OmitsLive`
- `TestHandleStatusStalenessBoundary` (subtests: `fresh`, `at threshold`, `one past threshold`)

**What they verify:** A latest reading older than `liveDataStalenessThreshold` causes `/status` to omit both `Live` and `battery.EstimatedCutoff`. A reading at exactly the threshold is still considered fresh; one second past it is not.

**Run command:** `go test ./internal/api/ -run 'TestHandleStatusStale|TestHandleStatusStalenessBoundary' -v`

## Affected Files

| File | Change |
|------|--------|
| `internal/api/status.go` | Add `liveDataStalenessThreshold`; suppress `Live` and `battery.EstimatedCutoff` when latest reading exceeds the threshold. |
| `internal/api/status_test.go` | Add `TestHandleStatusStaleLatestReading_OmitsLive` and `TestHandleStatusStalenessBoundary`. |
| `CHANGELOG.md` | Note the fix under the next release. |
| `specs/bugfixes/late-night-current-data-gap/report.md` | This report. |

## Verification

**Automated:**

- [ ] Regression tests pass
- [ ] Full test suite passes (`go test ./...`)
- [ ] `make lint` passes

**Manual verification:**

- Once deployed, the next time AlphaESS goes quiet for more than ~90 s the iOS Dashboard switches to "Awaiting live data" with a "—" SoC instead of holding the previous numbers. When AlphaESS resumes, the next poll cycle restores the live readout.

## Prevention

- Any future endpoint that surfaces "current" measurements should validate the underlying source's freshness before returning it as live.
- Consider a follow-up that emits a CloudWatch metric when the poller skips a live write so sustained outages are visible without waiting for a user report.

## Related

- Transit: T-1274
- Branch: `T-1274/bugfix-late-night-current-data-gap`
