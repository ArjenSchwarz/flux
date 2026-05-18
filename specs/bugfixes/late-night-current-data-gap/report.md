# Bugfix Report: Dashboard shows all-zero "live" data overnight

**Date:** 2026-05-19
**Status:** Fixed
**Transit:** T-1274

## Description of the Issue

From roughly 11 PM Sydney time onwards, the iOS Flux Dashboard becomes "useless" — every live readout (SoC, solar, house load, grid, battery power) shows zero, and stays at zero until well after midnight. The user expected either current values or a clear signal that current data is unavailable; instead the dashboard appears to claim the battery is empty and the system is idle, which is wrong.

**Reproduction steps:**

1. Wait until late evening (around 11 PM Sydney local time) when AlphaESS stops publishing fresh `getLastPowerData` values for this serial.
2. Open the iOS Dashboard.
3. Observe SoC = 0%, ppv = 0 W, pload = 0 W, pgrid = 0 W, pbat = 0 W — every live field zero.
4. The eyebrow still reads "Now · HH:MM" using the device clock, so the dashboard appears to be claiming the system is genuinely off.

**Impact:** User-facing correctness and trust. Showing "0% / 0 W everywhere" is worse than no data — it implies the battery is fully empty and the house is consuming nothing, neither of which is true.

## Investigation Summary

The dashboard's live readout comes from `StatusResponse.Live`, which the `/status` Lambda builds from the most recent row in `flux-readings`. The poller writes that row every 10 s from `client.GetLastPowerData`.

- `internal/alphaess/client.go:92` — `GetLastPowerData` calls `doGet`, then `json.Unmarshal(data, &result)` into a `PowerData` struct. Crucially, `json.Unmarshal([]byte("null"), &result)` succeeds without error and yields a zero-valued struct. So a `code: 200, data: null` response is indistinguishable from a real all-zero reading at the call site.
- `internal/poller/poller.go:169` — `fetchAndStoreLiveData` only branched on `err`. A zero-valued struct from a `null` payload counted as success, so the poller wrote a `ReadingItem` with every field zero and a freshly stamped `now()` timestamp.
- `internal/api/status.go:78` — handler took `latest := allReadings[len(allReadings)-1]` and populated `resp.Live` regardless of values or age.
- `Flux/Flux/Dashboard/DashboardView.swift:113` — renders whatever `live` contains, including zeros.

The user confirmed the symptom is "everything shows 0, no proper data since 11 PM". That rules out the earlier hypothesis that the latest reading was simply aging (which would have left the *previous* non-zero values visible). The poller is actively writing fresh-timestamped zero rows.

**Hypotheses tested:**

- *AlphaESS API errors silently at night.* Ruled out — would log `fetch live data failed`, leave the previous reading in place, and the dashboard would show non-zero stale values. The observed all-zero pattern is incompatible with this path.
- *AlphaESS returns `code: 200` with no payload (`data: null` or empty).* Confirmed as the most likely explanation. `json.Unmarshal` happily turns either into zero-filled `PowerData`, and the poller writes it.
- *AlphaESS returns a `code: 200` response carrying an explicit all-zero JSON object.* Possible too — same outcome at the write site. Defended against by the same all-zero check below.
- *DynamoDB TTL pruning.* Ruled out — TTL is 30 days on `flux-readings`.
- *iOS app filtering.* Ruled out — the dashboard renders whatever `Live` it receives.

## Discovered Root Cause

Two reinforcing defects:

1. `GetLastPowerData` does not distinguish "no data" from "data with every field zero". `json.Unmarshal` of a JSON `null` (or an empty data field) silently produces a zero-valued struct.
2. `fetchAndStoreLiveData` writes whatever the client returns, with no sanity check on the values. The result is a fresh `ReadingItem` per 10 s with `ppv=0, pload=0, pbat=0, pgrid=0, soc=0` — which `/status` then dutifully surfaces as the current state.

