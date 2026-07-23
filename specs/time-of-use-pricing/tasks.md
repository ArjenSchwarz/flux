---
references:
    - specs/time-of-use-pricing/requirements.md
    - specs/time-of-use-pricing/design.md
    - specs/time-of-use-pricing/decision_log.md
---
# Time-of-Use Pricing

## Foundation: Go plan domain and data layer

- [x] 1. Write failing tests for internal/plan band parsing and plan validation <!-- id:chkfin5 -->
  - New leaf package internal/plan (no dynamo/api imports)
  - Band time parser must accept 24:00 (internal minutes 0-1440) — derivedstats.ParseOffpeakWindow rejects h>23 and must not be reused
  - Cover codes: bandWindowInvalid, bandOverlap, multipleFreeBands, savingsRateMissing, noRatedBand (free window spans whole day; zero-width default remainder is valid), rate bounds 0..10 and 4dp
  - endDate <= startDate rejected (zero-day plan under exclusive ends)
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [1.5](requirements.md#1.5), [1.6](requirements.md#1.6), [1.7](requirements.md#1.7)

- [x] 2. Implement internal/plan types, band parser, and Validate <!-- id:chkfin6 -->
  - Blocked-by: chkfin5 (Write failing tests for internal/plan band parsing and plan validation)
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [1.5](requirements.md#1.5), [1.6](requirements.md#1.6), [1.7](requirements.md#1.7)

- [x] 3. Write failing unit and property tests for Segments, Covers, PlanFor, FreeWindow, SegmentBounds <!-- id:chkfin7 -->
  - rapid properties: Segments always tiles 00:00-24:00, no overlap, abutting same-rate segments NOT merged (Q26)
  - Property: sum of per-segment IntegrateOffpeakDeltas grid import equals whole-day integral within epsilon, including 23h/25h Sydney days via wall-clock SegmentBounds (never dayStart.Add elapsed arithmetic)
  - Covers/PlanFor boundaries: switch day D goes to successor, D-1 to predecessor
  - Repeated DST hour: deterministic time.Date resolution is acceptable (design)
  - Blocked-by: chkfin6 (Implement internal/plan types, band parser, and Validate)
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.8](requirements.md#1.8), [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [3.8](requirements.md#3.8)

- [x] 4. Implement Segments, Covers, PlanFor, FreeWindow, SegmentBounds <!-- id:chkfin8 -->
  - Blocked-by: chkfin7 (Write failing unit and property tests for Segments, Covers, PlanFor, FreeWindow, SegmentBounds)
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.8](requirements.md#1.8), [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [3.8](requirements.md#3.8)

- [x] 5. Create shared cross-language cost/segment vectors and Go golden-formula tests <!-- id:chkfin9 -->
  - internal/api/testdata/pricing_segments.json + pricing_costs.json, consumed later by FluxCore tests (note_lengths.json pattern)
  - Cost vectors MUST cover: all four tier-2 combos (offpeak +/-, server peak +/-), zero clamp, sparse-complete offpeak row (integratedAt set, sampleCount 0 = unavailable), geometry-mismatch day, multi-rate fallback (max rate, $0.00 savings)
  - Go test implements the tier-2 legacy DayCosts formula table from design.md — the same helper becomes the migrate tool's golden formula
  - Blocked-by: chkfin8 (Implement Segments, Covers, PlanFor, FreeWindow, SegmentBounds)
  - Stream: 1
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [3.5](requirements.md#3.5), [3.6](requirements.md#3.6), [5.2](requirements.md#5.2)

- [x] 6. Write failing tests for dynamo new-shape PricingItem and legacy read transform <!-- id:chkfina -->
  - internal/dynamo/pricing.go
  - Legacy detection via raw attribute map (peakRate present) — plain unmarshal silently drops unknown fields
  - Transform shared with cmd/migrate-pricing: defaultRate <- peakRate, windows <- [{11:00-14:00 free}], savingsReferenceRate <- offPeakSavingsRate, endDate <- legacyEnd + 1 day
  - Sentinel row untouched
  - Blocked-by: chkfin6 (Implement internal/plan types, band parser, and Validate)
  - Stream: 1
  - Requirements: [5.1](requirements.md#5.1), [7.3](requirements.md#7.3)

- [x] 7. Implement dynamo PricingItem band shape, raw-map legacy detection, transitional read transform <!-- id:chkfinb -->
  - Blocked-by: chkfina (Write failing tests for dynamo new-shape PricingItem and legacy read transform)
  - Stream: 1
  - Requirements: [5.1](requirements.md#5.1), [7.3](requirements.md#7.3)

- [x] 8. Write failing tests for DailyEnergyItem bandImports group and OffpeakItem window geometry <!-- id:chkfinc -->
  - internal/dynamo/models.go + dynamostore.go
  - bandImports: rated segments only {start,end,kwh} — free import stays on the offpeak row (Q31)
  - bandsComputedAt third sentinel group in UpdateDailyEnergyDerived (peak-from-readings Decision 3 pattern)
  - OffpeakItem windowStart/windowEnd HH:MM snapshot; absent means 11:00-14:00 (pre-feature rows)
  - Stream: 1
  - Requirements: [3.4](requirements.md#3.4), [3.5](requirements.md#3.5)

- [x] 9. Implement daily-energy band group and offpeak geometry fields <!-- id:chkfind -->
  - Blocked-by: chkfinc (Write failing tests for DailyEnergyItem bandImports group and OffpeakItem window geometry)
  - Stream: 1
  - Requirements: [3.4](requirements.md#3.4), [3.5](requirements.md#3.5)

- [x] 10. Write failing tests for ReplaceOpenEnded exclusive-end and legacy-shape rejection <!-- id:chkfine -->
  - internal/dynamo/pricing_transactional.go
  - closing.endDate = successor.startDate (same literal date); delete previousDate helper
  - Closing row still carrying peakRate -> legacyShape error, no partial UpdateItem patch (Q32) — a partial update would create a legacy-detected row with an exclusive end date, double-shifted by transform + migration
  - Blocked-by: chkfinb (Implement dynamo PricingItem band shape, raw-map legacy detection, transitional read transform)
  - Stream: 1
  - Requirements: [2.2](requirements.md#2.2), [2.6](requirements.md#2.6), [5.4](requirements.md#5.4)

- [x] 11. Implement ReplaceOpenEnded same-day succession and legacy guard <!-- id:chkfinf -->
  - Blocked-by: chkfine (Write failing tests for ReplaceOpenEnded exclusive-end and legacy-shape rejection)
  - Stream: 1
  - Requirements: [2.2](requirements.md#2.2), [2.6](requirements.md#2.6), [5.4](requirements.md#5.4)

## Lambda API

- [x] 12. Write failing handler tests for band-based /pricing endpoints <!-- id:chkfing -->
  - internal/api/pricing_handler.go tests; wire shape from design.md
  - Validation codes incl. noRatedBand and legacyShape via raw JSON key check
  - Overlap is half-open interval intersection naming the conflicting plan (AC 2.5)
  - replace-open-ended: same-day succession + legacy reject
  - Future-dated successor and ending the open-ended plan without a successor both accepted; 4KB body cap retained
  - Blocked-by: chkfin8 (Implement Segments, Covers, PlanFor, FreeWindow, SegmentBounds), chkfinf (Implement ReplaceOpenEnded same-day succession and legacy guard)
  - Stream: 1
  - Requirements: [1.7](requirements.md#1.7), [2.1](requirements.md#2.1), [2.3](requirements.md#2.3), [2.4](requirements.md#2.4), [2.5](requirements.md#2.5), [7.1](requirements.md#7.1), [7.2](requirements.md#7.2), [7.3](requirements.md#7.3)

- [x] 13. Implement pricing handlers for the band shape <!-- id:chkfinh -->
  - Blocked-by: chkfing (Write failing handler tests for band-based /pricing endpoints)
  - Stream: 1
  - Requirements: [1.7](requirements.md#1.7), [2.1](requirements.md#2.1), [2.3](requirements.md#2.3), [2.4](requirements.md#2.4), [2.5](requirements.md#2.5), [7.1](requirements.md#7.1), [7.2](requirements.md#7.2), [7.3](requirements.md#7.3)

- [x] 14. Write failing tests for plan-derived windows in status/day/history and nullable offpeak <!-- id:chkfini -->
  - status.go/day.go/history.go/compute.go tests; plans fetched in existing errgroups
  - /status.offpeak serialises null on no-free-band or no-plan day (never default window)
  - nextOffpeakStart uses the free band of the plan pricing the day the window falls on — switch eve must pick the successor window (AC 4.2/Q11)
  - No plan -> off-peak values absent not zero; unpriced days show no cost data (AC 2.7)
  - Blocked-by: chkfin8 (Implement Segments, Covers, PlanFor, FreeWindow, SegmentBounds), chkfinb (Implement dynamo PricingItem band shape, raw-map legacy detection, transitional read transform)
  - Stream: 1
  - Requirements: [4.1](requirements.md#4.1), [4.2](requirements.md#4.2), [4.4](requirements.md#4.4), [2.7](requirements.md#2.7)

- [x] 15. Implement plan-derived window resolution in the API and drop env window config <!-- id:chkfinj -->
  - Also remove OFFPEAK_START/END from cmd/api/main.go env validation and the handler offpeakStart/offpeakEnd fields
  - Lambda pricing read failure -> 500, never fabricated no-plan (Q14)
  - Blocked-by: chkfini (Write failing tests for plan-derived windows in status/day/history and nullable offpeak)
  - Stream: 1
  - Requirements: [4.1](requirements.md#4.1), [4.2](requirements.md#4.2), [4.4](requirements.md#4.4), [2.7](requirements.md#2.7)

- [x] 16. Write failing tests for bandImports in /day and /history and the shared live-split helper <!-- id:chkfink -->
  - DayEnergy + DaySummary gain nullable bandImports (rated only)
  - Today's split integrated live from readings via ONE shared helper used by both /day and /history (computeTodayEnergy pattern, AC 3.4)
  - /status does NOT carry bandImports (Q29)
  - Blocked-by: chkfind (Implement daily-energy band group and offpeak geometry fields), chkfinj (Implement plan-derived window resolution in the API and drop env window config)
  - Stream: 1
  - Requirements: [3.4](requirements.md#3.4), [3.6](requirements.md#3.6), [3.7](requirements.md#3.7)

- [x] 17. Implement bandImports serving and today's live split <!-- id:chkfinl -->
  - Blocked-by: chkfink (Write failing tests for bandImports in /day and /history and the shared live-split helper)
  - Stream: 1
  - Requirements: [3.4](requirements.md#3.4), [3.6](requirements.md#3.6), [3.7](requirements.md#3.7)

## Poller and tools

- [x] 18. Write failing tests for PlanSource <!-- id:chkfinm -->
  - internal/poller/plansource.go tests
  - Startup load via ListPricing (Scan); last-good cache served on read failure with warn (never treated as no-plan)
  - Cold start with unreachable table retries with backoff (Q14/AC 4.6)
  - Blocked-by: chkfin8 (Implement Segments, Covers, PlanFor, FreeWindow, SegmentBounds), chkfinb (Implement dynamo PricingItem band shape, raw-map legacy detection, transitional read transform)
  - Stream: 2
  - Requirements: [4.6](requirements.md#4.6)

- [x] 19. Implement PlanSource <!-- id:chkfinn -->
  - Blocked-by: chkfinm (Write failing tests for PlanSource)
  - Stream: 2
  - Requirements: [4.6](requirements.md#4.6)

- [x] 20. Write failing tests for the midnight-anchored OffpeakScheduler <!-- id:chkfino -->
  - internal/poller/offpeak.go tests
  - Run loop wakes at local midnight, refreshes PlanSource, resolves that day's window, sleeps to its start (Q27); no-free-band day sleeps to next midnight
  - Offpeak row written with windowStart/windowEnd geometry
  - Readings-only finalisation permitted without pending row or start snapshot when plan load succeeded late (Q36) — snapshot is diagnostics-only since T-1341
  - Blocked-by: chkfinn (Implement PlanSource), chkfind (Implement daily-energy band group and offpeak geometry fields)
  - Stream: 2
  - Requirements: [4.2](requirements.md#4.2), [4.3](requirements.md#4.3), [4.6](requirements.md#4.6)

- [x] 21. Implement OffpeakScheduler rework <!-- id:chkfinp -->
  - Blocked-by: chkfino (Write failing tests for the midnight-anchored OffpeakScheduler)
  - Stream: 2
  - Requirements: [4.2](requirements.md#4.2), [4.3](requirements.md#4.3), [4.6](requirements.md#4.6)

- [x] 22. Write failing tests for summarisation per-outcome gating and rated band block <!-- id:chkfinq -->
  - internal/poller/dailysummary.go tests, per-outcome table from design.md
  - Plan read failure -> PassResultError, no sentinels, retried; plan with free band -> all blocks; plan without free band -> socLow/derived/peak run in whole-day-rated mode, off-peak-split values absent, sentinels set; no plan -> window-independent stats only, band sentinel left unset (terminal until backfill)
  - Band block: rated segments via wall-clock SegmentBounds (fixes latent dayStart.Add DST bug in the peak block); sum raw integrals before per-entry rounding
  - Blocked-by: chkfinn (Implement PlanSource), chkfind (Implement daily-energy band group and offpeak geometry fields)
  - Stream: 2
  - Requirements: [3.5](requirements.md#3.5), [4.4](requirements.md#4.4), [4.6](requirements.md#4.6)

- [x] 23. Implement summarisation rework <!-- id:chkfinr -->
  - Blocked-by: chkfinq (Write failing tests for summarisation per-outcome gating and rated band block)
  - Stream: 2
  - Requirements: [3.5](requirements.md#3.5), [4.4](requirements.md#4.4), [4.6](requirements.md#4.6)

- [x] 24. Write failing tests for backfill CLI plan resolution and band rewrite <!-- id:chkfins -->
  - cmd/backfill-grid + cmd/backfill-solar: per-day plan resolution from the pricing table replaces --offpeak-start/end flags (a backfill spanning the switch date needs per-day windows)
  - backfill-grid additionally rewrites the day's rated bandImports and the offpeak row's window geometry
  - Multi-item writes are non-atomic but idempotent/re-runnable (documented)
  - Blocked-by: chkfinr (Implement summarisation rework)
  - Stream: 2
  - Requirements: [3.5](requirements.md#3.5), [4.5](requirements.md#4.5)

- [x] 25. Implement backfill CLI updates <!-- id:chkfint -->
  - Blocked-by: chkfins (Write failing tests for backfill CLI plan resolution and band rewrite)
  - Stream: 2
  - Requirements: [3.5](requirements.md#3.5), [4.5](requirements.md#4.5)

- [x] 26. Write failing tests for cmd/migrate-pricing <!-- id:chkfinu -->
  - Transform via the shared function from task 7
  - Golden check computes every priced day's costs with the EXACT legacy DayCosts formula (tier-2 table / task 5 helper), aborts on any mismatch before writing
  - Days priced by already-new-shape rows verified band-vs-band and logged
  - Idempotent (rows without peakRate skipped); --dry-run default writes nothing; --apply writes full-item Puts preserving pricingIds and sentinel
  - Blocked-by: chkfinb (Implement dynamo PricingItem band shape, raw-map legacy detection, transitional read transform), chkfin9 (Create shared cross-language cost/segment vectors and Go golden-formula tests)
  - Stream: 2
  - Requirements: [5.1](requirements.md#5.1), [5.2](requirements.md#5.2), [5.3](requirements.md#5.3), [5.4](requirements.md#5.4)

- [x] 27. Implement cmd/migrate-pricing <!-- id:chkfinv -->
  - Blocked-by: chkfinu (Write failing tests for cmd/migrate-pricing)
  - Stream: 2
  - Requirements: [5.1](requirements.md#5.1), [5.2](requirements.md#5.2), [5.3](requirements.md#5.3), [5.4](requirements.md#5.4)

- [x] 28. Update infrastructure template and remove window config plumbing <!-- id:chkfinw -->
  - infrastructure/template.yaml: ECS TaskRole read-only IAM on PricingTable MUST include dynamodb:Scan (ListPricing is a Scan) + GetItem/Query; TABLE_PRICING env for poller container
  - Drop OffpeakStartParameter/OffpeakEndParameter and OFFPEAK_START/END env from both containers
  - internal/config/config.go: remove OffpeakStart/End fields + validation; cmd/poller/logging.go log line
  - Wiring/config only — no test pair
  - Blocked-by: chkfinj (Implement plan-derived window resolution in the API and drop env window config), chkfinp (Implement OffpeakScheduler rework), chkfinr (Implement summarisation rework)
  - Stream: 2
  - Requirements: [4.1](requirements.md#4.1)

## App: FluxCore and UI

- [ ] 29. Write failing FluxCore tests for PricingPlan models and segmentation against shared vectors <!-- id:chkfinx -->
  - FluxCore/Pricing: PricingPlan/PlanWindow/PricingPlanDraft replace PricingPeriod/PricingPeriodDraft
  - covers(date:) uses < endDate (exclusive)
  - Draft validation mirrors server codes; segmentation helper output must match pricing_segments.json vectors exactly
  - Blocked-by: chkfin9 (Create shared cross-language cost/segment vectors and Go golden-formula tests)
  - Stream: 3
  - Requirements: [1.1](requirements.md#1.1), [1.4](requirements.md#1.4), [1.5](requirements.md#1.5), [2.2](requirements.md#2.2), [6.4](requirements.md#6.4)

- [ ] 30. Implement FluxCore plan models and segmentation <!-- id:chkfiny -->
  - Blocked-by: chkfinx (Write failing FluxCore tests for PricingPlan models and segmentation against shared vectors)
  - Stream: 3
  - Requirements: [1.1](requirements.md#1.1), [1.4](requirements.md#1.4), [1.5](requirements.md#1.5), [2.2](requirements.md#2.2), [6.4](requirements.md#6.4)

- [ ] 31. Write failing tests for three-tier cost resolution against cost vectors <!-- id:chkfinz -->
  - DayCosts/PeriodCosts tests against pricing_costs.json
  - Tier 1: bandImports geometry equals rated segments AND free import resolvable from offpeak row with matching geometry; sparse-complete row unusable
  - Tier 2: legacy formula verbatim — server-peak preference, max(0,) clamp, nil-offpeak path; single-rate plans never reach tier 3
  - Tier 3: multi-rate only — eInput x max rate, $0.00 savings
  - feedIn = eOutput x feedInRate, net = import - feedIn in every tier
  - Blocked-by: chkfiny (Implement FluxCore plan models and segmentation)
  - Stream: 3
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [3.5](requirements.md#3.5), [3.6](requirements.md#3.6), [4.5](requirements.md#4.5), [5.2](requirements.md#5.2)

- [ ] 32. Implement DayCosts and PeriodCosts three-tier resolution <!-- id:chkfio0 -->
  - Blocked-by: chkfinz (Write failing tests for three-tier cost resolution against cost vectors)
  - Stream: 3
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [3.5](requirements.md#3.5), [3.6](requirements.md#3.6), [4.5](requirements.md#4.5), [5.2](requirements.md#5.2)

- [ ] 33. Write failing tests for networking payloads, error codes, and nullable OffpeakData <!-- id:chkfio1 -->
  - URLSessionAPIClient pricing payloads for the band shape; PricingValidationReason new cases incl. legacyShape
  - OffpeakData.windowStart/windowEnd become optional and the offpeak object nullable — nil renders as no window, never defaultWindowStart/End constants (widgets included)
  - Blocked-by: chkfiny (Implement FluxCore plan models and segmentation)
  - Stream: 3
  - Requirements: [7.1](requirements.md#7.1), [7.2](requirements.md#7.2), [4.4](requirements.md#4.4)

- [ ] 34. Implement networking, APIModels, and chart window changes <!-- id:chkfio2 -->
  - Also DayChartDomain.offpeakRange: replace hardcoded +11h/+14h with the day's window from the API response; no window -> no shading
  - Blocked-by: chkfio1 (Write failing tests for networking payloads, error codes, and nullable OffpeakData)
  - Stream: 3
  - Requirements: [7.1](requirements.md#7.1), [7.2](requirements.md#7.2), [4.4](requirements.md#4.4)

- [ ] 35. Write failing tests for the pricing editor view model <!-- id:chkfio3 -->
  - PricingViewModel: default rate + exception windows editing (free toggle, rate field per window), 4dp normalisation
  - Succession flow ends the current plan on D and creates the successor starting D (AC 6.3)
  - Overlap remediation copy updated from day-before to switch-day phrasing (AC 6.5)
  - Blocked-by: chkfiny (Implement FluxCore plan models and segmentation)
  - Stream: 3
  - Requirements: [6.2](requirements.md#6.2), [6.3](requirements.md#6.3), [6.5](requirements.md#6.5)

- [ ] 36. Implement PricingEditor, plan list summary, and remediation copy <!-- id:chkfio4 -->
  - Settings/Pricing/PricingEditor.swift keeps sheet structure (dates, open-ended toggle, delete, remediation); Windows section rows: start/end pickers + Free toggle + rate field, add/remove
  - PricingPeriodsView band summary e.g. 'Free 10:00-15:00 / $0.2800 01:00-06:00 / $0.3500 default'
  - CostsCard/HistoryPeriodCostsCard layout unchanged (Q21)
  - Blocked-by: chkfio3 (Write failing tests for the pricing editor view model)
  - Stream: 3
  - Requirements: [6.1](requirements.md#6.1), [6.2](requirements.md#6.2), [6.3](requirements.md#6.3), [6.5](requirements.md#6.5)

- [ ] 37. Write failing tests for Day Detail and History cost wiring <!-- id:chkfio5 -->
  - DayDetailViewModel/HistoryViewModel pass bandImports + offpeak row data + plan into the cost helper
  - Partial-coverage caption (N of M days priced) retained (AC 3.7)
  - Blocked-by: chkfio0 (Implement DayCosts and PeriodCosts three-tier resolution), chkfio2 (Implement networking, APIModels, and chart window changes)
  - Stream: 3
  - Requirements: [3.7](requirements.md#3.7), [6.1](requirements.md#6.1)

- [ ] 38. Implement view-model cost wiring <!-- id:chkfio6 -->
  - Blocked-by: chkfio5 (Write failing tests for Day Detail and History cost wiring)
  - Stream: 3
  - Requirements: [3.7](requirements.md#3.7), [6.1](requirements.md#6.1)

## Cleanup

- [ ] 39. Remove transitional legacy read transform after the migration has run <!-- id:chkfio7 -->
  - Delete the dynamo read-side legacy conversion only; keep the write-path legacyShape rejection permanently
  - Gated by prerequisites.md: the production migration run must have completed and been verified first
  - Blocked-by: chkfinv (Implement cmd/migrate-pricing)
  - Stream: 1
  - Requirements: [5.4](requirements.md#5.4)
