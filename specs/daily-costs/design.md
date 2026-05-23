# Design: Daily Costs

## Overview

Add a single shared pricing-period configuration (CRUD on the Lambda; one new DynamoDB table) and a pure-Swift cost computation in FluxCore that consumes the pricing list together with the existing daily-energy figures to render a four-row costs card on Day Detail and a four-tile "Period costs" card on History. No `/day` or `/history` response changes.

## Architecture

```
              ┌───────────────────────┐
              │   flux-pricing (DDB)  │
              │  PK: pricingId        │
              └───────────▲───────────┘
                          │ TransactWriteItems / Put / Update / Delete / Scan
              ┌───────────┴───────────┐
              │  internal/dynamo/      │
              │     pricing.go         │
              └───────────▲───────────┘
                          │ PricingReadAPI / PricingWriteAPI
              ┌───────────┴───────────┐
              │  internal/api/         │
              │  pricing_handler.go    │
              └───────────▲───────────┘
                          │ HTTP — 5 routes on the existing http.ServeMux
                          │   GET    /pricing
                          │   POST   /pricing
                          │   PUT    /pricing/{id}
                          │   DELETE /pricing/{id}
                          │   POST   /pricing/replace-open-ended
              ┌───────────┴───────────┐
              │  URLSessionAPIClient   │  ─►  PricingService (ObservableObject)
              └───────────┬───────────┘            │
                          │                       │ pricing: [PricingPeriod]
                          ▼                       ▼
              ┌─────────────────────────────────────────────┐
              │  FluxCore/Pricing/                          │
              │   PricingPeriod.swift                       │
              │   PricingPeriodDraft.swift                  │
              │   DayCosts.swift     ← pure compute         │
              │   PeriodCosts.swift  ← pure compute         │
              └─────────────────────────────────────────────┘
                          │                       │
                          ▼                       ▼
                  Day Detail card          History "Period costs" card
```

Integration points (with file:line anchors from precedent):

| Integration | File | Anchor |
|---|---|---|
| New table resource | `infrastructure/template.yaml` | mirror `NotesTable` (lines 447–467); insert after `SocFireStateTable` (~line 529) |
| Lambda IAM | `infrastructure/template.yaml` | new `Effect: Allow` block in `FluxLambdaPolicy` (~line 294) |
| Lambda env var | `infrastructure/template.yaml` | `TABLE_PRICING: !Ref PricingTable` in `ApiFunction.Environment.Variables` (~line 561) |
| Required env vars | `cmd/api/main.go:35–49` | add `"TABLE_PRICING"` to `requiredEnvVars` |
| Mux wiring | `internal/api/handler.go:71–83` | five `mux.HandleFunc(...)` calls inside `buildMux()` |
| Handler dependency injection | `cmd/api/main.go:51–65` | new `pricingStoreAdapter` bridging `PricingReadAPI` + `PricingWriteAPI` |
| Settings entry (iOS) | `Flux/Flux/Settings/SettingsView.swift:119–125` | new `Section("Pricing")` matching SoC Alerts shape |
| Settings entry (macOS) | `Flux/Flux/Settings/SettingsView.swift:224–235` | new `LiquidGlassSection`/`FormRow` row |
| Day Detail card placement | `Flux/Flux/DayDetail/DayDetailView.swift` | inserted directly below `DayInFiveBlocksPanel` |
| History card placement | `Flux/Flux/History/HistoryView.swift` | inserted directly below `HistoryStatsOverviewCard` |

The poller is untouched: pricing is read-only for the poller (and not even that — the poller has no business with pricing). Only the Lambda touches `flux-pricing`.

## Components and Interfaces

### Backend — `internal/dynamo/pricing.go`

