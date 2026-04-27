# Decision Log: Evening / Night Stats

## Decision 1: Compute on the backend, not in the iOS app

**Date**: 2026-04-26
**Status**: accepted

### Context

The ticket (T-1018) explicitly notes that the data is already in raw readings, and the work could be done either with a small `/day` change or purely in the iOS app. Choosing where to compute affects deployability, test surface, and future reuse (widgets, web).

### Decision

Add a new `eveningNight` field to the `/day` API response and compute it in the Lambda handler, mirroring the existing `peakPeriods` pattern.

### Rationale

The codebase already has the infrastructure for this (`compute.go` with pure functions, table-driven tests, no new DynamoDB queries). Centralising the logic keeps a single source of truth, lets us reuse the existing `roundEnergy`/`roundPower` helpers, and means a future widget or web client gets the same numbers for free. The marginal cost of a Lambda redeploy is small.

### Alternatives Considered

- **Pure iOS computation from `parsedReadings`**: Faster to ship, no Lambda redeploy. Rejected because the logic would be duplicated if any other client (widget, web) needed it later, and Swift testing is more painful than the table-driven Go tests we already have.
- **Hybrid — backend exposes raw sunrise/sunset timestamps, iOS does totals**: Splits the logic awkwardly and gives the worst of both worlds.

### Consequences

**Positive:**
- Matches existing peak-usage-periods architecture, so reviewers and future-self find it where they expect.
- Testable in isolation with the existing `compute_test.go` patterns and benchmarks.
- iOS app stays thin.

**Negative:**
- Requires a coordinated deploy (Lambda + iOS app), but this is a single-developer project so coordination cost is near zero.

---

## Decision 2: Detect sunset/sunrise from `ppv > 0` readings, fall back to computed sunrise/sunset per block

**Date**: 2026-04-26
**Status**: accepted

### Context

"Evening" and "Night" are bounded by when the sun stops/starts producing measurable solar power. We need a definition that's testable and behaves sensibly on overcast days, partial-data days (e.g. recorder dies mid-day), and days with intermittent solar.

### Decision

