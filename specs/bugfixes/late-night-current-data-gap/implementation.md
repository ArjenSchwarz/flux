# Implementation Explanation: Late-night current-data gap (T-1274)

Branch: `T-1274/bugfix-late-night-current-data-gap` (4 commits ahead of `origin/main`).

## Beginner Level

### What Changed / What This Does

Flux's iOS Dashboard shows what the home battery is doing right now — solar coming in, household load, grid in/out, and battery percentage. Around 11 PM every night the dashboard was claiming everything was zero: 0% battery, 0 watts everywhere. By morning it would recover.

Three things were wrong, fixed in layers:

1. **AlphaESS (the battery's cloud API) sometimes sends an "empty" response overnight.** Our code used to treat that empty response as if it said "everything is zero" and store that as a real reading. Now we recognise the empty response as no-data and skip it.
2. **Even if AlphaESS sends a structurally-valid response where every number happens to be zero, that pattern can't happen for a working battery** (a running home always uses *some* power, and the battery can't legitimately be at 0% because it stops discharging at 5%). We now refuse to store those rows too, and log what we received so we can see in our logs what the upstream is actually sending.
3. **The dashboard now tells you when the data is too old to be live.** If the most recent reading is more than 90 seconds old, the dashboard switches to "Awaiting live data" instead of showing the stale values as if they were current.

There's also a one-off repair tool that fixes the bad rows from last night's outage — it reads what AlphaESS *can* tell us about that period (5-minute historical snapshots) and writes correct values back in place of the zeros.

### Why It Matters

The dashboard is the one part of the app you check expecting a current answer. Showing "0% battery, nothing happening" when actually the battery is at 65% and discharging is worse than showing nothing — it implies you're about to lose power, or that the system has died, when neither is true. Trust in the app drops as soon as it's seen lying once.

### Key Concepts

- **AlphaESS API** — the manufacturer's cloud service. Our backend asks it "what is the battery doing now?" every 10 seconds.
- **Poller** — the Go program that runs in AWS and does that asking on a schedule, then writes the answers to DynamoDB.
- **`flux-readings`** — the DynamoDB table that holds those 10-second-resolution rows. Each row has solar power, house load, battery power, grid power, state of charge, and a timestamp.
- **`/status` endpoint** — the Lambda the iOS app calls every 10 seconds to fetch the current dashboard data. It reads the most recent row from `flux-readings` and serves it back.
- **`getOneDayPowerBySn`** — a different AlphaESS endpoint that returns 5-minute historical snapshots for a whole day. Slower-resolution but more reliable overnight, which is why the repair tool uses it.

---

## Intermediate Level

### Changes Overview

Eight production-code files plus tests and docs:

| File | Role |
|------|------|
| `internal/alphaess/client.go` | Layer 1: `GetLastPowerData` checks the raw `json.RawMessage` for `null` or empty before unmarshalling and returns a typed error if so. |
| `internal/alphaess/models.go` | New `DerivePower(load, ppv, gridCharge, feedIn) (pgrid, pbat)` helper centralising the sign convention used wherever we go from 5-minute snapshot fields to live-reading shape. |
| `internal/poller/poller.go` | Layer 2: `isAllZeroPower` mirrors the existing `isAllZeroEnergy`. `fetchAndStoreLiveData` logs `ppv/pload/pbat/pgrid/soc` at warn level and skips the DynamoDB write when every field is zero. |
| `internal/api/status.go` | Layer 3: precomputes `liveFresh` (latest row exists and is within `liveDataStalenessThreshold = 90 * time.Second`). Gates `resp.Live`, `battery.EstimatedCutoff`, and `rolling15min.EstimatedCutoff` on it. |
| `internal/api/day.go` | `mapDailyPowerToPoints` now calls `alphaess.DerivePower` instead of inlining the pgrid/pbat formulas. |
| `internal/dynamo/models.go` | Layer 4 support: `NewReadingItemFromSnapshot` (snapshot → `ReadingItem`, TTL from `now` not snapshot time) and `IsAllZeroReading` (bogus-row detector for the backfill). |
| `cmd/backfill-readings/main.go` | Layer 4: per Sydney date, queries existing readings, deletes the `IsAllZeroReading` rows via `BatchWriteItem` DeleteRequest, fetches `getOneDayPowerBySn`, writes synthetic 5-minute `ReadingItem`s via `BatchWriteItem` PutRequest. Has `--dry-run` and a per-day plan log. |

Plus paired test files for each, an updated `CHANGELOG.md`, the bugfix report and decision log at `specs/bugfixes/late-night-current-data-gap/`, and refreshed agent notes for the poller, API, and dynamo layers.

### Implementation Approach

The defect was a layered failure of trust: the client trusted AlphaESS, the poller trusted the client, the API trusted the poller. The fix is layered defence: each layer now refuses the bad data and keeps the corruption from propagating downward.

