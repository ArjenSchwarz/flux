# Design: SoC Alerts

## Overview

Server-side threshold evaluator inside the existing Go poller dispatches APNs pushes when a per-device rule fires. Three new DynamoDB tables (`flux-devices`, `flux-soc-rules`, `flux-soc-fire-state`) hold device registrations, rule definitions, and per-fire dedup state. A new `Notifications` module in `FluxCore` plus a `SoCAlerts` Settings screen drives registration and rule CRUD over five new Lambda endpoints.

## Architecture

### Backend wiring

```
                                                  ┌──────────────────────┐
ECS Fargate poller (Go) ── fetchAndStoreLiveData ─▶ evaluator.Evaluate() │── push enqueue ──▶ apns worker pool ──▶ APNs
                              │                   │      │                                                   │
                              ▼                   │      └─ conditional PutItem ──▶ flux-soc-fire-state     │
                       flux-readings              │                                                          ▼
                                                  └─────────────────────────────────────── on 410/BadDeviceToken: poller marks device stale
                                                  └─────────────────────────────────────── midnight pass: cascade-delete 30-day orphans

Lambda API ─ http.ServeMux (path params) ─ /devices ─────────▶ flux-devices
                                          /devices/{id}/rules ▶ flux-soc-rules
                                                              ─ on edit/delete: Query+Delete fire-state for cleared windows
```

The evaluator runs inside `fetchAndStoreLiveData` (`internal/poller/poller.go:175`) immediately after `WriteReading` succeeds. It computes which rules would fire, writes fire-state rows synchronously (so a crash before push leaves at most a silent miss), and **enqueues** push work onto a bounded worker pool — the live-poll goroutine never blocks on APNs.

### Per-cycle budget and back-pressure

`Evaluate` is bounded by `context.WithTimeout(ctx, 3*time.Second)`. Within that window:
- Cache refresh (devices+rules, see below) is at most one Scan + N Queries.
- Per-rule work is in-memory predicate + at most one conditional `PutItem` on fire-state.
- `PushQueue.Enqueue(ctx, job)` is non-blocking until the queue is full (capacity 64). When full, `Enqueue` returns an error; the cycle drops the push, leaves the fire-state row in place (no re-fire today), and logs `flux_apns_queue_overflow`.

The worker pool (4 goroutines) drains the queue; each worker calls `Notifier.Push` with retry, then handles the stale-token / observability bookkeeping.

This decouples APNs RTT (10s–seconds) from the AlphaESS poll cadence (10s), so `pollLoop`'s `time.NewTicker` never coalesces ticks behind a slow push.

### Integration points (Go)

| Site | File | Change |
|---|---|---|
| Poller live-data path | `internal/poller/poller.go:175` (`fetchAndStoreLiveData`) | After `p.store.WriteReading(...)`, call `p.evaluator.Evaluate(ctx, soc, now)`. Errors logged, not returned. |
| Poller construction | `internal/poller/poller.go:48` (`New`) | Accept `Evaluator` and `PushQueue`. `cmd/poller/main.go` wires real implementations; tests inject fakes or no-ops. |
| Midnight pass | `internal/poller/poller.go:225` (`runMidnightFinalizer`) | New step: orphan device GC, see §Garbage Collection. |
| Lambda routing | `internal/api/handler.go:80` (`handle`) | Replace the per-method switch with a single `http.ServeMux` (Go 1.22+ path-param syntax). Existing four routes are migrated unchanged. |
| Lambda construction | `internal/api/handler.go:40` (`NewHandler`) | Accept device/rule reader+writer interfaces and a fire-state cleaner interface. Reuse the existing bearer-token guard via middleware. |

### Integration points (Swift)