Use the timestamp of the latest reading with `ppv > 0` on the requested date as the evening start, and the earliest such reading as the night end. If a given block has no qualifying reading, fall back **per block** to the computed sunset (for evening start) or sunrise (for night end), labelling that block with `boundarySource = "estimated"`. The "sunrise/sunset" definition used is the standard astronomical one (sun's upper limb at the horizon, ≈ 90.833° solar zenith) — not civil twilight (-6°) — because solar generation effectively ends at sunset, not at civil dusk, which is ~25 minutes later at Melbourne's latitude.

### Rationale

Matches the ticket wording ("when no more solar is *recorded*" / "starting to get solar again") for normal days. Per-block fallback handles the asymmetric cases the original whole-day rule missed — e.g. a recorder that dies at 13:00 has morning readings but no afternoon ones, so the evening boundary needs the fallback while the night boundary uses real readings. Naming the algorithm `sunrise/sunset` (zenith ≈ 90.833°) rather than `civil twilight` (-6°) avoids a 25-minute systematic offset that doesn't correspond to physical solar generation. A magic threshold (e.g. `ppv > 50W`) was rejected because there's no principled value and any noise floor below that is already negligible for usage totals; the user accepts that a single anomalous `ppv > 0` reading at 03:00 would shift the boundary as documented behaviour rather than a bug.

### Alternatives Considered

- **Fixed threshold (`ppv > 50W`)**: Filters tiny noise but introduces an arbitrary number; rejected.
- **Whole-day fallback (only when no readings have `ppv > 0` all day)**: Simpler condition, but produces wrong evening boundaries on partial-data days; rejected.
- **Civil twilight (-6°) as the fallback definition**: Conventional astronomical term but ~25 minutes after physical sunset, so it would systematically extend "evening" past when generation actually ends.
- **Computed sun as primary source (no readings used)**: Loses the ticket's stated intent of "when solar is *recorded*".
- **Hide the card entirely on overcast days**: User explicitly preferred a fallback over hiding.

### Consequences

**Positive:**
- Default behaviour matches ticket wording exactly.
- Per-block fallback handles partial-data days correctly without contortion.
- Sunset (-0.833°) aligns with physical generation cutoff better than civil dusk would.

**Negative:**
- Two code paths to test per block (readings-derived and computed-sun).
- A small static lookup table (`internal/api/melbourne_sun_table.go`, 366 entries) is added to the repo. See [Decision 9](#decision-9-embedded-static-lookup-table-for-melbourne-sunrisesunset).
- Very-low-`ppv` noise on a single reading at an unusual hour will shift the boundary; this is documented as accepted behaviour rather than guarded against.

---

## Decision 8: Today's evening block requires astronomical sunset, not just `lastPpvPositive`

**Date**: 2026-04-26
**Status**: accepted

### Context

During design review the algorithm walked through "today, midday, sun still up" and showed that using `lastPpvPositive` as evening start unconditionally would emit a 5-minute "evening" block from 12:55 → 13:00 — clearly wrong. Requirements 1.3 and 1.5 don't distinguish "the sun has actually set" from "we have a recent positive reading", so the algorithm needs a gate.

### Decision

For today's date specifically, the evening block is omitted entirely when `now <= melbourneSunriseSunset(today, isSunrise=false)`. Past dates use `lastPpvPositive` directly per requirement 1.3. Past-date weirdness (e.g. recorder dying at noon → evening start = 12:55) is accepted per Decision 2.

### Rationale

The astronomical sunset is the cleanest signal that "evening has begun"; using `lastPpvPositive` alone has no way to distinguish "a recent positive reading mid-day" from "the actual sunset transition". Adding a tolerance window (e.g. `lastPpvPositive within 2h of computed sunset`) introduces a magic number with no principled value. Gating on the astronomical sunset is parameter-free and trivially testable.

### Alternatives Considered

- **Use `max(lastPpvPositive, estimatedset)`**: Picks the later of the two. On normal days `lastPpvPositive ≈ estimatedset - few minutes`, so this would mostly use `estimatedset`, defeating the "use measured boundary when available" intent.
- **Time-window tolerance** (`lastPpvPositive within 2h of estimatedset`): magic number; rejected.
- **N consecutive zero-ppv readings test**: harder to specify and test; rejected.

### Consequences

**Positive:**
- Today's midday view shows only the Night block; evening appears at the moment of astronomical sunset, with `start = lastPpvPositive` if available (or `estimatedset` otherwise), and grows in-progress until midnight.
- Past dates retain the simpler "use last positive reading" rule.

**Negative:**
- One extra `melbourneSunriseSunset` call per request, plus an extra branch in the algorithm. Negligible cost.

---

## Decision 9: Embedded static lookup table for Melbourne sunrise/sunset

**Date**: 2026-04-26
**Status**: accepted (supersedes an earlier draft that proposed an NOAA closed-form computation)

### Context

The fallback sunrise/sunset is only used on rare days where no `ppv > 0` reading exists (heavy overcast, sensor outage). The first draft of the design specified a closed-form NOAA solar-position algorithm (~50 lines of trigonometry with multiple sign/unit conventions to get right). On review the user asked whether something simpler would work for what is essentially a fallback shown a handful of days per year.

### Decision

Replace the NOAA closed form with an embedded static lookup table keyed by `MM-DD` containing pre-computed Melbourne sunrise/sunset wall-clock times. The table is generated once from any astronomical calculator and committed as `internal/api/melbourne_sun_table.go`. Lookup uses `time.ParseInLocation("2006-01-02", date, sydneyTZ)` plus the table's `HH:MM` offset, which yields the correct UTC instant on any given calendar date — DST-immune because the IANA database resolves AEST vs AEDT for us.

### Rationale

For a fixed location, sunrise/sunset times in local clock form vary by less than 1 minute year-over-year on the same calendar date — well inside the ±2-minute tolerance ([req 1.12](#)). A single year's data is good for at least a decade. Storing local clock times (not raw UTC) and converting via `time.ParseInLocation` makes the table both human-readable and DST-immune: future Australian DST rule changes are absorbed by the IANA database that ships with Go releases, with no table refresh required. The fallback path triggers on at most a handful of days per year, so the implementation cost should match the impact.

### Alternatives Considered

- **NOAA closed-form computation** (the previous draft): ~50 lines of trig with longitude sign, JD epoch, EOT formula, and zenith-angle conversions all needing to be exact. Overkill for a rarely-hit fallback.
- **External API call** (e.g. `api.sunrise-sunset.org`): adds a runtime network dependency from Lambda for what should be a self-contained computation; rejected.
- **Third-party library** (e.g. `github.com/nathan-osman/go-sunrise`): one more dependency to track for math that is essentially static; rejected on dependency-cost grounds.
- **Monthly approximation table** (12 entries): simpler still, but ±15 min precision is well outside the ±2-minute target; rejected.

### Consequences

**Positive:**
- Trivial runtime implementation — a map lookup plus `time.ParseInLocation`.
- The table *is* the source of truth; no NOAA-vs-implementation divergence to debug.
- DST-immune by construction.
- Decade-plus stability without table refresh.

**Negative:**
- A 366-line generated `.go` file in the repo. Visible in diffs but mechanical and stable.
- Generation tool not committed; a code comment records the source. If the table ever needs regeneration, the engineer running it must use a Melbourne astronomical calculator (any reputable one — the cross-source variance is well inside ±2 min).

---

## Decision 7: Reuse `Australia/Sydney` time zone for Melbourne user

**Date**: 2026-04-26
**Status**: accepted

### Context

The codebase already uses `Australia/Sydney` (`sydneyTZ`) for all local-time conversions. The user is in Melbourne and asked for hardcoded Melbourne coordinates for the sunrise/sunset fallback. Melbourne's IANA zone is `Australia/Melbourne` but it shares the same UTC offset (and DST schedule) as Sydney year-round.

### Decision

Continue using `sydneyTZ` for all wall-clock conversions in this feature; only the lat/long constants for the sunrise/sunset computation are Melbourne-specific.

### Rationale

The two zones produce identical wall-clock times for any instant. Introducing a parallel `melbourneTZ` would add ceremony for zero observable behaviour change. The lat/long is the only piece that needs Melbourne specificity, and the sunrise/sunset algorithm uses it directly without going through a time zone.

### Alternatives Considered

- **Add a new `melbourneTZ` constant**: Pure ceremony for this codebase; rejected.
- **Switch the whole project from Sydney to Melbourne**: Out of scope and produces no observable difference.

### Consequences

**Positive:**
- No new time-zone surface area; existing tests and helpers continue to work unchanged.

**Negative:**
- A future relocation that crosses a state line (e.g. to Adelaide) would need a real time-zone refactor — but that's hypothetical and out of scope.

---

## Decision 3: Hardcode Melbourne coordinates rather than configure them

**Date**: 2026-04-26
**Status**: accepted

### Context

Civil sunset/sunrise computation needs latitude and longitude. The codebase already hardcodes the Sydney IANA time zone for Melbourne-equivalent UTC offset. Adding a configurable lat/long would require new SSM parameters, handler wiring, and zero benefit for a single-site personal app.

### Decision

Hardcode Melbourne coordinates (latitude ≈ -37.81°, longitude ≈ 144.96°) as Go constants in the same file as the civil-twilight function.

### Rationale

This is a single-site personal app. The lat/long never changes. Putting it in SSM would just be ceremony for the same effect.

### Alternatives Considered

- **SSM parameters `/flux/lat` and `/flux/lng`**: Configurable, but unnecessary; rejected.
- **Derive from AlphaESS system data**: Not currently exposed by anything we already query.

### Consequences

**Positive:**
- Simplest possible implementation; no infra change.

**Negative:**
- If the system ever moves, two constants need updating. Acceptable.

---

## Decision 4: Show the card on any day, mirror peak-usage-card visibility

**Date**: 2026-04-26
**Status**: accepted

### Context

The ticket says "today view", but the only daily view in the iOS app is `DayDetailView`, which navigates between any past date and today. Restricting the card to today only would create asymmetry.

### Decision

Render the card on any day where the response contains the `eveningNight` object, using the same visibility guard pattern as `PeakUsageCard` (hide on fallback-only data, hide when payload is absent).

### Rationale

Past days are useful for retrospective analysis (e.g. "did last night's heat pump cycle cost a lot?"). The "today view" wording in the ticket reflects the user's primary concern, not a strict UI restriction.

### Alternatives Considered

- **Today only**: Strictly literal but pointlessly restrictive.

### Consequences

**Positive:**
- One code path, consistent with PeakUsageCard.
- Past-day analysis is enabled.

**Negative:**
- None notable.

---

## Decision 5: Show in-progress periods with a status indicator

**Date**: 2026-04-26
**Status**: accepted

### Context

When viewing today's date, one or both no-solar periods may still be in progress (Night before sunrise, Evening after sunset). We need to decide whether to show partial totals or hide periods until they complete.

### Decision

Show whatever data is available; mark partial blocks with `status = "in-progress"` and surface an indicator in the iOS card matching the existing offpeak pending visual.

### Rationale

The dashboard already shows in-progress offpeak data using a "pending" status; the same pattern applies here. Hiding partial data wastes the user's primary use case (checking "today, so far"). Omitting a block that hasn't begun (e.g. night when it's still afternoon) keeps the card from showing nonsense.

### Alternatives Considered

- **Hide periods until complete**: Wastes today's value.
- **Show running totals with no indicator**: User has to mentally track whether the period is over.

### Consequences

**Positive:**
- Today's "so far" view is informative.
- Pattern matches existing offpeak status field.

**Negative:**
- One more state on the iOS card to render and test.

---

## Decision 6: Use elapsed-hours for the average, not reading-density mean

**Date**: 2026-04-26
**Status**: accepted

### Context

"Average per hour" can mean either `totalKwh / wallClockHours` or the mean instantaneous `pload` across readings. The two diverge when readings are sparse (gaps > 60s).

### Decision

Compute `averageKwhPerHour = totalKwh / elapsedHours`, where `elapsedHours` is the wall-clock duration of the period.

### Rationale

This is what users intuitively expect when they read "average kWh/h". It also stays consistent across complete and in-progress periods (in-progress just uses the elapsed window so far). Using the reading-density mean would make sparse-data periods look artificially flat.

### Alternatives Considered

- **Mean of `pload` across readings (W → kW)**: Closer to instantaneous-power language; rejected as misleading.

### Consequences

**Positive:**
- One simple, intuitive number.

**Negative:**
- Periods with reading gaps will under-report `totalKwh` and therefore the average; this is true of any integration approach and matches the existing `computeTodayEnergy` behaviour.

---

## Decision 10: Filter middle-of-the-night Ppv blips when picking the night-end boundary

**Date**: 2026-04-27
**Status**: accepted

### Context

Post-merge of T-1018, a sensor produced a tiny `Ppv > 0` reading at ~01:30 in production. The original `findEveningNight` used the first `Ppv > 0` reading on the date as the night-end boundary, so the Night block ended at 01:30 — clearly nonsense (the sun is hours below the horizon at 01:30 in Melbourne).

The symmetric problem on the evening side has not been observed: solar production drops to zero well before sunset, so a stray late-night `Ppv > 0` is not a credible last-of-day reading the same way an early-morning blip is a credible first-of-day reading.

### Decision

When scanning for `Ppv > 0` readings, ignore any reading whose timestamp is before `sunrise - 30 minutes`, where sunrise is the existing Melbourne sun-table value for the requested date (resolved via `time.ParseInLocation` in `sydneyTZ`, so DST-correct). The same lower-bound filter applies to both `firstPpv` (night-end) and `lastPpv` (evening-start) so a single stray reading cannot pollute either boundary on a day with no real production. If neither candidate qualifies, the corresponding block falls back to the table sunrise/sunset via the existing `boundarySource = estimated` path.

No upper bound is applied — solar drops to zero well before sunset, so a post-sunset blip is not a credible last-of-day reading and has not been observed in practice.

### Rationale

- 30 minutes is generous enough to admit early-morning twilight production from east-facing panels on clear days without admitting middle-of-night blips, which are typically hours before the threshold.
- Reusing the existing sun table avoids any new astronomical calculation; the same lookup that powers the no-readings fallback now also feeds the filter, so behaviour is consistent across the two paths.
- Keeping the evening side unchanged minimises blast radius and matches the actual observed failure mode.

### Alternatives Considered

- **Strict "after sunrise" cutoff (no buffer)**: Rejects all pre-sunrise readings — simpler, but pessimises legitimate early-morning production where panels can produce measurable power 10–20 minutes before civil sunrise. Rejected for being too aggressive on a continuous edge case.
- **Require N consecutive `Ppv > 0` readings**: More robust against any-time noise, including hypothetical near-sunrise blips. Rejected as over-engineering for the observed failure mode and adds complexity (window size, gap handling) without a clear win.
- **Apply a minimum-power threshold (e.g. `Ppv > 50 W`)**: Filters by magnitude rather than time. Rejected because real twilight production can be very low (single-digit watts on a partly-cloudy morning) and is still informative for the boundary; time-based filtering is a cleaner signal.
- **Apply an upper-bound filter at `sunset + buffer` on `lastPpv`**: Would also reject late-evening blips. Deferred — solar production drops to zero well before sunset, and adding the upper bound risks false negatives on legitimately late-clearing afternoons. The lower-bound-only filter on `lastPpv` is enough to handle the observed failure mode (a single 01:30 blip that, without filtering, would have made the evening block start at 01:30 and span 22+ hours).

### Consequences

**Positive:**
- Sensor blips before `sunrise - 30 min` no longer corrupt the Night block boundary.
- The fallback to estimated sunrise is the same code path that already handles fully overcast days, so no new states are added.

**Negative:**
- Production starting more than 30 min before the table sunrise will be ignored by the filter; in practice this would require either a major astronomical shift or a panel installation that captures pre-civil-twilight scattered light, neither of which is plausible for Melbourne. Acceptable trade-off.
- `findEveningNight` now resolves Melbourne sunrise eagerly (not just on the no-readings fallback path). One extra `time.ParseInLocation` call per `/day` request — negligible.

### Impact

- `internal/api/compute.go`: `findEveningNight` adds a `preSunriseBlipBuffer` constant and a `resolveSunrise` closure mirroring `resolveSunset`; the inline scan now skips `Ppv > 0` candidates before `sunrise - 30 min` for both the `firstPpv` and `lastPpv` slots.
- `internal/api/compute_test.go`: three new `TestFindEveningNight` cases pin the blip-only / blip-plus-real / within-buffer behaviour. The blip-only case asserts on both Night and Evening to lock in the symmetric fallback.

---