The combined effect: when AlphaESS goes quiet overnight (which the user reports happens reliably from around 11 PM), the dashboard renders 0% / 0 W / 0 W / 0 W as if those are the real current values.

**Defect type:** Silent acceptance of "no data" responses as if they were valid data.

**Why it occurred:** The error path covered HTTP / transport / decode failures, but a 200 response with a missing payload looked like a success at every layer. The pattern is already addressed for `getOneDateEnergyBySn` (`isAllZeroEnergy` in `internal/poller/poller.go`) — that precedent simply wasn't applied to `getLastPowerData`.

## Resolution for the Issue

Three layered changes:

1. **Client refuses null/empty Data.** `GetLastPowerData` checks for a missing or `null` Data field and returns a descriptive error. The poller's existing `slog.Error("fetch live data failed", ...)` path then logs the no-data response and skips the write — the same way it handles transport errors.
2. **Poller refuses all-zero values.** `isAllZeroPower` mirrors the existing `isAllZeroEnergy` pattern. When every field is zero, `fetchAndStoreLiveData` logs a warning that includes the raw payload values and returns without writing. This catches the "code: 200, data: {}" variant where the JSON parses but contains zeros, and it puts the diagnostic information directly into CloudWatch so the actual AlphaESS behaviour can be investigated.
3. **API drops stale `Live`.** `/status` now omits `Live` and the cutoff times derived from it when the most recent stored reading is older than `liveDataStalenessThresholdSec` (90 s — nine missed 10 s writes is unambiguously broken). With (1) and (2) above, the poller stops writing during AlphaESS quiet hours, so the latest reading ages and this gate fires, surfacing the existing "Awaiting live data" UI state.

**Changes made:**

- `internal/alphaess/client.go` — add `isNullJSON` and reject null/empty Data in `GetLastPowerData`.
- `internal/alphaess/client_test.go` — add `TestGetLastPowerData_NullData_ReturnsError` and `TestGetLastPowerData_EmptyData_ReturnsError`.
- `internal/poller/poller.go` — add `isAllZeroPower`; have `fetchAndStoreLiveData` log + skip on all-zero values; include raw values in the warn-level log line for diagnosis.
- `internal/poller/poller_test.go` — add `TestFetchAndStoreLiveData_AllZeroPayload_LogsAndSkips` and `TestFetchAndStoreLiveData_ValidSocZeroPower_Writes` (pins the threshold so legitimately quiet readings with valid SoC still persist).
- `internal/api/status.go` — gate `resp.Live` and the cutoff times on a 90 s freshness threshold.
- `internal/api/status_test.go` — `TestHandleStatusStaleLatestReading_OmitsLive` and `TestHandleStatusStalenessBoundary`.
- `CHANGELOG.md` — note the fix under the next release.

**Approach rationale:** Each layer fixes a distinct root cause and reinforces the others. (1) is the cleanest fix when AlphaESS returns a structurally empty response. (2) catches the "structurally valid but all-zero" variant and produces the diagnostic logging the user asked for ("why can't it collect the live data?"). (3) is defence in depth — if a future API quirk slips past (1) and (2), the dashboard still falls back to a correct "no live data" state instead of presenting an aged reading.

**Alternatives considered:**

