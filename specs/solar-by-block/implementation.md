# Implementation: Solar by Block (T-1162)

This document explains the solar-by-block implementation at three levels — beginner, intermediate, expert — and validates that the spec was fully realised.

## Beginner Level

### What this does

The Day Detail page shows your day broken into five "blocks": night, morning peak, off-peak, afternoon peak, evening. Until now each row showed only how much energy your house *used* during that block. This change adds, for the three daylight blocks (morning peak, off-peak, afternoon peak), how much *solar energy your panels produced* during the same window — displayed inline next to the usage value with a small sun icon in solar amber.

### Why it matters

In winter especially, knowing whether off-peak load was covered by solar versus drawn from grid/battery is the most useful single question for spotting changes in self-consumption habits. Putting solar production on the same row as load makes the comparison immediate.

### Key concepts (in plain language)

- **PPV** — instantaneous solar panel power (kilowatts), recorded by the poller every 10 seconds.
- **Trapezoidal integration** — adding up the area under a series of (time, power) points to get an energy total in kWh. Same technique already used for the load values.
- **Block** — a chronological slice of the day (night ≈ pre-sunrise, morning peak ≈ sunrise→11:00, off-peak = 11:00–14:00, afternoon peak ≈ 14:00→sunset, evening ≈ post-sunset).
- **Backfill CLI** — a one-shot Go program the operator runs locally to populate solar values on historical days that were summarised before this feature shipped.

---

## Intermediate Level

### Components touched

| Layer | File | Change |
|---|---|---|
| Integration | `internal/derivedstats/integrate_ppv.go` | New sibling of `integratePload`. Returns `(kwh, sampleCount)`. |
| Block builder | `internal/derivedstats/blocks.go` | Two new fields on `pendingBlock` (`unroundedSolarKwh`, `solarSampled`). After the existing `integratePload` pass, call `integratePpv` for daylight kinds only. `buildDailyUsageBlock` emits a rounded `*float64` when `solarSampled`, else nil. |
| Wire type | `internal/derivedstats/types.go` | `SolarKwh *float64 \`json:"solarKwh,omitempty"\`` on `DailyUsageBlock`. |
| Persistence | `internal/dynamo/models.go`, `derived_conv.go` | Mirror field on `DailyUsageBlockAttr`; explicit pass-through in both `DailyUsageFromAttr` and `DailyUsageToAttr`. |
| Sole-writer note | `internal/dynamo/dynamostore.go` | One-line comment on `UpdateDailyEnergyDerived` declaring it the only write path for `dailyUsage` outside the backfill CLI. |
| Backfill CLI | `cmd/backfill-solar/main.go` (+ test) | Stand-alone `main` package. Scans `flux-daily-energy` for a date range, recomputes `Blocks()` against current readings, **patches `solarKwh` in place** on the stored blocks, writes via `SET dailyUsage = :du`. |
| Swift model | `Flux/Packages/FluxCore/Sources/FluxCore/Models/APIModels.swift` | `solarKwh: Double?` (`nil` covers both null and absent JSON). |
| Day Detail UI | `Flux/Flux/DayDetail/DayInFiveBlocksPanel.swift` | Inline render with `sun.max.fill` in `FluxTheme.Palette.amber`. `lineLimit(1).minimumScaleFactor(0.85)` on time/value text to absorb narrow widths and Dynamic Type. |
| Tests | `integrate_ppv_test.go` (+ rapid property test), `blocks_test.go` (TestBlocks_SolarKwh suite), `derived_conv_test.go`, `derived_conv_property_test.go`, `sizing_test.go`, `dailysummary_test.go`, `derivedstats_e2e_test.go`, `day_derivedstats_test.go`, `history_*_test.go`, `cmd/backfill-solar/main_test.go`, `APIModelsTests.swift` | See AC table below. |

### Implementation approach

There is exactly one write path for `dailyUsage` in the live system: the poller's daily summarisation pass (`runSummarisationPass` in `internal/poller/dailysummary.go`), gated by a `derivedStatsComputedAt` sentinel. The Lambda `/day` handler never writes. This made the persistence story trivial: extend `DailyUsageBlock`, `DailyUsageBlockAttr`, both converters; `UpdateDailyEnergyDerived` carries the new field with no change.

For the live `/day` request, `Blocks()` runs the new integration and returns `solarKwh` in the response. For historical days where readings have already aged out (30-day TTL), the field stays nil and the iOS UI hides the value (per AC 3.4).

The backfill CLI handles the bridge case: days that were summarised before this feature shipped, whose readings are still inside TTL. Rather than rewriting the whole `dailyUsage` map (which would shift `totalKwh` and block boundaries against TTL-pruned readings — Decision 7), it deep-copies the stored blocks and only patches `SolarKwh` per matching `Kind`. Re-running with no new readings short-circuits at `needsBackfill` (idempotent, AC 2.5).

### Trade-offs