```go
type PricingItem struct {
    PricingID            string  `dynamodbav:"pricingId" json:"id"`
    StartDate            string  `dynamodbav:"startDate" json:"startDate"`            // YYYY-MM-DD, Melbourne
    EndDate              *string `dynamodbav:"endDate,omitempty" json:"endDate,omitempty"`
    PeakRate             float64 `dynamodbav:"peakRate" json:"peakRate"`              // AUD/kWh, 4dp
    FeedInRate           float64 `dynamodbav:"feedInRate" json:"feedInRate"`
    OffPeakSavingsRate   float64 `dynamodbav:"offPeakSavingsRate" json:"offPeakSavingsRate"`
    CreatedAt            string  `dynamodbav:"createdAt" json:"createdAt"`            // RFC3339
    UpdatedAt            string  `dynamodbav:"updatedAt" json:"updatedAt"`
}

// PricingSentinel pins which row is currently open-ended. Singleton with PK = "__open_ended".
// Every write that introduces, retires, or replaces the open-ended period maintains this row
// inside the same TransactWriteItems request, with a ConditionExpression on its previous value.
// This is what makes AC 1.9 ("at most one open-ended period") race-safe.
type PricingSentinel struct {
    PricingID    string  `dynamodbav:"pricingId"`        // always "__open_ended"
    OpenEndedID  *string `dynamodbav:"openEndedId,omitempty"`
    UpdatedAt    string  `dynamodbav:"updatedAt"`
}

type PricingReadAPI interface {
    ListPricing(ctx context.Context) ([]PricingItem, error) // excludes the sentinel row
    GetPricing(ctx context.Context, id string) (*PricingItem, error)
    GetSentinel(ctx context.Context) (*PricingSentinel, error) // for the validator pass
}

type PricingWriteAPI interface {
    PutPricing(ctx context.Context, item PricingItem, prevOpenEndedID *string) error
    UpdatePricing(ctx context.Context, item PricingItem, prevOpenEndedID *string) error
    DeletePricing(ctx context.Context, id string, prevOpenEndedID *string) error
    ReplaceOpenEnded(ctx context.Context, closingID string, closingEndDate string, newItem PricingItem) error
}
```

Behavioural contracts:

- `ListPricing` returns items sorted by `StartDate` ascending and **filters out the sentinel row** (`pricingId = "__open_ended"`). The API never exposes the sentinel.
- Every write that touches an open-ended period maintains the sentinel inside the same `TransactWriteItems` request. `prevOpenEndedID` is the validator's snapshot of the sentinel just before the write; the sentinel `ConditionExpression` is `(attribute_not_exists(pricingId) OR openEndedId = :prevOpenEndedID)` — the first clause lets the very first write create the sentinel, and the second clause catches concurrent writers on every subsequent write.

**`PutPricing` transaction shapes**:

| Case | Items in transaction | Sentinel target |
|---|---|---|
| New closed period | Plain `PutItem` (no transaction) | — |
| New open-ended period | (1) Sentinel Update; (2) Put new row | `null → newID` |