| Site | File | Change |
|---|---|---|
| iOS app delegate | New file `Flux/Flux/iOS/FluxiOSAppDelegate.swift` | Move `FluxiOSAppDelegate` out of `OrientationLock.swift` into its own file (current placement is a junk-drawer artifact). Add `application(_:didRegisterForRemoteNotificationsWithDeviceToken:)` and `application(_:didFailToRegisterForRemoteNotificationsWithError:)`. `OrientationLock` stays in `Flux/Flux/Charts/Expansion/iOS/OrientationLock.swift`. |
| macOS app delegate | `Flux/Flux/Mac/FluxAppDelegate.swift` | Add `applicationDidFinishLaunching(_:)` calling `NSApplication.shared.registerForRemoteNotifications()` only when `UNAuthorizationStatus == .authorized`, plus the two register-token callbacks. |
| iOS registration trigger | New `Flux/Packages/FluxCore/Sources/FluxCore/Notifications/SoCAlertsService.swift` | `requestAuthorizationAndRegister()` calls `UNUserNotificationCenter.current().requestAuthorization(...)`, and on grant calls `UIApplication.shared.registerForRemoteNotifications()`. This is the named site that triggers `didRegister...`. |
| Settings screen | `Flux/Flux/Settings/SettingsView.swift` | Add a `NavigationLink` (iOS) and a `LiquidGlassSection` row (macOS) under a new "Alerts" section, opening `SoCAlertsView()`. |
| API client protocol | `Flux/Packages/FluxCore/Sources/FluxCore/Networking/FluxAPIClient.swift` | Add: `registerDevice(_:)`, `fetchRules(deviceId:)`, `createRule(deviceId:rule:)`, `updateRule(deviceId:rule:)`, `deleteRule(deviceId:ruleId:)`. |
| Mock client | `Flux/Flux/Services/MockFluxAPIClient.swift` | Implement the new methods with in-memory storage; honours the 10-rule cap (returns 409 equivalent error). |
| New module placement | `Flux/Packages/FluxCore/Sources/FluxCore/Notifications/` | New directory, sibling to `WhatsNew/` and `Settings/`. |

### New CloudFormation resources

| Resource | Purpose |
|---|---|
| `DevicesTable` | `flux-devices`, PK `deviceId` (S). PITR enabled (user-authored, like notes). |
| `SocRulesTable` | `flux-soc-rules`, PK `deviceId` (S), SK `ruleId` (S). PITR enabled. |
| `SocFireStateTable` | `flux-soc-fire-state`, PK `deviceRule` (S, value `deviceId#ruleId`), SK `windowStartDate` (S, YYYY-MM-DD). Attributes also include `deviceId` and `ruleId` as separate columns for debuggability (no GSI added — see Open Issues §Resolved). TTL on `expiresAt`, set to 7 days. PITR disabled (idempotent, reconstructable). |
| SSM params (manual) | `/flux/apns/key` (SecureString, .p8 PEM), `/flux/apns/key-id` (String), `/flux/apns/team-id` (String), `/flux/apns/bundle-id` (String), `/flux/apns/env` (String, `production`\|`development`). All under `${SSMPathPrefix}/*`, which the existing IAM policy already covers. |
| `TaskRole` policy | `dynamodb:GetItem`, `Query`, `Scan`, `PutItem`, `UpdateItem`, `DeleteItem` on the three new table ARNs (Scan is for the daily orphan-GC over `flux-devices`). |
| `LambdaExecutionRole` policy | `dynamodb:GetItem`, `Query`, `PutItem`, `UpdateItem`, `DeleteItem` on `DevicesTable` and `SocRulesTable`; **`Query` + `DeleteItem` on `SocFireStateTable`** (needed for AC 5.3/5.4: clearing fire-state on rule edit/re-enable/delete — see §Cross-process State Propagation). |

### Deploy ordering

1. Create SSM SecureString params manually (CloudFormation cannot manage SecureString): the four `/flux/apns/*` keys.
2. `aws cloudformation deploy` — creates the three new Dynamo tables and the IAM updates. Until the poller is redeployed, the tables are simply unused.
3. Build and push the new poller container image (it imports `sideshow/apns2` and the new packages).
4. `aws ecs update-service --force-new-deployment` to pick up the new image.

