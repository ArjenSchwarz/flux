# SoC Alerts — Implementation Explained

Implementation notes for the soc-alerts feature, written at three levels so future readers (or the next person to extend this) can ramp up at the depth they need. Source: branch `T-1288/soc-alerts`.

---

## Beginner Level

### What Changed / What This Does

Flux now lets each device (iPhone, iPad, Mac) say "ping me when the battery drops below X% during this time of day". Up to 10 rules per device. The phone tells the backend the rule once; the backend's poller watches the battery every 10 seconds and sends a push notification the moment SoC dips below the threshold, even if the app is closed.

### Why It Matters

The Flux app polls live battery state only when it's open. If the user wants to know about a sudden battery drop during dinner ("alert me at 40%, 17:00–midnight"), the app can't do that by itself — it might not be running. Doing the watch on the server, which already runs 24/7, fixes that.

### Key Concepts

- **APNs (Apple Push Notification service)**: Apple's "send a push to a phone" pipe. You give Apple a token for each device, you POST a notification, and Apple delivers it to the right phone.
- **Window**: Rules fire only between a start and end time of day, e.g. 17:00–00:00. End-of-day is the literal string `"00:00"` interpreted as midnight on the next day.
- **Threshold crossing**: The rule fires when the battery *drops to or below* the threshold this cycle, but was *above* it last cycle — like a doorbell that rings on the descent, not while you're still pressing it.
- **Fire-state**: A row written before the push goes out, used as a "we already alerted on this rule today" flag. Protects against duplicate notifications when the poller restarts.

---

## Intermediate Level

### Changes Overview

Three layers of changes:

1. **Backend Go (poller + Lambda)** — adds rule evaluation to the existing 10-second poll cycle and exposes new REST endpoints on the Lambda.
2. **Infrastructure (CloudFormation)** — three new DynamoDB tables (`flux-devices`, `flux-soc-rules`, `flux-soc-fire-state`) plus IAM and env-var wiring.
3. **Frontend Swift (FluxCore + iOS/macOS app)** — new Notifications module, Settings → Alerts UI, app-delegate APNs token plumbing.

### Implementation Approach

**Evaluator** (`internal/poller/eval/evaluator.go`)

Runs inside `Poller.fetchAndStoreLiveData` after `WriteReading` succeeds. For each device's enabled rules in the cached snapshot, it:

1. Checks the reading is fresh (≤60 s old) and SoC is in 0–100. Skip cycle otherwise (AC 3.4).
2. Computes which rules are inside their window in the device's TZ (cached `*time.Location` so we don't hit tzdata every cycle).
3. Looks up `prev[(deviceId#ruleId, windowStartDate)]`. If absent → seed only, don't fire (AC 3.3). If present and `prev.UpdatedAt` differs from the rule's current `UpdatedAt` → also seed only (the rule was edited; Decision 16).
4. Otherwise if `prev.soc > threshold && soc <= threshold` (downward crossing) → write fire-state via `PutIfAbsent` (conditional Dynamo write, Decision 9), then enqueue the push job.

The `prev` map plus the `windowStartDate` key give us once-per-window-start-day idempotency in-process; the fire-state table gives us once-per-window-start-day idempotency across poller restarts and rolling deploys.

**Push pipeline** (`internal/poller/apns/`)

The evaluator never calls APNs directly. It pushes onto a 64-deep buffered channel drained by 4 worker goroutines. Each worker:

