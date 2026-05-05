# Decision Log: Low Since Off-Peak

## Decision 1: Use end of last off-peak window as the boundary

**Date**: 2026-05-03
**Status**: superseded by Decision 4

### Context

The Lambda `/status` endpoint exposes `battery.low24h` as the lowest SoC over
the past 24 hours. The window can include lows from before the most recent
off-peak charge, so the displayed value lags after the battery has been
topped up. The fix re-defines the window relative to the off-peak schedule,
but "since the last off-peak" admits multiple boundaries (window start, end,
or some derived reset point).

### Decision

Use the most recent off-peak window **end** time (Sydney local) as the
boundary. When `now` is at or after today's end, use today's end; otherwise
use yesterday's end at the same time-of-day, including when `now` falls
inside today's window.

### Rationale

The off-peak window's purpose is to charge the battery, so its end time is
the natural "fully-charged" reset point. Using the end keeps the metric
intuitive: "lowest SoC since we were last full." Using the boundary even
during the window itself shows the dip happening *during* charging (e.g.,
if load briefly exceeds charge rate), which is informative — the value
then resets cleanly when the window closes.

### Alternatives Considered

- **Off-peak window START**: Resets at the start of charging, so the dip
  during charging is hidden — Rejected because the user wants to see the
  low against the most recent full-charge state, not the pre-charge state.
- **Most recent COMPLETED off-peak day's start**: Inconsistent in the
  morning vs. afternoon and harder to explain — Rejected for ambiguity.

### Consequences

**Positive:**

- Value resets to a meaningful baseline once a day, immediately after the
  off-peak window closes.
- Mirrors the existing `nextOffpeakStart` helper structure, so the code
  stays symmetrical.

**Negative:**

- Returns `null` once the new window opens but contains no readings yet.
  This is a behavior change versus today's "any reading in the past 24h",
  but accepted explicitly.
- Requires an existing test fixture update — the current test asserts a
  value from outside the new window.

---

## Decision 2: Keep wire field name `low24h` despite changed semantics

**Date**: 2026-05-03
**Status**: accepted

### Context

The semantic of `battery.low24h` changes — the name no longer matches the
behaviour. Renaming the JSON tag, Go struct, and Swift type would clean
this up but ripples through ~14 files (model, fixtures, widget timeline,
4 test files, and 2 view labels) on the client side.

### Decision

Keep the JSON tag (`low24h`), Go struct name (`Low24h`), and Swift type
name (`Low24h`). Only the underlying computation changes. The user-visible
UI labels in the dashboard and large widget update from "24h low" to
"Lowest" — short enough to fit existing row widths; the surrounding
context (battery section) makes the meaning clear.

### Rationale

The user explicitly preferred the smaller diff. Both ends are owned by a
single developer; the field name is internal and can be renamed later as a
mechanical sweep if the inconsistency starts to mislead reviewers.

### Alternatives Considered

- **Full rename to `lowSinceOffpeak`**: Most honest naming, but a wider
  diff with no behavioural benefit — Rejected per user direction.
- **Keep field name and UI label both unchanged**: Smallest diff, but the
  visible UI label "24h low" would also be misleading — Rejected because
  the UI label change is two lines and removes a user-visible lie.

### Consequences

**Positive:**

- Smaller diff. No client-side wire-format changes.
- Existing client mocks, fixtures, and decoder tests stay valid.

**Negative:**

- Field name `low24h` no longer reflects what the field measures — a
  latent inconsistency for future readers. Documented in the Go struct
  doc comment to mitigate.

---

## Decision 3: Match existing DST handling, don't fix it here

**Date**: 2026-05-03
**Status**: accepted

### Context

The new `lastOffpeakEnd` helper computes "yesterday at HH:MM Sydney" via
`time.Date` + `AddDate(0, 0, -1)`, the same approach as the existing
`nextOffpeakStart` helper. This pattern can produce surprising results on
DST-transition days in Australia/Sydney (the wall-clock time may not exist
or may exist twice), but the existing helper accepts that behaviour.

### Decision

Match `nextOffpeakStart`'s DST handling exactly. No new DST logic is
introduced and no existing helper is changed.

### Rationale