**`UpdatePricing` transaction shapes** (the design-critic's four sub-cases):

| Case | Items in transaction | Sentinel target |
|---|---|---|
| closed → closed (rate-only edit on a closed period) | Plain `UpdateItem` (no transaction) | — |
| closed → open (extending an existing closed period to be open-ended) | (1) Sentinel Update; (2) Row Update | `null → rowID` |
| open → closed (capping the open-ended period with an end date) | (1) Sentinel Update; (2) Row Update | `rowID → null` |
| open → open (rate-only edit on the open-ended row) | (1) Sentinel Update (no-op write, asserts `openEndedId = rowID`); (2) Row Update | `rowID → rowID` |

The open→open case still needs the sentinel ConditionCheck even though the value doesn't change — otherwise a concurrent closed→open could leave the sentinel pointing at a closed row.

**`DeletePricing` transaction shapes**:

| Case | Items in transaction | Sentinel target |
|---|---|---|
| Delete a closed period | Plain `DeleteItem` (no transaction) | — |
| Delete the open-ended period | (1) Sentinel Update; (2) Delete row | `rowID → null` |

**`ReplaceOpenEnded` transaction** (always three items): (1) Sentinel Update `closingID → newItem.PricingID` (or `→ null` if `newItem.EndDate != nil`); (2) Closing-row Update with `ConditionExpression: attribute_not_exists(endDate) AND pricingId = :closingId`; (3) Put new row with `ConditionExpression: attribute_not_exists(pricingId)`.

The atomicity guarantee from this design covers AC 1.9 in full (single open-ended period). It does NOT cover AC 1.7 (no overlap) for concurrent *closed* period creates — two concurrent writers that both pass the validator's overlap check on the same uncovered date range would both succeed. For the two-user Flux deployment this race is implausible and recoverable; the design accepts it as a documented limit. If AC 1.7 ever needs hard race-safety, the same sentinel pattern can be extended with a generation counter that every closed-period write must increment.

### Backend — `internal/api/pricing_handler.go`

One handler per route. Validation chain per AC 1.10:
```
inverted_dates → overlap → rate_precision → rate_out_of_range → second_open_ended
```
Validation runs server-side only. The client may pre-check for UX but never as the only check.

Overlap detection: load all pricing rows (small table, low volume), build `[]dateRange{start, end}` (open-ended modelled as `end = nil`), check the candidate against each — exclude the row's own id on update. O(n) check, n ≤ ~50 for a decade of use.

Error response shape: `{"error": "<code>", "message": "<one-line>"}` — HTTP 400 for all validation errors, 401 for auth, 404 for unknown id on delete/update, 500 for storage errors.

### Backend — atomic `replace-open-ended` endpoint

Request body:
```json
{ "closingPricingId": "uuid", "newPeriod": { "startDate": "2026-05-23", "endDate": null, "peakRate": ..., "feedInRate": ..., "offPeakSavingsRate": ... } }
```

The handler derives the closing row's new `endDate` as `newPeriod.startDate − 1 day` in `Australia/Melbourne` (no `closeAt` field on the wire — the offset is fixed by AC 3.6), reads the sentinel to capture `prevOpenEndedID`, runs the full validation chain against the resulting two-row state, then calls `ReplaceOpenEnded`. `TransactionCanceledException` is mapped back per the table below.

Decision: clients invoke this endpoint only from the editor's "close current open-ended period" remediation flow; the normal create path uses `POST /pricing`.

### `TransactionCanceledException` → HTTP mapping

`TransactWriteItems` returns a per-item `Reasons[]` array on cancellation. Mapping:

| Item index | Condition that failed | HTTP | Code |
|---|---|---|---|
| 0 (sentinel) | `openEndedId != prevOpenEndedID` — another writer raced this one | 409 | `concurrent_open_ended_write` |
| 1 (closing row, replace-open-ended only) | `attribute_not_exists(endDate)` failed — row was closed since validator scan | 409 | `concurrent_open_ended_write` |
| 1 or 2 (new row) | `attribute_not_exists(pricingId)` failed — UUID collision | 500 | `internal_error` (caller retries with a fresh UUID) |

A `Reasons` array that is empty or has all-`None` entries falls through to HTTP 500; the handler logs the raw exception. The editor's overlap-remediation flow treats 409 as "refetch the list and retry" — the same UX as any optimistic-concurrency miss.

### FluxCore — pricing models

```swift
public struct PricingPeriod: Identifiable, Codable, Sendable, Equatable, Hashable {
    public let id: String
    public let startDate: String         // YYYY-MM-DD, Melbourne local calendar
    public let endDate: String?
    public let peakRate: Double
    public let feedInRate: Double
    public let offPeakSavingsRate: Double
    public let createdAt: Date           // RFC3339, decoded as `Date`
    public let updatedAt: Date
}

public struct PricingPeriodDraft: Codable, Sendable, Equatable {
    public let startDate: String
    public let endDate: String?
    public let peakRate: Double
    public let feedInRate: Double
    public let offPeakSavingsRate: Double
}
```

Dates are stored as `YYYY-MM-DD` strings to match `DayEnergy.date` (which is `String`, not `Date`, per `APIModels.swift`). Day-membership tests use lexicographic string comparison, which is correct for ISO-formatted `YYYY-MM-DD` strings. No `Date` round-trip is needed for the priced-day predicate. `createdAt` / `updatedAt` decode as `Date` since they're not used for day arithmetic, only for ordering and debugging.

Numeric fields use Swift `Double`; precision loss at four decimals over 30 days is sub-cent (Decision 20).

### FluxCore — `DayCosts`, `PeriodCosts`

`DaySummary` (per `APIModels.swift:375`) is the type held by `DayDetailViewModel.summary`. It has no `date` field, so the cost lookup takes the date as a separate argument. `DayEnergy` (held by `HistoryViewModel.days`) carries `date` inline.

```swift
public struct DayCosts: Equatable, Sendable {
    public let peakImportsCost: Double
    public let solarFeedInIncome: Double
    public let net: Double
    public let offPeakSavings: Double
}

public extension DaySummary {
    /// Day Detail call site. Returns nil when no pricing period covers
    /// `date`. Zero-valued kWh fields produce zero cost lines and do NOT
    /// make the day unpriced (Decision 18).
    func costs(forDate date: String, in pricing: [PricingPeriod]) -> DayCosts?
}

public extension DayEnergy {
    /// History per-day call site. Convenience wrapper that forwards
    /// `self.date` to the `DaySummary` extension above; History accumulates
    /// over `[DayEnergy]` so this is what `PeriodCosts.compute` calls.
    func costs(in pricing: [PricingPeriod]) -> DayCosts?
}

public struct PeriodCosts: Equatable, Sendable {
    public let peakImportsCost: Double
    public let solarFeedInIncome: Double
    public let net: Double
    public let offPeakSavings: Double
    public let pricedDayCount: Int
    public let totalDayCount: Int
    public var hasPartialCoverage: Bool { pricedDayCount < totalDayCount }
}

public extension PeriodCosts {
    /// Builds totals from the daily summaries. Returns nil iff no day in
    /// `days` is a priced day, matching AC 5.4.
    static func compute(days: [DayEnergy], pricing: [PricingPeriod]) -> PeriodCosts?
}
```

Implementation notes (non-obvious):

1. **Period selection**: the extensions pick the single covering period using `startDate <= date <= (endDate ?? "9999-12-31")` on string compare. Overlap is impossible by AC 1.7 between pricing periods, so the first match is the only match. Returns `nil` when no period covers the day.

2. **kWh resolution from `DaySummary` (resolves the off-peak split ambiguity per Decisions 9, 16, 18)**:
   - When `summary.offpeakGridImportKwh != nil`:
     - peak imports kWh = `(summary.eInput ?? 0) − (summary.offpeakGridImportKwh ?? 0)`, clamped to ≥0.
     - off-peak kWh = `summary.offpeakGridImportKwh ?? 0`.
   - When `summary.offpeakGridImportKwh == nil`:
     - peak imports kWh = `summary.eInput ?? 0` (all grid imports billed as peak, since the split is unknown).
     - off-peak kWh = `0` → off-peak savings $0.00.
   Solar export kWh = `summary.eOutput ?? 0` in both cases.

3. **Net**: `peakImportsCost - solarFeedInIncome`. Off-peak savings is excluded from net (AC 4.5).

4. **`DaySummary` does not currently have a `peakGridImportKwh` convenience accessor**. The cost computation derives the peak-imports value inline from `eInput` and `offpeakGridImportKwh`. No new field on `DaySummary` is added — keeps the data model unchanged.

### FluxCore — `URLSessionAPIClient` extensions

Mirror the SoC Alerts shape (file:`URLSessionAPIClient.swift:86–130`):

```swift
public func fetchPricing() async throws -> [PricingPeriod]
public func createPricing(_ draft: PricingPeriodDraft) async throws -> PricingPeriod
public func updatePricing(id: String, _ draft: PricingPeriodDraft) async throws -> PricingPeriod
public func deletePricing(id: String) async throws
public func replaceOpenEndedPricing(closingId: String, with draft: PricingPeriodDraft) async throws -> PricingPeriod
```

`replaceOpenEndedPricing` takes only `closingId` plus the new draft; the server derives the closing row's end date as `draft.startDate − 1 day` (no `closeAt` on the wire — AC 3.6 fixes the offset at 1 day).

Error mapping: 400 responses get surfaced as `FluxAPIError.pricingValidation(.invertedDates / .overlap(openEndedId:) / .ratePrecision / .rateOutOfRange / .secondOpenEnded)`. 409 responses with code `concurrent_open_ended_write` become `FluxAPIError.pricingValidation(.concurrentWrite)`; the editor handles this by `await service.refresh()` and prompting the user to retry.

### FluxCore — `PricingService` (`@Observable @MainActor`)

`@Observable` class (Swift 6 macro), `@MainActor`-isolated (matches `SoCAlertsService` and the project's Swift rules for view-bound observables). iOS 26 / macOS 26 baseline. Holds the canonical `var pricing: [PricingPeriod]`. Refresh policy (AC 2.7):

- Settings opens Pricing pane → `refresh()`.
- Day Detail appears → `refresh()` (one HTTP call per day-detail-open).
- History range changes → `refresh()`.
- After any mutating call → the service folds the response into the local list **and** calls `refresh()` once more in the background; this gives the editor immediate UI feedback while still satisfying AC 2.7's "re-fetch the pricing list … immediately after any mutating call." The second fetch is fire-and-forget and resolves race conditions with concurrent writers on another device.

The service injects into `DayDetailViewModel`, `HistoryViewModel`, and the Settings flow via the existing app composition (mirror `SoCAlertsService` wiring at app startup in `FluxApp.swift` / `FluxiOSAppDelegate.swift`).

### iOS / macOS UI — Settings

Files (new, under `Flux/Flux/Settings/Pricing/`):
- `PricingPeriodsView.swift` — list, mirrors `SoCAlertsView.swift`
- `PricingEditor.swift` — sheet, mirrors `SoCAlertEditor.swift`; rate inputs use a `Decimal` formatter at 4 dp
- `PricingViewModel.swift` — wraps `PricingService`

The editor's "close-and-create" remediation is a one-tap button that surfaces only when create returns `overlap` with `overlapTarget = openEndedId`. The button calls `replaceOpenEndedPricing`.

### iOS / macOS UI — Day Detail card

New file: `Flux/Flux/DayDetail/CostsCard.swift`. Four rows in a fixed order (peak imports, solar income, net, off-peak savings). Two-column layout: label on left, value right-aligned. Currency: `$X.XX`. Negative net: leading `−` (existing Day Detail typographic treatment).

Wiring: `DayDetailViewModel` takes `pricing: [PricingPeriod]` from `PricingService` and computes `var costs: DayCosts?` on the fly from `dayEnergy`. The view renders `CostsCard(costs:)` only when `costs != nil`.

### iOS / macOS UI — History "Period costs" card

New file: `Flux/Flux/History/HistoryPeriodCostsCard.swift`. Four tiles in a 2×2 grid on iPhone, 1×4 on iPad/macOS — same `StatTile` flavour used by `HistoryStatsOverviewCard`. Partial-coverage caption sits beneath the four tiles as a single line in the card's `tertiaryText` treatment.

Cost totals are **not** folded into `HistoryViewModel.PeriodSummary`. The reason is signature: `DerivedState.init(days:now:)` does not currently take pricing, and threading a `pricing: [PricingPeriod]` parameter through every call site is more disruptive than the costs feature warrants. Instead:

- Costs are computed in a separate second pass via `PeriodCosts.compute(days:pricing:)` (defined in FluxCore, see above).
- `HistoryViewModel` exposes a computed `var periodCosts: PeriodCosts?` that runs `compute` over the current `days` + `pricingService.pricing`. Re-runs whenever the active range changes, on view appear, and on pricing mutation (via the `@Observable` registration of `pricingService.pricing`).
- `HistoryView` renders `HistoryPeriodCostsCard(costs:)` only when `periodCosts != nil`.

`HistoryViewModel.PeriodSummary` itself is unchanged.

### Pattern-extension audit

| Existing pattern | Touch site needed for daily-costs? |
|---|---|
| `Section("Alerts")` in `SettingsView.swift` (iOS + macOS) | Yes — add parallel `Section("Pricing")` immediately above the existing Alerts section so both configuration entries sit together |
| `HistoryViewModel.PeriodSummary` totals (`HistoryDerivedState.swift`) | **No** — costs are computed in a separate `PeriodCosts.compute` pass; `PeriodSummary` is unchanged |
| `DerivedState.init(days:now:)` signature | **No** — kept stable; pricing is consumed only by the separate cost pass |
| `SettingsViewModel` injection | Yes — add `pricingService` ref |
| `DayDetailViewModel` injection | Yes — add `pricingService` ref |
| `HistoryViewModel` injection | Yes — add `pricingService` ref, expose computed `periodCosts: PeriodCosts?` |
| `MacRefreshCoordinator` refresh tiers | No — pricing changes don't need refresh tier; just trigger `refresh()` on view appear |
| `URLSessionAPIClient.interpret(...)` | Yes — add 400-with-`pricing*` and 409-with-`concurrent_open_ended_write` error mapping |
| `FluxiOSAppDelegate` / app composition | Yes — instantiate `PricingService` at app startup, hand into the three view models (mirror `SoCAlertsService` wiring) |
| `cmd/api/main.go` `requiredEnvVars` | Yes — add `TABLE_PRICING` |
| Adapter structs in `cmd/api/main.go:62–64` (`socRuleStoreAdapter` style) | Yes — add `pricingStoreAdapter` |
| `internal/api/handler.go` `buildMux()` | Yes — add five routes |
| `internal/api/handler.go` shared dependencies (`PricingStore` field) | Yes |
| Lambda integration tests (`internal/api/*_test.go`) | Yes — new `pricing_test.go` + `pricing_atomicity_test.go` |
| Backfill CLI (`cmd/backfill-solar` shape) | No — pricing has no derived persistence |

## Data Models

Only one new model: `flux-pricing` table (above). No changes to existing tables. No changes to existing API response shapes. No FluxCore model changes outside the new `Pricing/` directory.

CloudFormation outline (insert after `SocFireStateTable`):

```yaml
PricingTable:
  Type: AWS::DynamoDB::Table
  DeletionPolicy: Retain
  Properties:
    TableName: flux-pricing
    BillingMode: PAY_PER_REQUEST
    AttributeDefinitions:
      - AttributeName: pricingId
        AttributeType: S
    KeySchema:
      - AttributeName: pricingId
        KeyType: HASH
    PointInTimeRecoverySpecification:
      PointInTimeRecoveryEnabled: true
```

### Sentinel-row initial provisioning

The sentinel row (`pricingId = "__open_ended"`) is created lazily on first write that needs it. The ConditionExpression on every sentinel-touching write is:

```
ConditionExpression: attribute_not_exists(pricingId) OR openEndedId = :prevOpenEndedID
```

The first transactional write that creates an open-ended period passes `:prevOpenEndedID = null` (anything works in the second clause; the first clause carries the write). The same expression catches every concurrent writer thereafter. No CloudFormation custom resource is needed.

If the sentinel row genuinely does not exist before the very first read, `GetSentinel` returns `nil` and the validator treats that as "no open-ended period exists" — equivalent to a sentinel row with `openEndedId = null`. The `attribute_not_exists` clause in the ConditionExpression keeps the first write race-safe between two cold-starting Lambdas: both try to create the row, the loser fails with `ConditionalCheckFailed` (mapped to 409 / refetch / retry).

## Error Handling

| Failure mode | HTTP | Error code | Surfaced as |
|---|---|---|---|
| Missing/wrong bearer token | 401 | — | `FluxAPIError.unauthorized` |
| Validation: end < start | 400 | `inverted_dates` | `FluxAPIError.pricingValidation(.invertedDates)` |
| Validation: overlap | 400 | `overlap` | `.pricingValidation(.overlap(openEndedId: ...))` so editor can offer remediation |
| Validation: >4 dp | 400 | `rate_precision` | `.pricingValidation(.ratePrecision)` |
| Validation: rate out of range | 400 | `rate_out_of_range` | `.pricingValidation(.rateOutOfRange)` |
| Validation: second open-ended | 400 | `second_open_ended` | `.pricingValidation(.secondOpenEnded)` |
| Delete / update unknown id | 404 | — | `.notFound` |
| Sentinel ConditionCheck fail or closing-row ConditionCheck fail | 409 | `concurrent_open_ended_write` | `.pricingValidation(.concurrentWrite)` — editor refetches the list and retries |
| Unexpected `TransactionCanceledException` (incl. UUID collision, empty `Reasons[]`) | 500 | `internal_error` | `.serverError` (existing) |
| Storage / unexpected | 500 | — | `.serverError` (existing) |

Non-obvious: `overlap` carries the offending row's id back to the editor when (and only when) the offender is the unique open-ended period. The editor then surfaces the one-tap close-and-create button.

## Testing Strategy

### Go (backend)

- `internal/dynamo/pricing_test.go`:
  - Round-trip `PutPricing` / `GetPricing` / `UpdatePricing` / `DeletePricing` for both closed and open-ended periods.
  - `ListPricing` ordering invariant (start-date ascending) and sentinel-row exclusion.
  - Sentinel maintenance: every transactional write leaves the sentinel pointing at the unique open-ended row (or `null` when none exists).
- `internal/dynamo/pricing_atomicity_test.go` — `TransactWriteItems` failure shapes:
  1. **Sentinel race on replace-open-ended** — another writer flipped the sentinel between validator scan and transaction. ConditionCheck on `openEndedId = :prevOpenEndedID` fails → `Reasons[0] = ConditionalCheckFailed`. Assert: both rows untouched, sentinel untouched, handler returns HTTP 409 `concurrent_open_ended_write`.
  2. **Closing-row race on replace-open-ended** — the closing row's `attribute_not_exists(endDate)` fails because another writer just closed it. `Reasons[1] = ConditionalCheckFailed`. Assert: HTTP 409.
  3. **UUID collision** on new row — `attribute_not_exists(pricingId)` fails. `Reasons[2] = ConditionalCheckFailed`. Assert: HTTP 500 `internal_error`.
  4. **`Reasons` array unpopulated** — SDK quirk path. Assert: HTTP 500 with the raw `TransactionCanceledException` message logged and no partial state visible in a subsequent `ListPricing`.
  5. **PutPricing for a new open-ended period — sentinel race**: `openEndedId != null` at transaction time. Assert: HTTP 409, no new row created.
  6. **UpdatePricing closed→open — sentinel race**: same as case 5. Assert: row update reverts, HTTP 409.
  7. **UpdatePricing open→closed — sentinel race**: `openEndedId != rowID`. Assert: row update reverts, HTTP 409.
  8. **UpdatePricing open→open (rate-only edit) — sentinel race**: `openEndedId != rowID` because a concurrent writer flipped it. Assert: row update reverts, HTTP 409.
  9. **DeletePricing of open-ended row — sentinel race**: same as case 7. Assert: row not deleted, HTTP 409.
  10. **First-write sentinel creation race**: two callers both observe `GetSentinel = nil` and both submit a transaction with `attribute_not_exists(pricingId)`. Assert: one succeeds, the other gets HTTP 409, no partial state.
- At least one integration test against DynamoDB Local exercising cases 1, 5, and 8 — unit tests with mocks cannot prove DynamoDB-side atomicity.
- `internal/api/pricing_handler_test.go`: per-endpoint happy + sad paths covering every error code (`inverted_dates`, `overlap`, `rate_precision`, `rate_out_of_range`, `second_open_ended`, plus the 409 race codes); 401 path; 404 on delete unknown id; `POST /pricing/replace-open-ended` happy path and full `Reasons[]` mapping coverage.
- Integration: `internal/integration/` if it exists — otherwise per-handler unit tests against the in-memory store like SoC Alerts.

### Swift (FluxCore)

- `DayCostsTests`:
  - Linearity: `costs(scaleRate, kWh) = scale × costs(rate, kWh)` for each line.
  - Zero kWh → zero cost on each line.
  - Net definition: `net == peakImportsCost − solarFeedInIncome` (off-peak savings excluded).
  - Day with no covering period returns `nil`.
  - Day with covering period but missing aggregate row → `nil` (model exposes this via `DayEnergy?` at the call site; covered by a separate "missing row" test).
- `PeriodCostsTests`:
  - `pricedDayCount` counts exactly the days with covering pricing.
  - `hasPartialCoverage` matches the AC 5.3 caption.
  - `nil` result when zero priced days in the range (AC 5.4).
- `URLSessionAPIClientPricingTests`: stub responses for each endpoint; 400 with each error code → typed `pricingValidation`.

### Property-based tests (Swift Testing's `@Test` + Gen + Sourcery-free hand-written generators, mirroring existing `SoCAlertRuleTests` style)

- **Overlap symmetry**: for any two periods `A`, `B`, `overlaps(A, B) == overlaps(B, A)`. Open-ended modelled as `endDate = nil`.
- **Cost linearity**: for any rate `r`, kWh `k`, `cost(r, k) = r × k`; properties: commutativity, `cost(0, k) == 0`, `cost(r, 0) == 0`.
- **`PeriodCosts.net` invariant**: `period.net == Σ day.net` over priced days.

Property tests run in `FluxCoreTests`; use `swift-testing`'s parameterised `@Test` with generated arrays of `(Double, Double)` for rates and kWh values, bounded by AC 1.8's `[0, 10.0]` for rates and `[0, 1000]` for kWh.

### iOS / macOS UI

- `PricingPeriodsView`: snapshot tests for `empty`, `one open-ended period`, `multiple periods sorted` states.
- `PricingEditor`: snapshot tests for `create` and `edit` modes; one snapshot per error banner.
- `CostsCard`: snapshot tests for positive net, negative net, and zero off-peak savings (no fractional dollars).
- `HistoryPeriodCostsCard`: full coverage (no caption) vs partial coverage (caption present) snapshots.

### Manual

- iCloud two-device exercise: edit pricing on iOS, verify macOS reflects the change after re-opening Day Detail / History (covers AC 2.7).
- Overlap remediation: trigger from editor with an open-ended period; confirm both rows land in one atomic transaction.
- Pricing-then-rate-change: set pricing, view Day Detail, edit the rate, confirm the same day re-renders with new figures.

## Operational notes

- **Lambda cold start**: pricing CRUD shares the existing Lambda binary, so cold-start latency for `/day` and `/history` is unchanged. The new routes pull pricing rows on first call after a cold start — one `Scan` (≤50 rows) plus one `GetItem` for the sentinel. Negligible add to the existing init cost.
- **DynamoDB cost**: pay-per-request billing means the pricing table consumes capacity only on actual reads/writes. At realistic usage (a handful of edits across the lifetime of the feature, plus one `Scan` + one `GetSentinel` per Settings-open and per Day Detail / History refresh on each of two devices), monthly spend is well under a cent. PITR on a sub-KB table adds a similar negligible cost.
- **Migration**: none. The new table is created empty by the CloudFormation deploy; the sentinel row is created lazily on the first pricing write.