This ordering means the Lambda starts serving registration/rule endpoints immediately after step 2, even before the poller knows how to evaluate. Devices can register and create rules; nothing fires until step 4. Rolling back is the reverse: revert the poller image first, then the CloudFormation stack.

## Components and Interfaces

### Go — `internal/dynamo`

```go
// internal/dynamo/devices.go
type DeviceItem struct {
    DeviceID            string `dynamodbav:"deviceId"`
    Platform            string `dynamodbav:"platform"`            // "ios" | "macos"
    APNsToken           string `dynamodbav:"apnsToken,omitempty"` // lowercase hex; empty until granted
    APNsTokenUpdatedAt  string `dynamodbav:"apnsTokenUpdatedAt,omitempty"`
    TZIdentifier        string `dynamodbav:"tzIdentifier"`         // IANA
    TZUpdatedAt         int64  `dynamodbav:"tzUpdatedAt"`          // unix seconds, monotonic per device
    LastRegisteredAt    string `dynamodbav:"lastRegisteredAt"`     // RFC 3339 UTC
    TokenStatus         string `dynamodbav:"tokenStatus"`          // "active" | "stale"
    CreatedAt           string `dynamodbav:"createdAt"`
}

// internal/dynamo/socrules.go
type SoCRuleItem struct {
    DeviceID         string `dynamodbav:"deviceId"`
    RuleID           string `dynamodbav:"ruleId"`           // UUID, server-assigned
    ThresholdPercent int    `dynamodbav:"thresholdPercent"` // 1..99
    WindowStart      string `dynamodbav:"windowStart"`      // HH:MM
    WindowEnd        string `dynamodbav:"windowEnd"`        // HH:MM
    Enabled          bool   `dynamodbav:"enabled"`
    Label            string `dynamodbav:"label,omitempty"`  // ≤40 chars (AC 1.2)
    CreatedAt        string `dynamodbav:"createdAt"`
    UpdatedAt        string `dynamodbav:"updatedAt"`        // monotonic; bumped by every PUT
}

// internal/dynamo/socfirestate.go
type SoCFireStateItem struct {
    DeviceRule       string  `dynamodbav:"deviceRule"`       // PK: deviceId + "#" + ruleId
    WindowStartDate  string  `dynamodbav:"windowStartDate"`  // SK: YYYY-MM-DD in device TZ
    DeviceID         string  `dynamodbav:"deviceId"`         // duplicated for debug Queries
    RuleID           string  `dynamodbav:"ruleId"`           // duplicated for debug Queries
    FiredAt          string  `dynamodbav:"firedAt"`          // RFC 3339 UTC
    ObservedSoc      float64 `dynamodbav:"observedSoc"`
    APNsCollapseID   string  `dynamodbav:"apnsCollapseId"`   // base64url(SHA-256(deviceId|ruleId|windowStartDate))[:22]
    ExpiresAt        int64   `dynamodbav:"expiresAt"`        // TTL, 7 days after fire
}
```

### Go — `internal/poller/eval` (new package)

