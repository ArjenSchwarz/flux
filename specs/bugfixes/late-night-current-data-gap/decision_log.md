# Decision Log: Late-night current-data gap (T-1274)

## Decision 1: 90-second staleness threshold for `Live`

**Date**: 2026-05-19
**Status**: accepted

### Context

`/status` now drops `live` (and the cutoff times derived from it) when the most recent stored reading in `flux-readings` is older than a threshold. The threshold trades off two failure modes:

- **Too tight** (e.g. 30 s): the dashboard flicker-falls-back to "Awaiting live data" during routine transient blips — a single missed poll cycle plus Lambda cold-start jitter is enough.
- **Too loose** (e.g. 5 min): a sustained outage continues to show aged numbers for longer before the UI tells the user something is wrong.

The poller's normal cadence is `livePollInterval = 10 * time.Second` (`internal/poller/poller.go:17`). Without this gate the user saw aged readings for hours during the AlphaESS overnight outage.

### Decision

Set `liveDataStalenessThreshold = 90 * time.Second` in `internal/api/status.go`.

### Rationale

90 s = nine consecutive missed 10 s writes. Network or AlphaESS hiccups occasionally cost one or two polls; nine in a row is unambiguously an outage. The corresponding "Awaiting live data" state surfaces within a couple of dashboard refresh cycles (10 s each), so the user notices quickly but isn't bothered by transient flicker. The constant is `time.Duration` to match every other timing constant in the codebase (`livePollInterval`, `dailyPowerInterval`, etc.).

### Alternatives Considered

- **30 s threshold**: faster signal — Rejected because a single AlphaESS slow response (10 s timeout + retry) can push past 30 s on a healthy night. Too noisy.
- **5-minute threshold**: matches typical "stale" intuition elsewhere in the app — Rejected because hours of bogus zeros until the gate fires is the exact failure mode this fix exists to prevent.
- **Use `live.timestamp` client-side and compute staleness in each app** — Rejected because three clients (iOS Dashboard, macOS Dashboard, widgets) would each need to reimplement the same logic, and the existing widget `StalenessClassifier` already uses a different definition ("when was the *fetch*?", not "when was the reading?"). Centralising in `/status` keeps a single source of truth.

### Consequences

**Positive:**
- Dashboard truthfully reports "Awaiting live data" instead of presenting hours-old numbers as current.
- No client changes needed — the existing nil-handling path renders the correct UI.

**Negative:**
- A brief AlphaESS hiccup of 90–120 s causes a transient "Awaiting live data" state during which the user sees no values. Acceptable trade-off versus showing stale data.
- The threshold is a tuning knob that may need revisiting if AlphaESS behaviour shifts.

---

## Decision 2: Backfill deletes every-field-zero rows on the assumption they are non-recoverable bogus data

**Date**: 2026-05-19
**Status**: accepted

### Context

`cmd/backfill-readings` queries each day's existing `flux-readings` rows, identifies the all-zero ones via `dynamo.IsAllZeroReading`, deletes them, and writes synthetic 5-minute rows derived from `getOneDayPowerBySn`. The deletion step is unconditional within the date window — any row whose `ppv`, `pload`, `pbat`, `pgrid`, and `soc` are all exactly zero is treated as the AlphaESS-overnight-outage sentinel and removed.

### Decision

Treat every-field-zero (including `soc == 0`) as the bogus pattern and delete unconditionally.

### Rationale

A functioning battery system that has been running cannot legitimately produce a reading where every field is exactly zero. `Ppv = 0` is normal at night; `Pload = 0` is implausible (a household always has standby draw); `Pbat = 0` is plausible at idle; `Pgrid = 0` is plausible; but `Soc = 0` is impossible in normal operation because the AlphaESS inverter is configured to stop discharge at 5%. The only way every field lands on exactly zero is the unmarshal-from-null-payload path the bug introduced. The same pattern is already used for the daily-energy table (`isAllZeroEnergy` in `internal/poller/poller.go`) on the same logic.

### Alternatives Considered

- **Require `soc > 0` AND at least one power field non-zero before considering a row valid**: Equivalent in practice — every-field-zero implies `soc == 0`, and a real `soc > 0` with all-zero power fields is the "idle but valid" pattern we explicitly preserve. The current check is the simpler form of the same idea.
- **Use a "last-good before the run" timestamp window instead of a value-based check**: Rejected because there's no reliable signal in the table for "when did the outage start?". A value-based check works on any future occurrence without needing operator input.
- **Leave the rows in place and let the readers compensate** (e.g. teach `MinSOC` to skip `soc == 0`): Rejected because the corruption is in raw data and is better fixed at source; downstream patches multiply.

### Consequences

**Positive:**
- Idempotent: re-running the backfill is safe. Synthetic rows always pass `IsAllZeroReading == false` because their `Soc` (from `cbat`) is non-zero.
- Self-targeting: only the bogus rows are removed; legitimately quiet readings (zero power but real SoC) are untouched (`TestFetchAndStoreLiveData_ValidSocZeroPower_Writes` pins this).

**Negative:**
- If a future failure mode produces a legitimately every-field-zero state (e.g. a system that has just been powered off, polled at the instant of shutdown), the tool would delete that too. Considered acceptable because the synthetic replacement from `getOneDayPowerBySn` would reflect the same idle state, and a real every-field-zero reading carries no information worth preserving.
- Couples the helper's semantics to "AlphaESS no-data sentinel"; rename or repurpose would risk callers using it where a real all-zero state matters.

---