- **Persistence skipped when no PPV samples** (Decision 4). `integratePpv` returns `(kwh, sampleCount)` and `Blocks()` only sets `solarSampled = sampleCount > 0`. `omitempty` keeps the JSON key absent. A genuinely all-zero day (heavy fog) still has samples and emits `&0.0` — that is the wanted distinction.
- **Sibling function instead of generic** (Decision 6). `integratePpv` and `integratePload` differ only by the field selector (`Ppv` vs `Pload`) and the extra `sampleCount` return. A generic with a closure was rejected to keep the algorithmic body legible at the integration hot path.
- **Inline display, daylight rows only** (Decision 1, Decision 3). Solar on night/evening would be visual noise; the ticket scope was Day Detail only — Dashboard and History were left out by design.

---

## Expert Level

### Algorithmic parity with `integratePload`

`integratePpv` mirrors `integratePload` line-for-line: same `iL`/`iR` bracket search (early-break linear scan, requires sorted input), same edge synthesis (linear interpolation if the bracketing pair gap ≤ `maxPairGapSeconds = 60`), same trapezoidal pair sum, same kWh conversion (`watt-seconds / 3_600_000`), same `max(value, 0)` clamping for sensor noise. Two divergences only:

1. Field selector: `r.Ppv` instead of `r.Pload`.
2. `sampleCount` increments on every reading whose timestamp is inside `[startUnix, endUnix)` — synthesised edge points do not count. This is the lever that drives nil-vs-zero semantics.

The split-additivity property (`integrate([a,c)) ≈ integrate([a,b)) + integrate([b,c))` when no >60 s gap straddles `b`) is verified with `pgregory.net/rapid` in `integrate_ppv_property_test.go`.

### Today-gate interaction

`Blocks()` already clamps daylight blocks whose `[start, end)` extends past `now` for today. The clamp happens *before* the integration pass, so `integratePpv` runs over the already-clamped window. The today-gate's solar-still-up branch (`solarStillUp`) does not need solar-aware logic of its own — when present, the afternoonPeak's clamped `[start, now)` is integrated, producing the elapsed-portion solar (AC 1.5). When `start > now`, the future-omit pass drops the block entirely; no solar value is reported.

### Backfill CLI internals

- **Boundary parsing**: `validateOpts` parses both `--from` and `--to` with `time.ParseInLocation` in Sydney TZ and rejects reversed ranges (`--from after --to`). This catches operator typos before any DynamoDB call.
- **Pagination**: `paginate[T]` walks `LastEvaluatedKey` for both daily-energy and readings queries. Acceptable duplication with `dynamo.DynamoReader` — the CLI takes a minimal `dynamoAPI` interface (`Query` + `UpdateItem`) so the test fake doesn't need to satisfy `ReadAPI`.
- **`needsBackfill`**: short-circuits before any readings query when all three daylight blocks already have `SolarKwh` set. Makes re-runs cheap (AC 2.5).
- **`patchSolar`**: deep-copies `stored.Blocks` and only mutates `SolarKwh` on the daylight blocks where the recomputation produced a non-nil value. Every other field — `TotalKwh`, `Start`, `End`, `BoundarySource`, `PercentOfDay`, `Status`, `AverageKwhPerHour` — survives byte-for-byte (Decision 7). Verified by `TestBackfill_PreservesNonSolarFieldsByteForByte`.
- **Concurrency vs poller**: backfill defaults `--to` to yesterday; the poller writes only yesterday once a day (sentinel-gated). On overlap the algorithm is deterministic — same readings + same off-peak window → same `solarKwh` per block — so last-writer-wins produces field-equivalent state.
- **`time.Now()` safety inside `Blocks()`**: passing a real wall-clock `now` to historical dates is safe because the today-gate's `recentSolar` flag is set only when a reading lands in `[now-5min, now]`. Historical readings cannot fall inside that window, so `recentSolar = false` and the today-gate stays inert.

### Wire format and decoder

- Backend JSON tag: `"solarKwh,omitempty"` — nil encodes as missing key, not explicit null.
- DynamoDB attribute tag: `dynamodbav:"solarKwh,omitempty"` — same omission semantics at storage layer.
- Swift `solarKwh: Double?` — the synthesised `Codable` decoder treats `null` and absent both as nil (verified by four explicit fixture tests in `APIModelsTests.swift`).
- Item size budget: `sizing_test.go` re-asserts `< 4 KB` per row with the new field populated on all three daylight blocks.

### Edge cases tested

- All-zero PPV with samples present (heavy fog) → `&0.0`.
- Single PPV sample inside the window with no neighbour to trapezoid against → `(kwh=0, sampleCount=1)` → `&0.0`. Documented in test fixture `single sparse reading`.
- Reading gap > 60 s straddling a window boundary → integration skips the gap; sample count still > 0; block emits a non-nil value over the surviving sub-windows.
- Daylight block fully in the future (today, before sunrise) → future-omit drops it entirely.
- Day predating the readings TTL → backfill cannot recover; live read returns `solarKwh = nil`; UI hides.