```go
// internal/poller/eval/evaluator.go
type RulesCache interface {
    // ListEnabledDevicesWithRules returns the current rule snapshot, refreshed
    // at most every 30 s. Sorting by createdAt is the Cache's job (AC 1.6).
    Snapshot(ctx context.Context) ([]DeviceWithRules, error)
}

type FireStateRW interface {
    // PutIfAbsent uses ConditionExpression "attribute_not_exists(deviceRule)";
    // returns (true, nil) if newly written, (false, nil) if a row already exists.
    PutIfAbsent(ctx context.Context, item SoCFireStateItem) (bool, error)
}

type PushQueue interface {
    Enqueue(ctx context.Context, job PushJob) error // non-blocking until capacity 64
}

type Evaluator struct {
    cache     RulesCache
    fireState FireStateRW
    queue     PushQueue
    now       func() time.Time
    mu        sync.Mutex // guards prev (cheap; uncontended in normal operation)
    prev      map[prevKey]prevValue
}

type prevKey struct {
    deviceRule      string  // deviceId#ruleId
    windowStartDate string  // resets across days, satisfies "no carry-over from yesterday"
}
type prevValue struct {
    soc             float64
    ruleUpdatedAt   string  // version tag — drives reset on rule edit (AC 5.3)
}

func (e *Evaluator) Evaluate(ctx context.Context, soc float64, readingAt time.Time) {
    ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
    defer cancel()
    // 1. Skip if soc invalid (not 0..100) or stale (>60s old).
    // 2. snap := e.cache.Snapshot(ctx); skip device if cache load fails.
    // 3. For each device d in snap:
    //    a. Resolve d.TZ; on time.LoadLocation error log "tz_invalid" and skip d.
    //    b. localNow := readingAt.In(loc); compute windowStartDate per rule.
    //    c. For each enabled rule r in d.Rules:
    //         key := prevKey{deviceRule(d,r), windowStartDate(localNow, r)}
    //         val := e.prev[key]  (under e.mu)
    //         IF val.ruleUpdatedAt != r.UpdatedAt → delete e.prev[key] (rule changed); val.soc unset.
    //         IF rule not in window for localNow → continue.
    //         IF val.soc unset → e.prev[key] = {soc, r.UpdatedAt} and continue (seed, AC 3.3).
    //         IF val.soc > r.Threshold && soc <= r.Threshold:
    //             item := newFireStateItem(d.ID, r.ID, key.windowStartDate, soc, ...)
    //             wrote, err := e.fireState.PutIfAbsent(ctx, item)
    //             IF !wrote → skip (concurrent / already fired today).
    //             IF err → log "firestate_write_failed", continue (no push).
    //             e.queue.Enqueue(ctx, pushJobFor(d, r, item))
    //         e.prev[key] = {soc, r.UpdatedAt}
}
```

Behavioural contracts:
- **Mutex on `prev`.** Evaluate is called from a single goroutine today, but the mutex enforces the invariant for future maintainers (rejected the "comment is sufficient" stance from peer review). Cost: one uncontended lock per cycle.
- **`prev` keyed by `(deviceRule, windowStartDate)`.** Closes the "yesterday poisons today" gap: yesterday's final in-window SoC does not influence today's first in-window reading. On entering a new window-start day for a rule, the comparator is absent and the seed-on-first-reading rule (AC 3.3) applies.
- **Rule-version tag on `prev` entries.** When the cache reports a new `UpdatedAt`, the comparator is dropped and re-seeded on the next reading. This is how AC 5.3 propagates the "reset prev" requirement from a Lambda mutation into the poller process without IPC.
- **Conditional `PutIfAbsent` on fire-state.** Tolerates a rare two-evaluator overlap (rolling deploy) without double-pushing.
- **Time zone failure isolated per device.** A garbage TZ kills only that device's evaluation; logged for ops follow-up.

### Go — `internal/poller/apns` (new package)

```go
// internal/poller/apns/notifier.go
type Notifier struct {
    client   *apns2.Client     // sideshow/apns2; env selected by /flux/apns/env
    topic    string            // bundle id
    timeout  time.Duration     // 5 s per attempt
    maxRetry int               // 3
}

func (n *Notifier) Push(ctx context.Context, token, collapseID string, p eval.Payload) error
// 200 → nil
// 410 / 400 BadDeviceToken → returns ErrStaleToken; worker marks device tokenStatus="stale".
// 5xx / 429 / transport → retries with 1s × 2^attempt × jitter[0.5..1.5], up to maxRetry.
// On all-retry-exhaustion → returns the last error; fire-state row remains, push dropped per AC 3.10.

// internal/poller/apns/queue.go
type Queue struct { /* buffered chan PushJob, capacity 64; 4 workers */ }
func (q *Queue) Enqueue(ctx context.Context, job PushJob) error // returns ErrQueueFull if at capacity
```

Library: `github.com/sideshow/apns2`. Environment is chosen at startup from `/flux/apns/env` SSM param: `production` → `apns2.Client.Production()`, `development` → `apns2.Client.Development()`. Lambda **does not** import `sideshow/apns2`.