This is a behavioural change to a single derived field, not a refactor of
off-peak time arithmetic. Forcing a DST fix into this change would expand
scope and risk breaking the cutoff-suppression logic that `nextOffpeakStart`
feeds. If DST handling needs revisiting, do it as one shared helper across
both call sites.

### Alternatives Considered

- **Fix DST handling now**: Cleaner long-term but expands scope and forces
  changes to cutoff suppression — Rejected as out of scope.
- **Add a DST guard only to `lastOffpeakEnd`**: Creates inconsistency
  between the two helpers — Rejected.

### Consequences

**Positive:**

- Smallest possible change. No regression risk on cutoff logic.

**Negative:**

- DST surprises (~twice per year) carry over to `low24h`. Acceptable for
  a personal-scale system; revisit if it ever materially confuses output.

---

## Decision 4: Use Sydney midnight (start of today) as the boundary

**Date**: 2026-05-05
**Status**: accepted (supersedes Decision 1)

### Context

T-1084 redefined `battery.low24h` as the lowest SoC since the most recent
off-peak window end (Decision 1) so the value would reset after each
charging cycle. After living with that behaviour, the simpler "lowest
today" semantic — anchored to the Sydney calendar day — is preferred.
It matches everyday language, drops the off-peak coupling from this code
path, and resets at midnight in step with the date that the rest of the
UI displays.

### Decision

Use the start of the current Sydney-local calendar day (00:00
`Australia/Sydney` on `now`'s date) as the lower bound for the `low24h`
computation. The off-peak window is no longer read in this path.

### Rationale

- "Lowest today" matches how a person naturally talks about the metric;
  no off-peak awareness needed to interpret it.
- Removes the coupling between this field and the SSM off-peak config —
  the field continues to work even if off-peak parameters are missing
  or malformed, eliminating one of the two nil paths.
- Resets at midnight rather than mid-afternoon, which aligns with the
  date shown on the Dashboard and Day Detail screens.
- Lambda's 24h reading query window trivially covers the new bound at
  every moment in the day.

### Alternatives Considered

- **Off-peak window end (Decision 1, current behaviour)**: Resets after
  charging — Rejected on second thought because calendar-day semantics
  are more intuitive and decouple this field from off-peak config.
- **24h rolling (original behaviour)**: Original implementation —
  Rejected for the original staleness reason from T-1084; midnight
  reset inherits that fix without the off-peak coupling.
- **UTC midnight**: Trivially simpler — Rejected because the user (and
  the rest of the system, including the Day Detail date selector and
  off-peak window config) thinks in Sydney local time.
- **Most recent SoC=100 reading (true "since last full")**: Closest to
  literal "since charged" — Rejected because the battery rarely hits
  100, so the boundary would jitter and sometimes fall back to days
  earlier; "today" is more predictable.

### Consequences

**Positive:**

- Simpler boundary; no off-peak parsing in this path.
- Single nil path remains (no readings since midnight), down from two.
- `lastOffpeakEnd` helper and its test are deleted; only
  `nextOffpeakStart` remains for cutoff-suppression logic.
- Removes a hidden coupling between off-peak config validity and an
  unrelated battery metric.

**Negative:**

- Brief nil window each day between Sydney midnight and the first
  reading after it (~10s with current polling). Acceptable; UI already
  renders `—`.
- Field name `low24h` is now doubly stale (not 24h, not since-off-peak,
  not literally anything in particular). Decision 2 still applies — wire
  rename is out of scope.
- Implementation churn on a recently-shipped feature: a helper, its
  test, and the unparseable-config test are removed or replaced two
  weeks after they landed.

### Impact

- `internal/api/compute.go` — `lastOffpeakEnd` removed; `startOfDaySydney`
  added.
- `internal/api/status.go` — off-peak boundary replaced with midnight;
  off-peak config no longer read in this code path.
- `internal/api/compute_test.go` — `TestLastOffpeakEnd` replaced with
  `TestStartOfDaySydney`.
- `internal/api/status_test.go` — `TestHandleStatusAllDataPresent`
  re-anchored; `TestHandleStatusLow24hUnparseableOffpeak` removed; new
  `TestHandleStatusLow24hNoReadingsToday` added.
- `internal/api/response.go` — `Low24h` doc comment updated.
- No Swift changes — UI label "Lowest" already accurate; wire format
  byte-identical.

---