- **At the client layer** (Layer 1) the failure mode is structural — AlphaESS sends `data: null` or omits the field. A simple `bytes.TrimSpace == "null"` / `len == 0` check on the raw `json.RawMessage` catches it before the unmarshal silently produces zeros. The check is in `GetLastPowerData` only; other endpoints have their own null-handling needs and are scoped separately.
- **At the poller layer** (Layer 2) the failure mode is semantic — AlphaESS sends a structurally-valid response with all zeros. The check exactly mirrors `isAllZeroEnergy`, which already exists for the daily-energy endpoint, so the pattern is familiar to anyone working on the poller. The structured log includes the raw values so a future investigator can confirm exactly what AlphaESS was sending without needing to enable a debug flag.
- **At the API layer** (Layer 3) the gate is time-based and defence-in-depth: if some new failure mode slips past layers 1 and 2 and a zero (or merely aged) row ends up as the latest reading, `/status` still refuses to surface it as live once it's older than 90 seconds. The constant uses `time.Duration` to match every other timing constant in the codebase. The same `liveFresh` boolean gates `Live`, `battery.EstimatedCutoff`, and `rolling15min.EstimatedCutoff` — every field that's derived from the latest reading's instantaneous state. `low24h` and the rolling averages themselves continue to derive from their own time-windowed reading subsets and remain correct when the latest is stale.
- **The backfill tool** (Layer 4) is a separate concern: forward fixes prevent new corruption but the rows already in `flux-readings` from last night's outage still need replacing. The tool reuses the same field mapping the past-date Day Detail chart fallback uses (`mapDailyPowerToPoints`), so the synthetic readings are guaranteed to look like the existing chart's interpretation. The mapping was inlined in two places; this PR extracts it into `alphaess.DerivePower` so the sign convention can only drift in one location.

### Trade-offs

- **Server-side staleness gate vs client-side `live.timestamp` check** — `live.timestamp` is already in the wire response but no client reads it. Doing the check on the server means iOS, macOS, and widgets all get the right behaviour from a single line of Go.
- **Drop `Live` entirely vs add a `live.stale` flag** — dropping is a smaller wire-format change and the iOS Dashboard already handles `Live == nil` with the "Awaiting live data" UI. Adding a flag would require client updates with no UX benefit.
- **Real-time fallback to `getOneDayPowerBySn` when live is stale** — considered and deferred. The snapshot data has the right shape, but it's only refreshed hourly by the poller, so on-demand fetches would burn the AlphaESS API budget and a cached fallback would be up to an hour stale. The "Awaiting live data" state is more honest for a dashboard people open expecting current numbers.
- **Generic `isAllZero` over an interface vs three concrete functions** — three concrete one-liners on three different types in three packages is cleaner than introducing a `Zeroable` interface with five method receivers across the codebase.
- **CLI talks to DynamoDB directly vs going through the `Store` interface** — going through `Store` would require adding `WriteReadings` (plural) and `DeleteReadings` to the interface for a single one-off caller, and the `LogStore` dry-run would replace the current per-day summary log with per-item noise. The CLI uses the raw client to keep the production interface narrow.

---

## Expert Level

### Technical Deep Dive

**Client-layer null detection** (`internal/alphaess/client.go`). The AlphaESS envelope is `{code, msg, data: json.RawMessage}` and `doGet` already gates on `code` ∈ {0, 200}. The new check is on the raw `data` bytes: `len(data) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null"))`. The TrimSpace handles any flavour of whitespace AlphaESS might wrap around the null literal; `len == 0` covers the case where the field is omitted from the envelope entirely (which `json.RawMessage` represents as a nil slice rather than the JSON literal `null`). The check is only applied to `GetLastPowerData` because that's the endpoint whose silent-zero failure mode this PR is closing; other endpoints have endpoint-specific empty-data interpretations (e.g. `getOneDayPowerBySn` legitimately returns `[]` for a date with no data) and are not in scope.

**Poller-layer all-zero detection** (`internal/poller/poller.go`). `isAllZeroPower` is byte-identical in shape to `isAllZeroEnergy`: a single `return d.Ppv == 0 && d.Pload == 0 && d.Pbat == 0 && d.Pgrid == 0 && d.Soc == 0` against the live PowerData type. The semantically interesting bit is the warn log immediately upstream: `slog.Warn("skipping reading write: AlphaESS returned all-zero values (inverter likely not reporting)", "sysSn", …, "ppv", …, "pload", …, "pbat", …, "pgrid", …, "soc", …)`. This is the actionable diagnostic — in a steady-state outage CloudWatch shows the raw payload values per 10 s poll, so a future investigator can confirm without instrumentation whether AlphaESS is returning `null` (layer 1 path) or a structurally-valid all-zero object (layer 2 path). On the happy path the check costs one short-circuited float comparison.

**API-layer staleness gate** (`internal/api/status.go`). `liveDataStalenessThreshold = 90 * time.Second` is chosen as nine consecutive missed 10 s poll cycles — tight enough that a sustained outage flips the UI promptly (within two dashboard refresh cycles) but loose enough to absorb a single AlphaESS slow-response (10 s timeout) plus retry. The gate is computed once as `liveFresh := len(allReadings) > 0 && nowUnix - latest.Timestamp <= int64(threshold.Seconds())` and reused at three sites. Note that the threshold gates only the instantaneous-state fields (`Live`, both `EstimatedCutoffTime`s). `Battery.Low24h` and `Rolling15m.AvgLoad`/`AvgPbat` continue to derive from their own time-windowed reading subsets (`sinceMidnight` and `fifteenMinReadings`), which already produce correct results — empty when no readings fall in the window, populated when they do, independent of the latest reading's age.