### Go — `internal/api` (Lambda)

Replacing the per-method switch with `http.ServeMux`:

```go
mux := http.NewServeMux()
mux.HandleFunc("GET /status",                                h.handleStatus)
mux.HandleFunc("GET /history",                               h.handleHistory)
mux.HandleFunc("GET /day",                                   h.handleDay)
mux.HandleFunc("PUT /note",                                  h.handleNote)
mux.HandleFunc("POST /devices",                              h.handleRegisterDevice)
mux.HandleFunc("GET /devices/{deviceId}/rules",              h.handleListRules)
mux.HandleFunc("POST /devices/{deviceId}/rules",             h.handleCreateRule)
mux.HandleFunc("PUT /devices/{deviceId}/rules/{ruleId}",     h.handleUpdateRule)
mux.HandleFunc("DELETE /devices/{deviceId}/rules/{ruleId}",  h.handleDeleteRule)
// Bearer-token guard is a middleware wrapping the mux.
```

Adapter shim translates `events.LambdaFunctionURLRequest` → `*http.Request` (path, headers, body) and `http.ResponseWriter` → `events.LambdaFunctionURLResponse`. ~30 LOC.

Endpoint shapes:

```
POST   /devices                               body: DeviceRegistration         → 200 DeviceItem
GET    /devices/{deviceId}/rules                                                → 200 {"rules": [SoCRule, ...]}  (sorted by createdAt)
POST   /devices/{deviceId}/rules              body: SoCRuleCreate              → 201 SoCRule, server-assigned ruleId/createdAt/updatedAt
PUT    /devices/{deviceId}/rules/{ruleId}     body: SoCRuleUpdate              → 200 SoCRule (updatedAt bumped)
DELETE /devices/{deviceId}/rules/{ruleId}                                       → 204
```

Validation (mirrored from AC 1.3 / 1.5):
- `thresholdPercent` is an integer in `[1, 99]`.
- `windowStart`, `windowEnd` parse as HH:MM `[00:00, 23:59]`. Reject `start == end`.
- `label` length ≤ 40.
- Per-device rule cap (10) enforced server-side: a POST that would create the 11th rule returns 409 `Conflict {"error":"rule cap reached"}`.

### Cross-process state propagation (AC 5.3 / 5.4)

| AC | Lambda action | Poller action |
|---|---|---|
| 5.3 (edit / re-enable) | Bump `SoCRuleItem.UpdatedAt`. Query `flux-soc-fire-state` by `deviceRule = deviceId#ruleId`, DeleteItem each row found. | On next cache refresh (≤30 s), evaluator sees new `UpdatedAt`. The `ruleUpdatedAt` tag mismatch causes `prev` entry deletion. Next in-window reading seeds; no fire on seed (AC 3.3). |
| 5.4 (delete) | DeleteItem the rule row. Query `flux-soc-fire-state` by `deviceRule`, DeleteItem each row. | On next cache refresh, the rule disappears from the snapshot; future cycles do not look up `prev[deviceRule, *]`. Stale `prev` entries are not actively evicted (bounded memory footprint, lifetime-only growth at the 10–20-device scale). |

Worst-case rule edit → evaluator effect latency: ≤30 s (cache refresh) + ≤10 s (poll cycle) = ≤40 s.

### Swift — `FluxCore/Notifications/`

