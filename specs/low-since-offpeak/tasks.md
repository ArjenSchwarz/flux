---
references:
    - specs/low-since-offpeak/smolspec.md
    - specs/low-since-offpeak/decision_log.md
---
# Lowest SoC Since Midnight (Today)

## Completed: T-1084 (off-peak-end boundary, shipped)

- [x] 1. lastOffpeakEnd helper exists with passing tests <!-- id:9hnhhep -->
  - Add lastOffpeakEnd(now, offpeakStart, offpeakEnd) (time.Time, bool) in internal/api/compute.go alongside nextOffpeakStart.
  - Add a table-driven test in internal/api/compute_test.go covering: now before today end -> yesterday end; now at today end -> today end; now after today end -> today end; now inside today window -> yesterday end; invalid config strings -> returns false.
  - Success: go test ./internal/api/... passes including the new test.

- [x] 2. /status returns low24h filtered to readings since last off-peak end <!-- id:9hnhheq -->
  - In internal/api/status.go (around line 119-124) filter allReadings via filterReadings(allReadings, lastEnd.Unix(), nowUnix) before passing to MinSOC.
  - When lastOffpeakEnd returns false, leave battery.Low24h nil.
  - Update the Low24h doc comment in internal/api/response.go to describe the new semantics.
  - Success: go build ./... passes; status.go leaves Low24h nil whenever no readings exist in the resolved window or the config is unparseable.
  - Blocked-by: 9hnhhep (lastOffpeakEnd helper exists with passing tests)

- [x] 3. Status handler tests reflect the new boundary <!-- id:9hnhher -->
  - Update TestHandleStatusAllDataPresent in internal/api/status_test.go so its low24h assertion matches the since-last-off-peak-end semantics (retime the existing 24h-old SoC=20 reading to fall inside the new window OR change the asserted minimum).
  - Add a test asserting low24h is nil when off-peak config is unparseable.
  - Success: go test ./internal/api/... is green; the new no-config test would fail against a return-everything implementation.
  - Blocked-by: 9hnhheq (/status returns low24h filtered to readings since last off-peak end)

- [x] 4. Dashboard and widget labels say Lowest <!-- id:9hnhhes -->
  - Change the row label string "24h low" to "Lowest" in Flux/Flux/Dashboard/SecondaryStatsView.swift:24 and Flux/FluxWidgets/Views/SystemLargeView.swift:39.
  - Success: make macos-lint passes; the SwiftUI preview in SecondaryStatsView shows the new label without truncation.
  - Blocked-by: 9hnhher (Status handler tests reflect the new boundary)

## Follow-up: redirect to Sydney-midnight boundary

(IDs to be assigned when added to rune.)

- [ ] 5. startOfDaySydney helper replaces lastOffpeakEnd
  - Delete lastOffpeakEnd from internal/api/compute.go.
  - Add startOfDaySydney(now time.Time) time.Time returning time.Date(year, month, day, 0, 0, 0, 0, sydneyTZ) for now.In(sydneyTZ).
  - Replace TestLastOffpeakEnd in internal/api/compute_test.go with TestStartOfDaySydney covering: 06:00 Sydney weekday -> midnight same date; just past midnight Sydney -> midnight same date; near 23:59 Sydney -> midnight same date; UTC instant that lands on the next Sydney day -> Sydney's tomorrow midnight (timezone conversion proof).
  - Success: go test ./internal/api/... is green; lastOffpeakEnd no longer appears anywhere in the codebase.

- [ ] 6. /status computes low24h since Sydney midnight today
  - In internal/api/status.go, drop the lastOffpeakEnd call and the off-peak field reads in the low24h block. Compute start := startOfDaySydney(now).Unix(), then filterReadings(allReadings, start, nowUnix), then MinSOC. Leave battery.Low24h nil only when MinSOC returns found=false.
  - Update the Low24h doc comment in internal/api/response.go to describe the new "since midnight Sydney today" semantics.
  - Success: go build ./... and go test ./internal/api/... pass; nextOffpeakStart still used for cutoff suppression at status.go:107 and :138 (verify by grep).
  - Blocked-by: 5

- [ ] 7. Status handler tests reflect the midnight boundary
  - Update TestHandleStatusAllDataPresent: ensure at least one fixture reading sits before Sydney midnight so the filter must exclude it; assert Low24h.Soc against the lowest reading inside the new window.
  - Remove TestHandleStatusLow24hUnparseableOffpeak — there is no longer an off-peak failure mode for this field.
  - Add TestHandleStatusLow24hNoReadingsToday: readings exist but all predate Sydney midnight; assert battery.Low24h is nil.
  - Success: go test ./internal/api/... is green; the new no-readings-today test would fail against a return-everything implementation.
  - Blocked-by: 6

- [ ] 8. Documentation reflects the redirect
  - Update docs/agent-notes/api-layer.md to state that low24h is now "lowest SoC since midnight Sydney today" and no longer reads off-peak config.
  - Add a CHANGELOG.md entry under Unreleased > Changed describing the redirect (one line).
  - Success: rg -n "off-peak end\|lastOffpeakEnd" docs/ CHANGELOG.md returns no stale references for low24h semantics.
  - Blocked-by: 7