### Architecture impact

- The "single writer for `dailyUsage`" invariant is now stated in code (`UpdateDailyEnergyDerived` comment) and enforced operationally — only the backfill CLI runs outside that path. Future writers must explicitly revisit the backfill's idempotency assumption.
- `/history` carries the new field through transparently because `DailyUsage` is part of the response shape; the field is unsurfaced on History UI by design (Non-Goal).
- iOS `DailyUsageBlock` initialiser adds `solarKwh: Double? = nil` at the matching position — all existing call sites compile unchanged. SwiftData cache (`CachedDayEnergy`) stores `DailyUsage` as Codable JSON, so adding an optional field is non-breaking.

---

## Completeness Assessment

### Fully implemented

All 18 tasks in `tasks.md` are marked `[x]` and verified in code:

| Requirement | Implementation | Test |
|---|---|---|
| 1.1 daylight blocks carry `solarKwh` | `blocks.go:245-250`, `:297-300` | `blocks_test.go` TestBlocks_SolarKwh sunny case; `day_derivedstats_test.go` |
| 1.2 night/evening omit `solarKwh` | switch fires only on three kinds | TestBlocks_SolarKwh night/evening assertions |
| 1.3 trapezoidal integral of PPV | `integrate_ppv.go` | `integrate_ppv_test.go`, property test |
| 1.4 no samples → nil | `solarSampled = sampleCount > 0`; emit guarded by `p.solarSampled` | `blocks_test.go` no-readings cases |
| 1.5 in-progress today reports elapsed integral | today-gate clamp before integration | `blocks_test.go` in-progress case |
| 2.1 DynamoDB attr extended | `models.go`, `derived_conv.go` | `derived_conv_test.go` round-trip + property + `sizing_test.go` |
| 2.2 persist on summarisation when samples present | poller path unchanged; converters carry field | `dailysummary_test.go` SolarKwh persistence assertion |
| 2.3 skip persistence when no samples | omitempty + `solarSampled` gate | covered transitively |
| 2.4 standalone CLI | `cmd/backfill-solar/main.go` | full `main_test.go` suite |
| 2.5 idempotent re-runs | `needsBackfill` short-circuit | `TestBackfill_Idempotent_SkipsRowsAlreadyPopulated` |
| 2.6 stored-without-solar → nil | converters preserve nil | covered by round-trip + decoder tests |
| 2.7 backwards-compatible API | `omitempty` + Swift Optional | `APIModelsTests.swift` absent-key |
| 3.1 inline display | `DayInFiveBlocksPanel.swift:66-73` | preview + manual |
| 3.2 sun icon, amber colour | `sun.max.fill`, `FluxTheme.Palette.amber` | preview |
| 3.3 night/evening rows unchanged | `isDaylight` guard | preview |
| 3.4 nil → no icon, no value | `if let solar = block.solarKwh` | `APIModelsTests.swift` null/absent fixtures |
| 3.5 zero renders as `0.0 kWh` | `EnergyFormatting.format(0.0)` | `APIModelsTests.swift` zero fixture |
| 3.6 same precision/unit as totalKwh | reuses `EnergyFormatting.format` | preview |
| 4.1 unit tests for sunny / winter / gap / collapsed-morning | TestBlocks_SolarKwh + integrate tests | partial — see below |
| 4.2 iOS decoder fixtures (zero + null + absent) | `APIModelsTests.swift` | OK |
| 4.3 backfill dry-run mode | `--dry-run` flag, log-only path | `TestBackfill_DryRun_NoUpdateItemCalled` |

### Partially implemented (minor gap)

**AC 4.1 — "morning peak collapsed to zero duration"** sub-case is covered transitively by the existing degenerate-omit logic in `Blocks()` (`p.start.Before(p.end)` filter at `blocks.go:227`) but does not have a dedicated subtest in `TestBlocks_SolarKwh`. The behaviour is: when sunrise lands after off-peak start, no morningPeak block is produced; nothing to assert about its `SolarKwh`. The gap is "no dedicated subtest demonstrating the omission", not "untested behaviour". Acceptable as a minor follow-up; not a blocker for this PR.

### Pre-push fixes applied

Two important issues surfaced by the pre-push review and fixed in this branch:

1. `validateOpts` in `cmd/backfill-solar/main.go` now rejects reversed `--from`/`--to` with a clear error; new `TestValidateOpts_RejectsReversedDateRange` covers it.
2. `DayInFiveBlocksPanel.swift` time / total / solar text now uses `lineLimit(1).minimumScaleFactor(0.85)` to absorb narrow widths and larger Dynamic Type — addresses Decision 3's negative-consequence note that the inline layout could pinch on smaller screens.

### Missing

Nothing required by the spec is missing. The decision log contains every divergence from the original draft (notably Decisions 4, 5, 7, 8 which were revised after review).