```swift
// DeviceIdentifier.swift
public enum DeviceIdentifier {
    /// Reads or generates the stable per-install UUID in the *standard*
    /// `UserDefaults` (app container suite), NOT `.fluxAppGroup` and NOT
    /// Keychain. App uninstall on iOS/macOS deletes this. See Decision 8.
    public static func currentOrGenerate() -> String
}

// SoCAlertRule.swift
public struct SoCAlertRule: Identifiable, Codable, Sendable, Equatable {
    public let id: String                  // server-assigned UUID
    public var thresholdPercent: Int
    public var windowStart: String         // HH:MM
    public var windowEnd: String           // HH:MM
    public var enabled: Bool
    public var label: String?
    public let createdAt: Date
    public var updatedAt: Date
}

// SoCAlertsService.swift
@MainActor
public final class SoCAlertsService: ObservableObject {
    public static let shared = SoCAlertsService()  // accessed by the app delegate from the main actor
    public func bind(apiClient: any FluxAPIClient)  // called once from FluxApp init
    public func requestAuthorizationAndRegister() async throws
    /// Idempotent. Computes the registration payload and POSTs only if the backend
    /// representation differs from what this device last successfully sent.
    /// Idempotency truth is on the *backend* (`tzUpdatedAt` guard); the local
    /// "last sent" cache is only an optimisation.
    public func registerDeviceIfNeeded(token: Data?, tz: TimeZone) async throws
    public func refresh() async throws
    public func create(_ rule: SoCAlertRuleDraft) async throws -> SoCAlertRule
    public func update(_ rule: SoCAlertRule) async throws -> SoCAlertRule
    public func delete(_ ruleId: String) async throws
    @Published public private(set) var rules: [SoCAlertRule] = []
    @Published public private(set) var authStatus: UNAuthorizationStatus = .notDetermined
    @Published public private(set) var lastError: Error?
    public func clearError()                       // called by editor sheet on dismiss
}

// NotificationAuthService.swift
public enum NotificationAuthService {
    public static func currentStatus() async -> UNAuthorizationStatus
    public static func requestAlertsAuthorization() async throws -> Bool
}
```

`SoCAlertsService.requestAuthorizationAndRegister`:
1. Calls `UNUserNotificationCenter.current().requestAuthorization([.alert])`.
2. On grant (iOS): `await UIApplication.shared.registerForRemoteNotifications()` on the main actor. This is the explicit registration call site that triggers `application(_:didRegisterForRemoteNotificationsWithDeviceToken:)`.
3. On grant (macOS): `NSApplication.shared.registerForRemoteNotifications()` from the `MainActor`.
4. On denial: calls `registerDeviceIfNeeded(token: nil, tz: .current)` so the device row exists; the token attaches later when the user grants in system Settings.

Delegate callbacks reach the service via the singleton:
```swift
func application(_ application: UIApplication,
                 didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
    Task { @MainActor in
        try? await SoCAlertsService.shared.registerDeviceIfNeeded(token: deviceToken, tz: .current)
    }
}
```

The `Task { @MainActor in ... }` hop is required for Swift 6 strict concurrency; the singleton is `@MainActor`. Concurrent calls (delegate + foreground hook) are tolerated — the backend `tzUpdatedAt` guard de-dupes.

### Swift — `Flux/Settings/SoCAlerts/` (new directory)

```swift
// SoCAlertsView.swift     — list, banners, "Add" button
// SoCAlertEditor.swift     — sheet for create/edit
// SoCAlertsViewModel.swift — wraps SoCAlertsService for the view
```

UI baselines (existing-pattern references):
- The list reuses the `Form` + `Section` pattern from `SettingsView.swift` (iOS) and `LiquidGlassSection` (macOS).
- The "Add" affordance matches the existing add-note button on Day Detail.
- The denied-permission banner matches the existing `validationError` red text in `SettingsView.swift:108`.

## Data Models

(See Components for Dynamo item shapes.)

### Wire shapes

```jsonc
// POST /devices
{
  "deviceId": "uuid-from-container-userdefaults",
  "platform": "ios",                       // or "macos"
  "apnsToken": "hexstring-lowercase-no-separators",  // omit until granted
  "tzIdentifier": "Australia/Sydney",
  "tzUpdatedAt": 1714838400                // unix seconds, monotonic per device
}
// → 200 { "deviceId": "...", "platform": "...", "tzIdentifier": "...", "tokenStatus": "active|stale", ... }

// POST /devices/{deviceId}/rules
{
  "thresholdPercent": 40,
  "windowStart": "17:00",
  "windowEnd": "00:00",                    // end-of-day via cross-midnight rule (Decision 7)
  "enabled": true,
  "label": "Evening cooking"               // optional, ≤40 chars
}
// → 201 { "id": "uuid", "thresholdPercent": 40, ..., "createdAt": "RFC3339", "updatedAt": "RFC3339" }

// GET /devices/{deviceId}/rules
// → 200 { "rules": [SoCRule, ...] }   // server-sorted by createdAt asc
```