**Backfill semantics** (`cmd/backfill-readings/main.go`). The four-step per-day pipeline is intentionally sequential within a day (the query informs which deletes are needed, AlphaESS gates the writes) but trivially parallelisable across days. We don't parallelise because the dominant time is AlphaESS's network round-trip, and the 500 ms inter-day delay is an explicit politeness pace against the upstream's rate limiter. The TTL semantics on synthetic readings deserve attention: `NewReadingItemFromSnapshot` derives the TTL from `now`, not from the snapshot's `uploadTime`, so a backfilled row written today for a snapshot from two days ago gets the full 30-day TTL window starting now. The alternative (TTL from snapshot time) would have older backfilled rows expire almost immediately and leave gaps re-emerge.

**Shared sign convention** (`internal/alphaess/models.go::DerivePower`). The function takes raw float64s rather than a `PowerSnapshot` parameter so it can be called from both the `alphaess.PowerSnapshot` consumer (`NewReadingItemFromSnapshot` in `internal/dynamo`) and the `dynamo.DailyPowerItem` consumer (`mapDailyPowerToPoints` in `internal/api`) without forcing either type into a shared interface. The sign convention is asserted by the test matrix: importing (positive pgrid, positive pbat), exporting (negative both), idle (zeros), and the diagonal case where solar exactly covers load.

### Architecture Impact

- **The `Store` interface stays narrow.** The backfill CLI deliberately uses the raw `*dynamodb.Client` rather than going through `dynamo.Store`. Production `Store` is consumed by the poller and Lambda; widening it for a one-off repair tool would couple two unrelated lifecycles. The `*DynamoStore` concrete type's batch helpers (`WriteDailyPower`) remain the canonical example to follow if a future tool wants to share them.
- **`alphaess.DerivePower` is now a public choke point for the snapshot-to-reading sign convention.** Any future change to how Flux interprets `feedIn`/`gridCharge` orientation has exactly one place to land. Both downstream call sites (past-date Day Detail and the readings backfill) automatically pick up the change.
- **The `liveFresh` boolean is a deliberately local concept** scoped to `handleStatus`. It is not exposed as part of the wire response, because the UX contract is "either fresh live data or none" rather than "stale-tagged data". If a future client needs to distinguish "no data ever" from "data, but old", that's an API surface change worth its own discussion — currently both paths produce `Live: null`.
- **Warn-level structured logs in the live poll path** are new. Previously the only thing the poller emitted per 10 s was an info-level "stored reading". The warn path fires only on the bad-data branch, but in a sustained AlphaESS outage that's still ~6 warns/minute, ~360/hour. CloudWatch ingestion cost is negligible at this scale; future alerting on the metric will need to handle the burstiness.

### Potential Issues

- **The 90 s threshold is the most likely future-tuning point.** If AlphaESS introduces a higher-latency response mode (e.g. a 60-second-resolution endpoint variant), the threshold will need to grow with it. Conversely, if the poll cadence drops to 5 s for some reason, the threshold should follow.
- **`getOneDayPowerBySn` for today only returns snapshots up to the most recent 5-minute boundary.** The backfill's synthetic readings for "today" therefore stop at, e.g., 10:25 when run at 10:30. The poller's live writes (when AlphaESS recovers) fill in from there with their own 10 s-cadence timestamps. There is no conflict — the two carry different timestamps — but the Day Detail downsample will produce a 5-minute bucket containing both 1 synthetic and ~30 live readings, weighted equally. The synthetic reading's 1/31 weight is small enough that the visual smoothing is invisible, but it's worth knowing if a future refinement wants to drop synthetic readings once a real reading covers the same bucket.
- **`IsAllZeroReading` includes `Soc == 0` as a required condition.** This depends on the AlphaESS inverter being configured with a non-zero minimum discharge (currently 5%). If that configuration is ever changed to allow discharge to 0%, the helper would incorrectly delete real readings taken at the moment the battery genuinely sat at 0% SoC. Decision 2 of the decision log captures this assumption.
- **The diagnostic warns are observability, not alarms.** No CloudWatch metric is emitted for "live write skipped". If sustained AlphaESS outages become a recurring concern (instead of the apparent one-off), a follow-up should add a `LiveWriteSkipped` count metric so the existing `SummarisationPassResult` pattern can be extended to "live poll health".
- **The backfill is idempotent on outage days but cumulative on healthy days.** Re-running on the same outage day deletes the previously-written synthetic rows and re-writes them (same `uploadTime`-derived timestamps, so the BatchWriteItem upserts cleanly). Running on a healthy day adds synthetic 5-minute readings alongside the existing 10-second live readings — not wrong, but redundant. The CLI defaults to the trailing 3 days; widening the window is a deliberate choice the operator makes per invocation.
