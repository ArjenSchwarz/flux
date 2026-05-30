---
references:
    - specs/peak-from-readings/smolspec.md
    - specs/peak-from-readings/decision_log.md
---
# Peak From Readings

## Backend

- [x] 1. Peak grid import is computed from readings over the two windows bracketing off-peak <!-- id:1am1857 -->
  - A new integration helper sums max(Pgrid,0) over [day-start, offpeak-start) and [offpeak-end, next-day-start) using the same trapezoidal method and usability gate as the existing off-peak integrator.
  - Returns a combined value plus provenance and a usable flag that is true only when both sub-windows pass the gate.
  - Verify with unit tests: both sub-windows summed correctly; gate failure when one sub-window has sparse readings yields not-usable; DST-length (23h/25h) days handled via the unix-timestamp window args; peak+offpeak lands within 3% of eInput on a representative full day.
  - References: specs/peak-from-readings/smolspec.md

- [x] 2. The daily-energy row carries and persists the peak grid import value <!-- id:1am1858 -->
  - The stored daily-energy item gains an optional peak grid import field plus an independent compute sentinel; the daily-energy write path persists both when set and omits them when nil.
  - Storage naming divergence with the off-peak field is intentional (Decision 6).
  - Verify with a store-layer test that round-trips a row with the field set and a row with it unset, confirming the attribute is absent from the persisted item when nil.
  - Blocked-by: 1am1857 (Peak grid import is computed from readings over the two windows bracketing off-peak)
  - References: specs/peak-from-readings/smolspec.md

- [x] 3. The hourly summarisation pass backfills peak grid import via an independent sentinel <!-- id:1am1859 -->
  - The pass populates peak grid import on eligible rows gated on its own sentinel, leaving the existing derived-stats block and its sentinel untouched (Decision 3).
  - A row that already has derived stats but no peak gets peak written on the next tick; a row is skipped only when both sentinels are set; a sub-window gate failure leaves the field absent.
  - Verify with pass-level tests: a row with derived stats but no peak gets peak written; a row with both sentinels set is skipped; a gate failure leaves the field unwritten.
  - Blocked-by: 1am1858 (The daily-energy row carries and persists the peak grid import value)
  - References: specs/peak-from-readings/smolspec.md

- [x] 4. The /day and /history responses expose peakGridImportKwh <!-- id:1am185a -->
  - Both the day-summary and history-day responses include peakGridImportKwh sourced from the stored value, at JSON key peakGridImportKwh alongside offpeakGridImportKwh, omitted when absent.
  - No real-time compute path for today (Decision 4).
  - Verify with API tests asserting the field appears with the stored value when present and is absent from the JSON when unset, on both endpoints.
  - Blocked-by: 1am1859 (The hourly summarisation pass backfills peak grid import via an independent sentinel)
  - References: specs/peak-from-readings/smolspec.md

- [x] 5. Historical peak grid import is backfilled by the renamed backfill-grid CLI
  - Rename cmd/backfill-offpeak to cmd/backfill-grid (Decision 7) and extend its per-date loop so that, alongside the unchanged flux-offpeak recompute, it computes peakGridImportKwh via derivedstats.IntegratePeakGridImportKwh over the two windows bracketing off-peak and writes peakGridImportKwh + peakComputedAt to the corresponding flux-daily-energy row through UpdateDailyEnergyDerived (peak group only).
  - The CLI must GET the daily-energy row first and skip the date for peak when it is absent (no phantom-row creation); today is skipped on both sides; off-peak recompute behaviour is unchanged. Add a --table-daily-energy flag.
  - Verify with CLI-core tests: a date with a present daily-energy row gets peak written; an absent daily-energy row is skipped for peak without creating a row; dry-run writes nothing to either table; off-peak recompute still works.
  - References: specs/peak-from-readings/smolspec.md, specs/peak-from-readings/decision_log.md

## iOS

- [x] 6. The History grid entry prefers server peak grid import with a residual fallback <!-- id:1am185b -->
  - The iOS day-energy model and its cached form decode the optional peakGridImportKwh field.
  - The History card grid entry uses the server value when non-nil and falls back to max(0, eInput − offpeakImport) when absent; no display changes beyond the source of the peak number.
  - Verify with tests asserting the server value is used when present and the residual is used when the field is nil.
  - Blocked-by: 1am185a (The /day and /history responses expose peakGridImportKwh)
  - References: specs/peak-from-readings/smolspec.md

- [x] 7. Day Detail Power summary shows server peak grid import with residual fallback
  - Decode the existing /day peakGridImportKwh on the FluxCore DaySummary, and have the Day Detail Power summary row (SummaryBlock 'Grid in (peak)') and its Compare overlay (ComparisonSnapshot) prefer the server value, falling back to max(0, eInput − offpeak) when absent.
  - The Dashboard (/status, today) has no server peak and must keep its current residual behaviour unchanged. Past days only; today falls back (Decision 4).
  - Verify with tests: DaySummary decodes the field (present / omitted); ComparisonSnapshot prefers server value and falls back when nil. No display/layout changes beyond the peak number source.
  - References: specs/peak-from-readings/smolspec.md, specs/peak-from-readings/decision_log.md

- [x] 8. Day Detail peak-imports cost uses server peak grid import with residual fallback
  - DayCosts prices peak from the server peakGridImportKwh when present, falling back to the residual; off-peak savings and the all-peak-when-offpeak-nil rule (daily-costs Decision 23) are unchanged. The DayEnergy.costs forwarder passes the field into the transient DaySummary it builds.
  - Verify with tests: peak cost uses the server value when present and the residual when nil; off-peak savings unchanged.
  - References: specs/peak-from-readings/smolspec.md, specs/peak-from-readings/decision_log.md
