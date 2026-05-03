---
references:
    - specs/low-since-offpeak/smolspec.md
    - specs/low-since-offpeak/decision_log.md
---
# Low SoC Since Last Off-Peak End

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