### APNs payload

```jsonc
{
  "aps": {
    "alert": {
      "title": "Battery at 38%",
      "body":  "Evening cooking — below 40% until midnight."   // label included when present
    },
    "sound": "default"
  },
  "ruleId": "uuid",
  "thresholdPercent": 40,
  "observedSoc": 38
}
```

`apns-collapse-id` header = `base64url(SHA-256(deviceId|ruleId|windowStartDate))[:22]`. 22 base64url chars encode 132 bits — fits comfortably under Apple's 64-byte cap. Same value is written to `SoCFireStateItem.APNsCollapseID` so duplicate-detection can be cross-referenced in logs.

## Error Handling

### Backend

| Failure | Detection | Action |
|---|---|---|
| AlphaESS reading missing / SoC outside 0–100 (AC 3.4) | `Evaluate` precheck | Skip cycle, do not advance comparator, emit `flux_eval_skip_stale`. |
| Comparator absent (first reading or new window-start day) | `prev` map miss | Seed with current value + `ruleUpdatedAt`; do not fire. |
| Rule `UpdatedAt` changed | tag mismatch in `prev` | Delete `prev[key]`, treat next reading as seed (AC 5.3). |
| `time.LoadLocation` fails for a device TZ | `Evaluate` per-device step | Log `flux_eval_tz_invalid`, skip device. |
| Fire-state row already exists for today | `PutIfAbsent` returns `wrote=false` | Skip push, no further work. |
| APNs 5xx / 429 / transport error | sideshow client error | Worker retries (3 attempts, exponential backoff). On exhaustion: log `flux_apns_push_failed{class=transient}`, fire-state row remains. |
| APNs 410 / 400 `BadDeviceToken` | sideshow status | Worker calls `DevicesStore.MarkStale(deviceId)` (poller IAM allows `UpdateItem` on `DevicesTable`). Drop the push. |
| Dynamo `PutItem` fire-state fails | UpdateItem error | Log, skip push, return. Next cycle re-tries naturally. |
| Push queue full | `Enqueue` returns `ErrQueueFull` | Log `flux_apns_queue_overflow`, fire-state row remains. |
| Lambda receives an unknown path | `ServeMux` 404 | Default 404 response with JSON error body. |
| Lambda receives a malformed body | `json.Unmarshal` error | 400, mirroring `handleNote`. |
| Lambda asked to create the 11th rule | server cap check | 409 `{"error":"rule cap reached"}`. |
| Lambda rule edit / delete: fire-state cleanup Query fails | post-write failure | Log `flux_lambda_firestate_cleanup_failed`. The PUT/DELETE still returns success — the next evaluator cycle is correct because rule `UpdatedAt` mismatch resets `prev`. Stale fire-state row TTLs out after 7 days. |

### Frontend

| Failure | UX |
|---|---|
| Notification permission denied | Banner with system-Settings deep-link (AC 2.2). Rules still editable; registration POSTs without `apnsToken`. |
| Backend POST to register/create/update fails | Optimistic local change kept; banner with retry; auto-retry on next foreground (AC 1.7). |
| `SoCAlertsService.refresh()` partial failure | `rules` is left unchanged (no partial overwrite); `lastError` set; banner shown until `clearError()`. |
| TZ change with no foreground | Backend uses stale TZ. Documented behaviour, no UI affordance. |

## Garbage Collection of Orphan Device Records

The midnight pass in `runMidnightFinalizer` (`internal/poller/poller.go:225`) gains a new step after the yesterday-finalisation work:

