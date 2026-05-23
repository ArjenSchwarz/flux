---
references:
    - specs/daily-costs/requirements.md
    - specs/daily-costs/design.md
    - specs/daily-costs/decision_log.md
---
# Tasks: Daily Costs

## Backend

- [ ] 1. Add flux-pricing table, IAM policy, and TABLE_PRICING env var in infrastructure/template.yaml and cmd/api/main.go requiredEnvVars <!-- id:osag2li -->
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1)

- [ ] 2. Write internal/dynamo/pricing_test.go covering CRUD round-trip, ListPricing ordering and sentinel-row exclusion <!-- id:osag2lj -->
  - Blocked-by: osag2li (Add flux-pricing table, IAM policy, and TABLE_PRICING env var in infrastructure/template.yaml and cmd/api/main.go requiredEnvVars)
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.5](requirements.md#1.5), [2.5](requirements.md#2.5)

- [ ] 3. Implement internal/dynamo/pricing.go with PricingItem, PricingSentinel, PricingReadAPI, PricingWriteAPI and basic non-transactional CRUD <!-- id:osag2lk -->
  - Blocked-by: osag2lj (Write internal/dynamo/pricing_test.go covering CRUD round-trip, ListPricing ordering and sentinel-row exclusion)
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.5](requirements.md#1.5), [2.5](requirements.md#2.5)

- [ ] 4. Write internal/dynamo/pricing_atomicity_test.go enumerating the 10 TransactWriteItems failure shapes from the design <!-- id:osag2ll -->
  - Blocked-by: osag2lk (Implement internal/dynamo/pricing.go with PricingItem, PricingSentinel, PricingReadAPI, PricingWriteAPI and basic non-transactional CRUD)
  - Stream: 1
  - Requirements: [1.9](requirements.md#1.9), [2.3](requirements.md#2.3)

- [ ] 5. Implement sentinel-aware transactional writes (Put/Update/Delete sub-cases and ReplaceOpenEnded) in internal/dynamo/pricing.go <!-- id:osag2lm -->
  - Blocked-by: osag2ll (Write internal/dynamo/pricing_atomicity_test.go enumerating the 10 TransactWriteItems failure shapes from the design)
  - Stream: 1
  - Requirements: [1.9](requirements.md#1.9)

- [ ] 6. Write internal/api/pricing_test.go covering happy paths and every error code per AC 2.3, 2.4, 2.6, and the close-and-create endpoint <!-- id:osag2ln -->
  - Blocked-by: osag2lm (Implement sentinel-aware transactional writes (Put/Update/Delete sub-cases and ReplaceOpenEnded) in internal/dynamo/pricing.go)
  - Stream: 1
  - Requirements: [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.3](requirements.md#2.3), [2.4](requirements.md#2.4), [2.5](requirements.md#2.5), [2.6](requirements.md#2.6)

- [ ] 7. Implement internal/api/pricing_handler.go with validation chain (AC 1.10), TransactionCanceledException to HTTP mapping, and replace-open-ended handler <!-- id:osag2lo -->
  - Blocked-by: osag2ln (Write internal/api/pricing_test.go covering happy paths and every error code per AC 2.3, 2.4, 2.6, and the close-and-create endpoint)
  - Stream: 1
  - Requirements: [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [1.5](requirements.md#1.5), [1.6](requirements.md#1.6), [1.7](requirements.md#1.7), [1.8](requirements.md#1.8), [1.9](requirements.md#1.9), [1.10](requirements.md#1.10), [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.3](requirements.md#2.3), [2.4](requirements.md#2.4), [2.5](requirements.md#2.5), [2.6](requirements.md#2.6)

- [ ] 8. Wire pricing routes into internal/api/handler.go buildMux and add pricingStoreAdapter in cmd/api/main.go <!-- id:osag2lp -->
  - Blocked-by: osag2lo (Implement internal/api/pricing_handler.go with validation chain (AC 1.10), TransactionCanceledException to HTTP mapping, and replace-open-ended handler)
  - Stream: 1
  - Requirements: [2.1](requirements.md#2.1), [2.2](requirements.md#2.2)

## FluxCore — Models & Client

- [ ] 9. Write FluxCoreTests/PricingPeriodTests.swift covering Codable round-trip, YYYY-MM-DD string compare ordering, and equality <!-- id:osag2lq -->
  - Stream: 2
  - Requirements: [1.2](requirements.md#1.2), [1.3](requirements.md#1.3)

- [ ] 10. Implement FluxCore/Pricing/PricingPeriod.swift and PricingPeriodDraft.swift <!-- id:osag2lr -->
  - Blocked-by: osag2lq (Write FluxCoreTests/PricingPeriodTests.swift covering Codable round-trip, YYYY-MM-DD string compare ordering, and equality)
  - Stream: 2
  - Requirements: [1.2](requirements.md#1.2), [1.3](requirements.md#1.3)

- [ ] 11. Write FluxCoreTests/URLSessionAPIClientPricingTests.swift covering all five endpoints plus 400/401/404/409 error mapping <!-- id:osag2ls -->
  - Blocked-by: osag2lr (Implement FluxCore/Pricing/PricingPeriod.swift and PricingPeriodDraft.swift)
  - Stream: 2
  - Requirements: [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.3](requirements.md#2.3), [2.4](requirements.md#2.4)

- [ ] 12. Implement URLSessionAPIClient pricing extensions and FluxAPIError.pricingValidation cases (including concurrentWrite) <!-- id:osag2lt -->
  - Blocked-by: osag2ls (Write FluxCoreTests/URLSessionAPIClientPricingTests.swift covering all five endpoints plus 400/401/404/409 error mapping)
  - Stream: 2
  - Requirements: [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.3](requirements.md#2.3), [2.4](requirements.md#2.4)

- [ ] 13. Write FluxCoreTests/PricingServiceTests.swift covering refresh, mutation fold and fire-and-forget refetch <!-- id:osag2lu -->
  - Blocked-by: osag2lt (Implement URLSessionAPIClient pricing extensions and FluxAPIError.pricingValidation cases (including concurrentWrite))
  - Stream: 2
  - Requirements: [2.7](requirements.md#2.7)

- [ ] 14. Implement FluxCore/Pricing/PricingService.swift as @Observable @MainActor <!-- id:osag2lv -->
  - Blocked-by: osag2lu (Write FluxCoreTests/PricingServiceTests.swift covering refresh, mutation fold and fire-and-forget refetch)
  - Stream: 2
  - Requirements: [2.7](requirements.md#2.7)

## FluxCore — Cost Computation

- [ ] 15. Write FluxCoreTests/DayCostsTests.swift covering DaySummary.costs(forDate:in:) and DayEnergy.costs(in:), including the off-peak nil fallback to eInput <!-- id:osag2lw -->
  - Blocked-by: osag2lr (Implement FluxCore/Pricing/PricingPeriod.swift and PricingPeriodDraft.swift)
  - Stream: 2
  - Requirements: [4.1](requirements.md#4.1), [4.2](requirements.md#4.2), [4.3](requirements.md#4.3), [4.4](requirements.md#4.4), [4.5](requirements.md#4.5), [4.6](requirements.md#4.6)

- [ ] 16. Implement FluxCore/Pricing/DayCosts.swift with DaySummary and DayEnergy extensions <!-- id:osag2lx -->
  - Blocked-by: osag2lw (Write FluxCoreTests/DayCostsTests.swift covering DaySummary.costs(forDate:in:) and DayEnergy.costs(in:), including the off-peak nil fallback to eInput)
  - Stream: 2
  - Requirements: [4.1](requirements.md#4.1), [4.2](requirements.md#4.2), [4.3](requirements.md#4.3), [4.4](requirements.md#4.4), [4.5](requirements.md#4.5), [4.6](requirements.md#4.6), [6.1](requirements.md#6.1), [6.2](requirements.md#6.2)

- [ ] 17. Write FluxCoreTests/PeriodCostsTests.swift with property-based tests for overlap symmetry, cost linearity, and net invariant, plus partial-coverage and empty-range cases <!-- id:osag2ly -->
  - Blocked-by: osag2lx (Implement FluxCore/Pricing/DayCosts.swift with DaySummary and DayEnergy extensions)
  - Stream: 2
  - Requirements: [5.1](requirements.md#5.1), [5.2](requirements.md#5.2), [5.3](requirements.md#5.3), [5.4](requirements.md#5.4), [5.5](requirements.md#5.5)

- [ ] 18. Implement FluxCore/Pricing/PeriodCosts.swift with compute(days:pricing:) <!-- id:osag2lz -->
  - Blocked-by: osag2ly (Write FluxCoreTests/PeriodCostsTests.swift with property-based tests for overlap symmetry, cost linearity, and net invariant, plus partial-coverage and empty-range cases)
  - Stream: 2
  - Requirements: [5.1](requirements.md#5.1), [5.2](requirements.md#5.2), [5.3](requirements.md#5.3), [5.4](requirements.md#5.4), [5.5](requirements.md#5.5)

## Settings UI

- [ ] 19. Write PricingViewModel tests covering list/add/edit/delete, validation error surfacing, and the one-tap overlap remediation flow <!-- id:osag2m0 -->
  - Blocked-by: osag2lv (Implement FluxCore/Pricing/PricingService.swift as @Observable @MainActor)
  - Stream: 2
  - Requirements: [3.1](requirements.md#3.1), [3.4](requirements.md#3.4), [3.5](requirements.md#3.5), [3.6](requirements.md#3.6), [3.7](requirements.md#3.7)

- [ ] 20. Implement Flux/Flux/Settings/Pricing/PricingViewModel.swift wrapping PricingService <!-- id:osag2m1 -->
  - Blocked-by: osag2m0 (Write PricingViewModel tests covering list/add/edit/delete, validation error surfacing, and the one-tap overlap remediation flow)
  - Stream: 2
  - Requirements: [3.1](requirements.md#3.1), [3.4](requirements.md#3.4), [3.5](requirements.md#3.5), [3.6](requirements.md#3.6), [3.7](requirements.md#3.7)

- [ ] 21. Write snapshot tests for PricingPeriodsView covering empty, single open-ended, multiple sorted, and partially-loaded states <!-- id:osag2m2 -->
  - Blocked-by: osag2m1 (Implement Flux/Flux/Settings/Pricing/PricingViewModel.swift wrapping PricingService)
  - Stream: 2
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [3.8](requirements.md#3.8)

- [ ] 22. Implement Flux/Flux/Settings/Pricing/PricingPeriodsView.swift with list, add affordance, swipe-to-delete, and empty state <!-- id:osag2m3 -->
  - Blocked-by: osag2m2 (Write snapshot tests for PricingPeriodsView covering empty, single open-ended, multiple sorted, and partially-loaded states)
  - Stream: 2
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [3.7](requirements.md#3.7), [3.8](requirements.md#3.8), [3.9](requirements.md#3.9)

- [ ] 23. Write snapshot tests for PricingEditor covering create, edit, per-error-code banners, and the remediation surface <!-- id:osag2m4 -->
  - Blocked-by: osag2m1 (Implement Flux/Flux/Settings/Pricing/PricingViewModel.swift wrapping PricingService)
  - Stream: 2
  - Requirements: [3.4](requirements.md#3.4), [3.5](requirements.md#3.5), [3.6](requirements.md#3.6)

- [ ] 24. Implement Flux/Flux/Settings/Pricing/PricingEditor.swift (sheet with 4dp rate inputs, destructive delete with confirmation, one-tap remediation button) <!-- id:osag2m5 -->
  - Blocked-by: osag2m4 (Write snapshot tests for PricingEditor covering create, edit, per-error-code banners, and the remediation surface)
  - Stream: 2
  - Requirements: [3.4](requirements.md#3.4), [3.5](requirements.md#3.5), [3.6](requirements.md#3.6), [3.7](requirements.md#3.7)

- [ ] 25. Wire Settings entry on iOS and macOS in SettingsView.swift and instantiate PricingService at app startup (FluxApp.swift / FluxiOSAppDelegate.swift) <!-- id:osag2m6 -->
  - Blocked-by: osag2m3 (Implement Flux/Flux/Settings/Pricing/PricingPeriodsView.swift with list, add affordance, swipe-to-delete, and empty state), osag2m5 (Implement Flux/Flux/Settings/Pricing/PricingEditor.swift (sheet with 4dp rate inputs, destructive delete with confirmation, one-tap remediation button))
  - Stream: 2
  - Requirements: [3.1](requirements.md#3.1), [3.9](requirements.md#3.9)

## Day Detail Card

- [ ] 26. Write snapshot tests for CostsCard covering positive net, negative net, and zero off-peak savings <!-- id:osag2m7 -->
  - Blocked-by: osag2lx (Implement FluxCore/Pricing/DayCosts.swift with DaySummary and DayEnergy extensions)
  - Stream: 2
  - Requirements: [4.1](requirements.md#4.1), [4.7](requirements.md#4.7)

- [ ] 27. Implement Flux/Flux/DayDetail/CostsCard.swift (four rows, AUD formatting, leading minus on negative net) <!-- id:osag2m8 -->
  - Blocked-by: osag2m7 (Write snapshot tests for CostsCard covering positive net, negative net, and zero off-peak savings)
  - Stream: 2
  - Requirements: [4.1](requirements.md#4.1), [4.7](requirements.md#4.7)

- [ ] 28. Wire costs computation into DayDetailViewModel and render CostsCard below DayInFiveBlocksPanel in DayDetailView.swift <!-- id:osag2m9 -->
  - Blocked-by: osag2lv (Implement FluxCore/Pricing/PricingService.swift as @Observable @MainActor), osag2m8 (Implement Flux/Flux/DayDetail/CostsCard.swift (four rows, AUD formatting, leading minus on negative net))
  - Stream: 2
  - Requirements: [4.1](requirements.md#4.1), [4.6](requirements.md#4.6), [4.8](requirements.md#4.8), [6.2](requirements.md#6.2)

## History Period Costs Card

- [ ] 29. Write snapshot tests for HistoryPeriodCostsCard covering full coverage and partial coverage with the N-of-M caption <!-- id:osag2ma -->
  - Blocked-by: osag2lz (Implement FluxCore/Pricing/PeriodCosts.swift with compute(days:pricing:))
  - Stream: 2
  - Requirements: [5.1](requirements.md#5.1), [5.3](requirements.md#5.3), [5.4](requirements.md#5.4)

- [ ] 30. Implement Flux/Flux/History/HistoryPeriodCostsCard.swift (four tiles, partial-coverage caption) <!-- id:osag2mb -->
  - Blocked-by: osag2ma (Write snapshot tests for HistoryPeriodCostsCard covering full coverage and partial coverage with the N-of-M caption)
  - Stream: 2
  - Requirements: [5.1](requirements.md#5.1), [5.3](requirements.md#5.3)

- [ ] 31. Wire periodCosts computation into HistoryViewModel and render HistoryPeriodCostsCard below HistoryStatsOverviewCard in HistoryView.swift <!-- id:osag2mc -->
  - Blocked-by: osag2lv (Implement FluxCore/Pricing/PricingService.swift as @Observable @MainActor), osag2mb (Implement Flux/Flux/History/HistoryPeriodCostsCard.swift (four tiles, partial-coverage caption))
  - Stream: 2
  - Requirements: [5.1](requirements.md#5.1), [5.4](requirements.md#5.4), [5.5](requirements.md#5.5), [6.2](requirements.md#6.2)