- *Only fix the API staleness check (the original PR #48 scope).* Rejected once the user clarified that the dashboard was showing all-zeros — staleness alone couldn't explain that, because the rows were fresh, just bogus.
- *Fall back to the most recent `flux-daily-power` 5-minute snapshot when `getLastPowerData` is bogus.* Considered but deferred: `flux-daily-power` is itself polled hourly and `docs/flux-v1.md` notes its power fields are unreliable on this serial, so it's not a viable real-time substitute. Diagnostic logging from (2) will tell us whether this is worth revisiting.
- *Detect repeated-identical (non-zero) readings.* Rejected — heuristic, false-positives when the system is genuinely steady.

## Regression Tests

**Test files:** `internal/alphaess/client_test.go`, `internal/poller/poller_test.go`, `internal/api/status_test.go`

**Test names:**

- `TestGetLastPowerData_NullData_ReturnsError` — `data: null` becomes a typed error.
- `TestGetLastPowerData_EmptyData_ReturnsError` — missing `data` field becomes a typed error.
- `TestFetchAndStoreLiveData_AllZeroPayload_LogsAndSkips` — every-field-zero values do not get persisted; the warn log mentions the all-zero condition.
- `TestFetchAndStoreLiveData_ValidSocZeroPower_Writes` — a quiet but valid reading (SoC > 0, power fields zero) still persists. Pins the threshold so we don't accidentally suppress real overnight data.
- `TestHandleStatusStaleLatestReading_OmitsLive` and `TestHandleStatusStalenessBoundary/{fresh,at threshold,one past threshold}` — the API drops `Live` when the latest stored reading is older than 90 s.

**Run command:** `go test ./internal/alphaess/ ./internal/poller/ ./internal/api/ -run 'NullData|EmptyData|AllZeroPayload|ValidSocZeroPower|HandleStatusStale|HandleStatusStalenessBoundary' -v`

## Affected Files

| File | Change |
|------|--------|
| `internal/alphaess/client.go` | Reject null/empty Data in `GetLastPowerData`; add `isNullJSON` helper. |
| `internal/alphaess/client_test.go` | Tests for the null/empty paths. |
| `internal/poller/poller.go` | Add `isAllZeroPower`; have `fetchAndStoreLiveData` log + skip on all-zero. |
| `internal/poller/poller_test.go` | All-zero-skip + valid-SoC-writes tests. |
| `internal/api/status.go` | `liveDataStalenessThresholdSec`; suppress `Live` and cutoff times when latest reading is too old. |
| `internal/api/status_test.go` | Staleness + boundary tests. |
| `CHANGELOG.md` | Note the fix under the next release. |
| `specs/bugfixes/late-night-current-data-gap/report.md` | This report. |

## Verification

**Automated:**

- [x] Regression tests pass (`TestHandleStatusStaleLatestReading_OmitsLive`, `TestHandleStatusStalenessBoundary`).
- [x] `internal/api` suite passes; the only pre-existing failure is `internal/config/TestLoad_MissingRequiredVars/AWS_REGION`, caused by the test relying on `t.Setenv` while `AWS_REGION` is exported in the local shell. Unrelated to this fix; reproduced on `main`.
- [x] `make lint` passes (`golangci-lint run` reports 0 issues).
- [x] `go vet ./...` passes.

**Manual verification:**

- Once deployed, the next late-evening / overnight window AlphaESS goes quiet, expect:
  - CloudWatch logs from the poller showing `getLastPowerData: no data in response` and/or `skipping reading write: AlphaESS returned all-zero values` with the raw `ppv/pload/pbat/pgrid/soc` values. These tell us exactly what AlphaESS is sending — confirming whether the issue really is "null payload", "all-zero payload", or both.
  - The iOS Dashboard switches to "Awaiting live data" with a "—" SoC instead of "0% / 0 W everywhere" within ~90 s of the last good reading.
  - When AlphaESS resumes (typically by morning), the next successful poll restores the live readout automatically.

## Prevention

- Any future endpoint that surfaces "current" measurements should validate the underlying response is structurally present (`isNullJSON`) and semantically non-trivial (an `isAllZero*` check) before persisting.
- The new warn-level logs give us per-poll visibility into AlphaESS overnight behaviour. After a couple of nights of telemetry we should know whether `null` or "all-zero object" is the actual response — that informs whether to pursue a 5-min-snapshot fallback for genuine real-time data.
- Consider a follow-up that emits a CloudWatch metric when the poller skips a live write so sustained outages are visible without waiting for a user report.

## Related

- Transit: T-1274
- Branch: `T-1274/bugfix-late-night-current-data-gap`