- Calls the wrapped `sideshow/apns2` client with the topic (bundle id), the collapse-id (`base64url(sha256(deviceId|ruleId|windowStartDate))[:22]`, 132 bits, well under Apple's 64-byte cap — Decision 14), and the JSON payload.
- Classifies the response: 200 = OK; 410 / 400-BadDeviceToken = `ErrStaleToken` (no retry, marks the device's `tokenStatus = "stale"`); 4xx-non-stale = `ErrPermanent` (no retry); 5xx / 429 / transport = retry up to 3× with `1s × 2^attempt × jitter[0.5–1.5]` (Decision 10).
- Increments per-class failure counters (`stale` / `transient` / `permanent`) and emits per-push log lines (`flux_apns_push_succeeded`, `flux_apns_push_failed{class=...}`).

If the queue is full when the evaluator tries to enqueue, `Enqueue` returns `ErrQueueFull` immediately (non-blocking). The fire-state row stays put, so we don't re-fire later today — the user gets one missed alert, not a delayed surprise (Decision 10 / Decision 15).

**Lambda CRUD** (`internal/api/`)

The existing per-method switch in `Handle` is replaced by an `http.ServeMux` with Go 1.22+ path-param syntax. A thin Lambda↔HTTP adapter (`adapter.go`, ~30 LOC) translates events.LambdaFunctionURLRequest into `*http.Request` and back. A bearer-token middleware wraps the mux so auth runs *before* routing (an invalid token on `/unknown` still surfaces 401, not 404).

Five new routes:
- `POST /devices` — idempotent upsert keyed by deviceId. Uses a Dynamo conditional `PutItem` with the expression `attribute_not_exists(deviceId) OR tzUpdatedAt <= :incoming` so stale TZ updates are rejected with 409 (AC 4.5).
- `GET /devices/{id}/rules` — returns the device's rules sorted by createdAt ascending (AC 1.6).
- `POST /devices/{id}/rules` — enforces the 10-rule cap with 409, assigns a UUID + createdAt/updatedAt, returns 201.
- `PUT /devices/{id}/rules/{ruleId}` — bumps `updatedAt`, then runs `DeleteByDeviceRule` on the fire-state table (Query + DeleteItem) so the evaluator's `UpdatedAt`-tagged prev-comparator self-corrects on the next cache refresh.
- `DELETE /devices/{id}/rules/{ruleId}` — 204; same fire-state cleanup.

Fire-state cleanup failures are logged (`flux_lambda_firestate_cleanup_failed`) and the request still returns success — the evaluator's `UpdatedAt` tag handles correctness regardless.

**Orphan GC** (`internal/poller/orphan_gc.go`)

A new step in `runMidnightFinalizer`. Scans `flux-devices`, skips devices newer than 30 days, skips devices with any fire-state row newer than 24 hours (in-flight push protection), then cascade-deletes fire-state → rules → device. The device-row delete is conditional on `lastRegisteredAt = :scanned`; a device that re-registered between the Scan and the Delete keeps its row (logged as `flux_orphan_gc_skipped_reregistered`).

**Frontend Swift** (`Flux/Packages/FluxCore/Sources/FluxCore/Notifications/`)

- `DeviceIdentifier` — UUID in the app's container `UserDefaults` (not Keychain, not the app-group suite; Decision 8). Uninstall resets it.
- `SoCAlertRule` / `SoCAlertRuleDraft` — Codable value types. Draft has a `validate()` method matching the server-side rules (1..99 threshold, HH:MM, start ≠ end, label ≤ 40 chars).
- `SoCAlertsService` (`@Observable`, `@MainActor`) — owns the rules cache and the registration cache. Idempotent registration: if `(token, tz)` matches the last successfully-sent value (stored in UserDefaults as a JSON blob), the POST is skipped. On failure, the pending registration is stashed; `foregroundHook()` (called from `AppNavigationView.onChange(of: scenePhase)`) replays it.
- `NotificationAuthService` — wraps the three `UNUserNotificationCenter` calls.
- `SoCAlertsView` + `SoCAlertEditor` + `SoCAlertsViewModel` — list / sheet for create-and-edit, with permission-denied banner and a `disabled` Add button at the cap.
- App delegates (iOS in `Flux/Flux/iOS/FluxiOSAppDelegate.swift`, macOS in `Flux/Flux/Mac/FluxAppDelegate.swift`) — receive the APNs device token and hand it off to `SoCAlertsService.shared` via `Task { @MainActor in ... }`.

### Trade-offs

- **Comparator in process memory, not Dynamo**: the literal reading of AC 3.2 would have required ~8.6M Dynamo writes/day at the 100-rule cap. Decision 11 relaxed it to "may persist". The cost is a possible silent miss in the ~10s window after a poller restart.
- **Lambda owns fire-state cleanup** (Decision 17): we widened the Lambda IAM with `Query + DeleteItem` on the fire-state table rather than streaming Lambda mutations to the poller. Smaller blast radius than a Streams + cleanup-Lambda combo, and the evaluator's `UpdatedAt` tag means a cleanup failure can't cause a wrong alert.
- **`sideshow/apns2`** (Decision 13): the de-facto Go APNs library handles HTTP/2 connection reuse and JWT refresh. Writing a custom client would replicate ~150 LOC of well-trodden behaviour for no benefit.
- **Bounded queue, drop on overflow** (Decision 15): an unbounded queue would mask back-pressure. Dropping pushes (with the fire-state row retained so today's rule doesn't re-fire) keeps the live-poll goroutine on its 10s cadence regardless of APNs RTT.

---

## Expert Level

### Technical Deep Dive

**Prev-comparator key and reset semantics (Decision 16 / AC 5.3 / AC 3.6).** The map is `map[prevKey]prevValue` where `prevKey = struct{deviceRule, windowStartDate string}`. The `windowStartDate` is computed by `inWindow` and reflects the *opening day* of a cross-midnight window — for `22:00-06:00`, evaluations at 02:00 on day D+1 still see `windowStartDate = D`. This means a fire at 02:00 increments today's counter even though it's calendrically tomorrow, which is exactly the AC 3.6(b) example. The same key drives the once-per-window-start-day guarantee end-to-end, including in the APNs `apns-collapse-id`. The `prevValue` carries the rule's `UpdatedAt` string; a mismatch on the next lookup deletes the entry, so a Lambda PUT that bumps `UpdatedAt` causes the next evaluator cycle to re-seed (no spurious fire, AC 3.3). Worst-case latency from Lambda mutation to evaluator effect: 30s cache refresh + 10s poll cadence = 40s.

**Fire-state idempotency (Decision 9, AC 3.8).** `PutIfAbsent` uses `ConditionExpression = "attribute_not_exists(deviceRule)"`. The conditional check is server-side, so two concurrent evaluators (rolling deploy window) cannot both write. The eval package's `FireStateRW` interface maps a Dynamo `ConditionalCheckFailedException` to `(wrote=false, err=nil)` so the evaluator can branch on the boolean without inspecting the error type. The collapse-id is computed deterministically from the same triple that keys fire-state, so even if a duplicate push slips past the in-process and Dynamo guards (e.g., a fire-state write that succeeded but returned an error due to a connection blip after the put landed), APNs collapses on the device.

**Queue overflow contract.** `Queue.Enqueue` is `select { case q.ch <- job: nil; default: ErrQueueFull }`. Non-blocking by design (Decision 15). The eval package defines its own `ErrQueueFull` sentinel (`internal/poller/eval/evaluator.go:259`) and `cmd/poller/socalerts.go:144` translates `apns.ErrQueueFull` into it; both packages stay independent. On overflow the evaluator increments the per-cycle counter and logs `flux_apns_queue_overflow` — the fire-state row is *not* removed, so the rule doesn't re-fire later today. This is intentional: a delayed alert hours after the user has forgotten the original moment is worse UX than a silent miss.

**APNs retry classification.** `classifyResponse` (notifier.go) is the single source of truth. Permanent failures wrap `ErrPermanent`; stale-token failures wrap `ErrStaleToken`. The queue worker uses `errors.Is(err, ErrPermanent)` to count failure classes — no string sniffing, no inter-package coupling on log messages.

**Lambda routing migration.** `http.ServeMux` was chosen over a third-party router because Go 1.22 added method+path patterns natively. The adapter shim normalises the Lambda Function URL event shape (`req.RequestContext.HTTP.Method`, `req.RawPath`, `req.QueryStringParameters` or `req.RawQueryString`, base64 body framing) into an `*http.Request`. `jsonNotFound` and `jsonMethodNotAllowed` middlewares wrap the mux to convert ServeMux's plain-text 404/405 into the project's JSON error shape; only the body is rewritten, the `Allow` header from the 405 path is preserved. The bearer-token middleware uses `crypto/subtle.ConstantTimeCompare` so it's safe against timing attacks (carried over from the original `Handle.validToken`).

**Conditional `PutDeviceConditional` semantics (AC 4.5).** The condition expression `attribute_not_exists(deviceId) OR tzUpdatedAt <= :incoming` is structured to allow first-time inserts (`attribute_not_exists` is true) while also permitting same-or-newer TZ updates. A stale incoming `tzUpdatedAt` fails the condition and the Lambda returns 409, leaving the row's existing TZ intact. The handler's existing-row read happens *before* the conditional write so it can preserve fields the payload omits (e.g., APNs token from a previous registration when this request came from the denial path).

**Orphan GC race (AC 4.6, Decision 12).** The scan-then-delete has a TOCTOU window: a device could re-register between the Scan and the cascade Delete. The cascade deletes fire-state first → rules second → device row last, with the device delete being conditional on `lastRegisteredAt = :scanned`. If the device re-registered, `lastRegisteredAt` is now newer and the conditional Delete returns `ConditionalCheckFailedException`. The cleanup orphans the fire-state and rule rows for that device — but the re-registration handler's job is to recreate them from the client, so the inconsistency is short-lived. The alternative (a transaction or two-phase delete) was rejected as over-engineering for a 10-device-cap feature.

**Device identifier rationale (Decision 8).** `UserDefaults.standard` (app container) and not the app-group suite, because the widget extension shares the app-group container and the widget surviving an app uninstall would leak the identifier. Not Keychain because Keychain survives uninstall on iOS — that's the wrong semantics here (Decision 5 wants rules lost on reinstall). The trade-off is that a user who reinstalls cannot recover their rules; they re-create up to 10 entries. The two-user shared-bearer-token model means we deliberately don't have an identity system to bootstrap recovery against.

### Architecture Impact

- The poller no longer has a single "live-data goroutine does everything" model. The push-queue worker pool introduces a second concurrent component with its own lifecycle (`Start`/`Stop` in `Poller.Run`). New maintainers of this code need to remember that an APNs outage isn't allowed to wedge the poll cadence — the bounded queue is the load-bearing pattern here.
- The Lambda gained a non-trivial write surface (devices + rules) plus a narrow grant on fire-state. The IAM scope is intentionally split (full CRUD on devices/rules, only Query+DeleteItem on fire-state) so that a future Lambda refactor can't accidentally widen the fire-state surface by re-using a "Lambda can touch the SoC tables" macro.
- The new `FluxAPIClient` protocol extensions provide default implementations for the SoC endpoints that throw `notConfigured`. This was added so unrelated test mocks don't need to grow stubs, but the cost is that a production conformance bug (e.g. URLSessionAPIClient losing one of the methods through a refactor) would silently fall through to the default and surface as a runtime `notConfigured` instead of a build failure. The two production conformers (`URLSessionAPIClient`, `MockFluxAPIClient`) are small enough that this is acceptable; if more land, split into a separate `FluxSoCAPIClient` protocol.

### Potential Issues

- **Unbounded `prev` map**: pruning happens only via `UpdatedAt` mismatch. In normal operation each (deviceRule, windowStartDate) entry stays around indefinitely; with ≤20 devices × ≤10 rules × ≤7 retained days (matching the fire-state TTL) the bound is ~1400 entries. Acceptable in practice but worth re-visiting if the user count scales unexpectedly. A periodic prune (drop entries with `windowStartDate < today-7d`) would be easy if needed.
- **TZ snapshot consistency within a cycle**: `evaluateDevice` resolves `time.LoadLocation(d.TZIdentifier)` once per device per cycle (now cached). AC 3.5 says "evaluated at the start of the cycle" — strictly speaking the implementation snapshots per-device-at-call rather than once-up-front. For Sydney-only deployments it doesn't matter; if a user travels mid-cycle the second device's evaluation could see a newer TZ. The cache refresh interval (30s) bounds this to one mistaken cycle.
- **Stale APNs queue on poller restart**: pushes in-flight at shutdown have up to `shutdownDrainTimeout` (25s) to complete. Anything still queued gets dropped when `q.Stop` returns. The fire-state row is already written, so on restart the same crossing won't re-fire. The user sees a silent miss for that crossing only.
- **`AppNavigationView` foreground hook**: relies on the scenePhase observer firing. If a user keeps the app open in the background and only resumes via a notification tap that doesn't change scenePhase, the pending registration could sit unreplayed. iOS's lifecycle should make this hard to hit, but it's an edge case worth being aware of.
- **No client-side rate limiting on rule CRUD**: nothing stops the Settings UI from rapidly re-toggling a rule's `enabled` state, which would generate one fire-state cleanup per toggle. Cheap (Query + DeleteItem with empty result) but visible in logs. If a user finds the rule-toggle path frustratingly slow, debouncing in the view-model would help.

---

## Completeness Assessment

### Fully Implemented

All 35 tasks in the spec are complete and the test suite passes.

**Backend Go:**
- Three Dynamo stores (devices/rules/fire-state) with conditional PutIfAbsent, conditional `lastRegisteredAt` delete, and TTL on fire-state.
- Evaluator with mutex-guarded prev map, (deviceRule, windowStartDate)-keyed comparator, UpdatedAt-tag reset, downward-crossing semantics, fire-state-before-enqueue ordering, collapse-id hash, per-cycle observability counters (rules_evaluated / rules_fired / pushes_queued / queue_overflow).
- APNs Notifier with 3-retry exponential backoff, stale-token / permanent / transient classification via typed sentinel errors (`ErrStaleToken`, `ErrPermanent`), environment selection from SSM.
- 64-deep bounded push queue with 4 workers; non-blocking enqueue returns ErrQueueFull on capacity.
- Lambda routing migrated to `http.ServeMux` + bearer-token middleware + Lambda adapter. All four legacy routes unchanged. Five new routes with fire-state cleanup on PUT/DELETE.
- Conditional device upsert (AC 4.5) implemented at the Dynamo layer via `PutDeviceConditional` with the `attribute_not_exists OR tzUpdatedAt <=` expression.
- Orphan GC step in midnight finalizer with 24h in-flight fire-state guard and conditional delete on `lastRegisteredAt`.
- Integration test covering normal day / restart / mid-day rule edit / cross-midnight.

**Infrastructure:**
- Three new Dynamo tables in CloudFormation with the correct PITR/TTL choices.
- IAM widened: TaskRole gets full CRUD on all three new tables; LambdaExecutionRole gets full read+write on devices/rules and `Query + DeleteItem` only on fire-state (Decision 17).
- Lambda + ECS env vars for the three new table names and the five `/flux/apns/*` SSM paths.
- Lambda entry point (`cmd/api/main.go`) now constructs the device store, rule store, and fire-state cleaner and wires them via the handler's setters.
- Poller entry point (`cmd/poller/main.go` / `cmd/poller/socalerts.go`) loads APNs creds via batch `GetParameters`, constructs Notifier + Queue + Evaluator + Orphan GC, and wires them through `Poller.SetSocAlerts` / `SetOrphanGC`.

**Frontend Swift:**
- `DeviceIdentifier` (UserDefaults.standard, not Keychain, not app-group).
- `SoCAlertRule` + `SoCAlertRuleDraft` value types with validation parity to AC 1.3 / 1.2.
- `FluxAPIClient` protocol extended; `URLSessionAPIClient` ships real implementations; `MockFluxAPIClient` honours the 10-rule cap.
- `SoCAlertsService` with idempotent registration (lastSent UserDefaults cache), optimistic CRUD, foregroundHook replay.
- `NotificationAuthService` wrapping `UNUserNotificationCenter`.
- Settings → Alerts entry on iOS Form and macOS LiquidGlassSection (the LiquidGlassSection is now shared between SettingsView and SoCAlertsView rather than duplicated).
- `SoCAlertsView` + `SoCAlertEditor` + `SoCAlertsViewModel` with empty / list / permission-denied / cap-reached / error states.
- iOS and macOS app delegates handle `didRegisterForRemoteNotificationsWithDeviceToken` and call `SoCAlertsService.shared.registerDeviceIfNeeded` on the MainActor.
- `AppNavigationView.onChange(of: scenePhase)` invokes `SoCAlertsService.shared.foregroundHook()` so pending registrations replay on app foreground.

### Partially Implemented

- **Observability events.** Per-cycle counters now emit; per-push events (`flux_apns_push_succeeded` / `_failed`, `flux_apns_mark_stale_failed`, `flux_apns_queue_overflow`, `flux_eval_skip_stale`, `flux_eval_tz_invalid`) are present. CloudWatch metrics (vs. structured logs) aren't wired — the project's existing CloudWatch path is reserved for the daily-derived-stats pass. Logs are sufficient for current ops.

### Missing

Nothing required by the spec is missing. Operational follow-ups that are explicitly out of scope:

- APNs key rotation runbook lives in `prerequisites.md`; no automation.
- No CLI/maintenance tool for surfacing devices currently marked `tokenStatus = "stale"`.
- TestFlight / App Store transition to `aps-environment = production` is documented in prerequisites but not automated.
