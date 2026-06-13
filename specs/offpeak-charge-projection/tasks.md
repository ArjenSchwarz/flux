---
references:
    - specs/offpeak-charge-projection/requirements.md
    - specs/offpeak-charge-projection/design.md
    - specs/offpeak-charge-projection/decision_log.md
---
# Off-peak Charge Projection

- [ ] 1. Write failing unit and property tests for projectOffpeakEndSoc <!-- id:zugls51 -->
  - Table-driven in internal/api/compute_test.go using the design.md Testing Strategy fixtures (capacity 13.34 kWh, window end 14:00): 12:00/50->97.5, 13:30/40->56.9, 13:00/97->100.0, 13:00/90->98.2, 12:00/100->100.0; before-window 10:00/50->nil; after-window 14:30/50->nil; capacity<=0->nil
  - Cover the 95% tie-break: SoC exactly 95 charges at the 500 W rate; SoC <95 uses 4.5 kW up to 95 then 500 W
  - Property tests (testing/quick): result in [soc,100]; monotonic non-decreasing in hours and in soc, holding the other inputs fixed per generated paired comparison
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.5](requirements.md#1.5), [1.6](requirements.md#1.6), [1.7](requirements.md#1.7), [1.8](requirements.md#1.8), [1.9](requirements.md#1.9), [1.10](requirements.md#1.10), [2.1](requirements.md#2.1), [2.3](requirements.md#2.3)
  - References: internal/api/compute.go, internal/api/compute_test.go, specs/offpeak-charge-projection/design.md

- [ ] 2. Implement projectOffpeakEndSoc, charge constants, and the ProjectedEndSoc response field <!-- id:zugls52 -->
  - compute.go constants (mirror maxDischargeKW style): offpeakChargeRateKW=4.5, offpeakTrickleRateKW=0.5, fastChargeMaxSoc=95.0
  - projectOffpeakEndSoc(soc, capacityKwh, now, offpeakStart, offpeakEnd) *float64: closed-form two-rate curve; gate via withinOffpeakWindow and capacity>0; window-end = sydneyTZ midnight + endMin (as nextOffpeakStart); clamp to [soc,100]; roundPower to 1 dp
  - response.go: add ProjectedEndSoc *float64 `json:"projectedEndSoc"` to OffpeakData (pointer, no omitempty so absence serialises as explicit null)
  - Blocked-by: zugls51 (Write failing unit and property tests for projectOffpeakEndSoc)
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [1.5](requirements.md#1.5), [1.6](requirements.md#1.6), [1.7](requirements.md#1.7), [1.8](requirements.md#1.8), [1.9](requirements.md#1.9), [1.10](requirements.md#1.10), [2.1](requirements.md#2.1), [2.3](requirements.md#2.3), [3.1](requirements.md#3.1), [3.2](requirements.md#3.2)
  - References: internal/api/compute.go, internal/api/response.go

- [ ] 3. Write failing status-handler integration tests for the projection <!-- id:zugls53 -->
  - inside window + fresh live -> offpeak.projectedEndSoc present; outside window -> nil; stale live (no fresh reading) -> nil
  - simulateLoadWatts>0 returns the SAME projectedEndSoc as the unsimulated call (AC 2.4)
  - absent projection serialises as JSON null (no omitempty)
  - Blocked-by: zugls52 (Implement projectOffpeakEndSoc, charge constants, and the ProjectedEndSoc response field)
  - Stream: 1
  - Requirements: [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.4](requirements.md#2.4), [3.1](requirements.md#3.1), [3.2](requirements.md#3.2)
  - References: internal/api/status_test.go

- [ ] 4. Wire projectOffpeakEndSoc into the status handler <!-- id:zugls54 -->
  - After resp.Offpeak = buildOffpeak(...), on the liveFresh branch call projectOffpeakEndSoc(latest.Soc, capacity, now, h.offpeakStart, h.offpeakEnd) and assign to resp.Offpeak.ProjectedEndSoc
  - Reuse the existing `capacity` variable (status.go:125) for AC 1.4 parity with EstimatedCutoff — no second capacity lookup
  - Run make test and make lint
  - Blocked-by: zugls53 (Write failing status-handler integration tests for the projection)
  - Stream: 1
  - Requirements: [1.4](requirements.md#1.4), [2.2](requirements.md#2.2), [2.4](requirements.md#2.4), [3.1](requirements.md#3.1)
  - References: internal/api/status.go

- [ ] 5. Add projectedEndSoc to FluxCore OffpeakData and test decoding <!-- id:zugls55 -->
  - OffpeakData: add `public let projectedEndSoc: Double?` and a trailing init parameter `projectedEndSoc: Double? = nil` (default preserves the MockFluxAPIClient, OffPeakBlock, WidgetFixtures call sites)
  - FluxCore decode test: projectedEndSoc decodes for present, explicit null, and absent JSON; the property name must match the Go json tag "projectedEndSoc"
  - Type change (exempt from preceding test); bundled with its decode test as one cohesive change
  - Stream: 2
  - Requirements: [3.2](requirements.md#3.2)
  - References: Flux/Packages/FluxCore/Sources/FluxCore/Models/APIModels.swift

- [ ] 6. Write failing BatteryBlock test for the projection row and precedence <!-- id:zugls56 -->
  - With projectedOffpeakEndSoc set: the off-peak row label is "Projected at {windowEnd}" and value is SOCFormatting.format(projected); the "Charged during off-peak" delta row is suppressed
  - With projectedOffpeakEndSoc nil: behaviour unchanged (delta row per showsOffpeakDelta)
  - Expose the row-selection and label logic as internal computed properties so it is testable without view-rendering infrastructure
  - Stream: 2
  - Requirements: [4.1](requirements.md#4.1), [4.2](requirements.md#4.2), [4.3](requirements.md#4.3)
  - References: Flux/Flux/Helpers/BatteryBlock.swift

- [ ] 7. Implement the BatteryBlock projection row <!-- id:zugls57 -->
  - Add params projectedOffpeakEndSoc: Double? and offpeakWindowEnd: String?
  - Render the projection row INSTEAD of the delta row when a projection is present (mutually exclusive); "Lowest" row last: = !(projectedOffpeakEndSoc != nil || rendersOffpeakDelta)
  - Label "Projected at \(offpeakWindowEnd ?? "off-peak end")", value SOCFormatting.format(projected)
  - Blocked-by: zugls56 (Write failing BatteryBlock test for the projection row and precedence)
  - Stream: 2
  - Requirements: [4.1](requirements.md#4.1), [4.2](requirements.md#4.2), [4.3](requirements.md#4.3), [4.4](requirements.md#4.4)
  - References: Flux/Flux/Helpers/BatteryBlock.swift

- [ ] 8. Wire the projection through DashboardView and run app build/lint/test <!-- id:zugls58 -->
  - DashboardView batteryPanel: pass projectedOffpeakEndSoc: viewModel.status?.offpeak?.projectedEndSoc and offpeakWindowEnd: viewModel.status?.offpeak?.windowEnd
  - Run make macos-build, make macos-test, make macos-lint
  - Blocked-by: zugls57 (Implement the BatteryBlock projection row), zugls55 (Add projectedEndSoc to FluxCore OffpeakData and test decoding)
  - Stream: 2
  - Requirements: [3.3](requirements.md#3.3), [4.1](requirements.md#4.1), [4.4](requirements.md#4.4)
  - References: Flux/Flux/Dashboard/DashboardView.swift