1. `Scan flux-devices` (filtered: `lastRegisteredAt < now-30d`).
2. For each candidate device:
   a. Query `flux-soc-fire-state` by `deviceRule` prefix — if any row newer than 24 h exists, skip GC for this device this pass (in-flight push protection).
   b. Query `flux-soc-rules` by `deviceId`, delete each rule row.
   c. Query `flux-soc-fire-state` by each `deviceRule`, delete each row.
   d. **Conditional `DeleteItem` on the device row** with `ConditionExpression = "lastRegisteredAt = :scanned"`. If the device re-registered between Scan and Delete, the condition fails (`ConditionalCheckFailedException`) and the row is preserved. Log `flux_orphan_gc_skipped_reregistered`.
3. Emit per-pass observability: `flux_orphan_gc_scanned`, `_deleted`, `_skipped`.

Cascade order matters: fire-state and rules first, device row last. A crash between cascade steps leaves the device row visible to the next pass, which re-attempts.

## Testing Strategy

### Unit (Go)

- `internal/poller/eval/evaluator_test.go` — table-driven across AC 3.1–3.6 transitions: above→at-or-below, at-or-below→at-or-below, missing prev, stale reading, out-of-window, cross-midnight day-counter, multi-rule independence, rule-`UpdatedAt`-bump reset, yesterday/today comparator isolation.
- `internal/poller/apns/notifier_test.go` — fake HTTP/2 server replaying APNs response codes; verifies 3-attempt backoff, stale-token classification, collapse-id propagation, environment selection.
- `internal/poller/apns/queue_test.go` — capacity, overflow signal, worker drain on shutdown.
- `internal/dynamo/socrules_test.go`, `devices_test.go`, `socfirestate_test.go` — round-trip marshalling, key construction, TTL math, conditional-put behaviour.
- `internal/api/devices_test.go`, `socrules_test.go` — endpoint validation, 409 on cap, 401 on missing/invalid bearer, fire-state cleanup on PUT and DELETE.
- `internal/poller/orphan_gc_test.go` — conditional-delete behaviour against a fake Dynamo when re-registration occurs mid-pass.

### Property-based (Go, `pgregory.net/rapid`)

- `inWindow(t, start, end, tz)` (already a clean PBT candidate; properties listed in §Architecture above are retained).
- `windowStartDate(t, start, end, tz)`:
  - For any `t` strictly inside the window: the result equals the local date at the opening minute.
  - For `t` exactly at the opening minute: increments.
  - For cross-midnight: the value persists across local midnight inside the same window.
  - DST spring-forward: a window fully inside the skipped hour produces `(false, ignored)` from `inWindow`; a window straddling the skip uses Go's wall-clock arithmetic.

### Integration (Go)

`internal/integration/socalerts_test.go` (new) drives the evaluator + queue + fake Dynamo + fake APNs for a representative 24-hour day, including a simulated poller restart (verifies AC 3.3 fallback) and a rule edit mid-day (verifies AC 5.3 reset).

### iOS / macOS

- `SoCAlertsServiceTests` (FluxCore tests) — mocks the API client; asserts idempotent register, optimistic create/update/delete flows, retry-on-foreground, denial path.
- `SoCAlertsViewModelTests` — input validation parity with AC 1.3 (validation fires both on field-edit and on save).
- View-level tests (no snapshot infra in the macOS target today, so structural assertions only): renders correct empty / list / cap-reached states; permission-denied banner appears when `authStatus == .denied`.

### Manual verification before merge

- End-to-end on a real iPhone: register, grant permission, create a rule with a 1-minute window in the near future, force-drop SoC (or stub `LiveData`) and observe the push within ~10 seconds.
- Token rotation: delete-and-reinstall the app, confirm the prior device record is GC'd within 30 days (the conditional-delete protects against the re-registration race).
- Rule edit mid-window: edit the threshold while inside an active window with SoC just above the new threshold; verify next downward crossing fires once.

## Open Issues to Confirm

(All previously-open issues have been resolved by Decisions 11, 12, 13 in the decision log and by the design revisions above. None outstanding.)
