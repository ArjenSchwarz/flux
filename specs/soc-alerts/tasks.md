---
references:
    - specs/soc-alerts/requirements.md
    - specs/soc-alerts/design.md
    - specs/soc-alerts/decision_log.md
    - specs/soc-alerts/prerequisites.md
---
# SoC Alerts

## Backend: data layer

- [x] 1. Write tests for DynamoDB device, rule, and fire-state items and stores <!-- id:p3nx2vk -->
  - Marshal/unmarshal round-trip for DeviceItem; SoCRuleItem; SoCFireStateItem
  - PK composition: SoCFireStateItem.deviceRule = deviceId + "#" + ruleId
  - TTL = firedAt + 7d as Unix seconds
  - PutIfAbsent via fake WriteAPI: returns (true,nil) on first write, (false,nil) on second write when ConditionExpression attribute_not_exists(deviceRule) is supplied
  - Conditional DeleteItem on lastRegisteredAt: returns conditional-check-failed when value mismatches
  - Stream: 1
  - Requirements: [3.6](requirements.md#3.6), [3.7](requirements.md#3.7), [3.8](requirements.md#3.8), [4.1](requirements.md#4.1), [4.3](requirements.md#4.3), [5.2](requirements.md#5.2), [6.1](requirements.md#6.1)
  - References: internal/dynamo/devices_test.go, internal/dynamo/socrules_test.go, internal/dynamo/socfirestate_test.go

- [x] 2. Implement DeviceItem, SoCRuleItem, SoCFireStateItem types and stores (PutIfAbsent for fire-state) <!-- id:p3nx2vl -->
  - Files: internal/dynamo/devices.go, socrules.go, socfirestate.go
  - Stores follow the existing DynamoNoteWriter pattern (narrow WriteAPI interface; *dynamodb.Client satisfies it at compile time)
  - Add only the methods the evaluator and Lambda need to internal/dynamo/store.go; the poller-side Store interface and the Lambda-side reader interface stay separate
  - PutIfAbsent uses ConditionExpression attribute_not_exists(deviceRule)
  - DeleteDeviceConditional uses ConditionExpression lastRegisteredAt = :scanned for the orphan GC race
  - Blocked-by: p3nx2vk (Write tests for DynamoDB device, rule, and fire-state items and stores)
  - Stream: 1
  - Requirements: [3.6](requirements.md#3.6), [3.7](requirements.md#3.7), [3.8](requirements.md#3.8), [4.1](requirements.md#4.1), [4.3](requirements.md#4.3), [5.2](requirements.md#5.2), [6.1](requirements.md#6.1)
  - References: internal/dynamo/devices.go, internal/dynamo/socrules.go, internal/dynamo/socfirestate.go, internal/dynamo/store.go

## Backend: evaluator

- [x] 3. Write property-based tests for inWindow and windowStartDate helpers (cross-midnight, DST) <!-- id:p3nx2vm -->
  - pgregory.net/rapid properties from design.md Testing Strategy: 24-hour periodicity; non-cross-midnight membership; cross-midnight membership; DST spring-forward (window fully inside the skipped hour returns false); windowStartDate increments at the opening minute; persists across local midnight within the same window
  - Generators: any HH:MM start != end; any tz in {Australia/Sydney, UTC, America/New_York}; any wall time in a 7-day window
  - Stream: 1
  - Requirements: [1.4](requirements.md#1.4), [3.5](requirements.md#3.5), [3.6](requirements.md#3.6), [6.5](requirements.md#6.5)
  - References: internal/poller/eval/window_test.go

- [x] 4. Implement inWindow and windowStartDate helpers in internal/poller/eval <!-- id:p3nx2vn -->
  - Files: internal/poller/eval/window.go
  - Use time.Date for DST-correct local arithmetic
  - Return (inside bool, windowStartDate string) so callers fetch both with one pass
  - Blocked-by: p3nx2vm (Write property-based tests for inWindow and windowStartDate helpers (cross-midnight, DST)), helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers, helpers
  - Stream: 1
  - Requirements: [1.4](requirements.md#1.4), [3.5](requirements.md#3.5), [3.6](requirements.md#3.6), [6.5](requirements.md#6.5)
  - References: internal/poller/eval/window.go

- [x] 5. Write table-driven tests for Evaluator: seed, fire transitions, missing prev, stale reading, out-of-window, rule-UpdatedAt reset, comparator key isolation across days <!-- id:p3nx2vo -->
  - Table cases mirror design.md Components Evaluator pseudocode: missing prev seeds and skips; above->at-or-below fires once; at-or-below->at-or-below no second fire; out-of-window skips and does not advance comparator; stale reading (>60s) and out-of-range SoC skip; rule UpdatedAt mismatch deletes prev and reseeds on next reading; yesterday's last in-window value does not influence today's first in-window evaluation; multiple rules fire independently in the same cycle
  - Mock RulesCache and FireStateRW; assert PutIfAbsent called once per fire and Enqueue called only after PutIfAbsent returns wrote=true
  - Blocked-by: p3nx2vn (Implement inWindow and windowStartDate helpers in internal/poller/eval)
  - Stream: 1
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [3.4](requirements.md#3.4), [3.5](requirements.md#3.5), [3.6](requirements.md#3.6), [3.8](requirements.md#3.8), [5.1](requirements.md#5.1), [5.2](requirements.md#5.2), [5.3](requirements.md#5.3), [6.2](requirements.md#6.2)
  - References: internal/poller/eval/evaluator_test.go

- [x] 6. Implement Evaluator with mutex-protected prev, RulesCache (30s refresh), fire-state PutIfAbsent, collapse-id hash helper <!-- id:p3nx2vp -->
  - Files: internal/poller/eval/evaluator.go, rulescache.go, collapseid.go
  - prevKey = struct{deviceRule string; windowStartDate string}; prevValue carries soc plus ruleUpdatedAt for the version-tag reset
  - sync.Mutex guards prev (enforces the single-writer invariant rather than relying on a comment)
  - RulesCache.Snapshot refreshes at most every 30s; returns sorted-by-createdAt rules per device
  - Evaluate runs under context.WithTimeout(ctx, 3*time.Second)
  - collapseID = base64url(sha256(deviceId|ruleId|windowStartDate))[:22] (Decision 14)
  - Blocked-by: p3nx2vl (Implement DeviceItem, SoCRuleItem, SoCFireStateItem types and stores (PutIfAbsent for fire-state)), p3nx2vo (Write table-driven tests for Evaluator: seed, fire transitions, missing prev, stale reading, out-of-window, rule-UpdatedAt reset, comparator key isolation across days)
  - Stream: 1
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [3.4](requirements.md#3.4), [3.5](requirements.md#3.5), [3.6](requirements.md#3.6), [3.8](requirements.md#3.8), [5.1](requirements.md#5.1), [5.2](requirements.md#5.2), [5.3](requirements.md#5.3), [6.2](requirements.md#6.2), [6.4](requirements.md#6.4)
  - References: internal/poller/eval/evaluator.go, internal/poller/eval/rulescache.go, internal/poller/eval/collapseid.go

## Backend: APNs

- [x] 7. Write tests for APNs Notifier: retry/backoff, stale-token classification, environment selection from SSM <!-- id:p3nx2vq -->
  - Fake HTTP/2 server replays APNs status codes (200, 410, 400 BadDeviceToken, 500, 429, transport error)
  - Verify: 200 returns nil; 410 returns ErrStaleToken; 5xx/429/transport retries up to 3 attempts with backoff bounded by 1s * 2^attempt * jitter[0.5,1.5]; exhausted retries return the last error and do NOT clear fire-state
  - Environment switch: client built from /flux/apns/env=development uses Development host, =production uses Production host
  - Stream: 1
  - Requirements: [3.9](requirements.md#3.9), [3.10](requirements.md#3.10), [3.11](requirements.md#3.11)
  - References: internal/poller/apns/notifier_test.go

- [x] 8. Implement Notifier wrapping sideshow/apns2 (token auth, env switch, retry policy) <!-- id:p3nx2vr -->
  - Files: internal/poller/apns/notifier.go
  - Wraps github.com/sideshow/apns2 with token-based auth (Decision 13)
  - Notification payload built per design.md APNs payload section; aps-topic header = bundle id
  - On ErrStaleToken: writes tokenStatus=stale to DevicesTable via the same write interface the registration uses (poller IAM allows UpdateItem on flux-devices)
  - Blocked-by: p3nx2vq (Write tests for APNs Notifier: retry/backoff, stale-token classification, environment selection from SSM)
  - Stream: 1
  - Requirements: [3.9](requirements.md#3.9), [3.10](requirements.md#3.10), [3.11](requirements.md#3.11), [6.4](requirements.md#6.4)
  - References: internal/poller/apns/notifier.go, internal/poller/apns/payload.go

- [x] 9. Write tests for push Queue: capacity, overflow signal, worker drain on shutdown <!-- id:p3nx2vs -->
  - Buffered chan capacity 64; Enqueue returns ErrQueueFull when channel is full and ctx is non-blocking
  - Workers (4) drain on shutdown when the underlying ctx is cancelled; in-flight pushes complete; queued-but-not-started pushes are dropped (and logged)
  - Worker count, capacity, and ErrQueueFull observable so callers can assert overflow without races
  - Stream: 1
  - Requirements: [3.7](requirements.md#3.7), [3.10](requirements.md#3.10), [3.11](requirements.md#3.11), [6.1](requirements.md#6.1)
  - References: internal/poller/apns/queue_test.go

- [x] 10. Implement Queue (buffered chan, 4 workers, ErrQueueFull on capacity) <!-- id:p3nx2vt -->
  - Files: internal/poller/apns/queue.go
  - PushJob carries deviceID, ruleID, token, collapseID, payload, attempt count
  - Worker calls Notifier.Push, emits flux_apns_push_succeeded / _failed{class} observability events, handles stale-token UpdateItem
  - Blocked-by: p3nx2vs (Write tests for push Queue: capacity, overflow signal, worker drain on shutdown)
  - Stream: 1
  - Requirements: [3.7](requirements.md#3.7), [3.10](requirements.md#3.10), [3.11](requirements.md#3.11), [6.1](requirements.md#6.1), [6.4](requirements.md#6.4)
  - References: internal/poller/apns/queue.go

## Backend: Lambda API

- [x] 11. Write tests for Lambda handler ServeMux migration: existing endpoints unchanged, bearer-token middleware, Lambda adapter shim <!-- id:p3nx2vu -->
  - Tests verify /status, /history, /day, /note still pass through unchanged (regression guard for the routing refactor)
  - Lambda adapter shim: translates events.LambdaFunctionURLRequest to *http.Request (path, headers, body, base64 decode) and http.ResponseWriter back to events.LambdaFunctionURLResponse; ~30 LOC
  - Bearer-token middleware wraps the mux; existing constant-time compare preserved
  - Stream: 1
  - Requirements: [6.3](requirements.md#6.3)
  - References: internal/api/handler_test.go, internal/api/adapter_test.go, internal/api/middleware_test.go

- [x] 12. Refactor handler.go to http.ServeMux + Lambda adapter; preserve /status, /history, /day, /note behaviour <!-- id:p3nx2vv -->
  - File: internal/api/handler.go (rewrite), internal/api/adapter.go (new), internal/api/middleware.go (new)
  - http.ServeMux with Go 1.22+ {param} syntax; one HandleFunc per (method, path)
  - Existing handlers (handleStatus, handleHistory, handleDay, handleNote) reused unchanged; bearer-token check moves into middleware wrapping the mux
  - Blocked-by: p3nx2vu (Write tests for Lambda handler ServeMux migration: existing endpoints unchanged, bearer-token middleware, Lambda adapter shim)
  - Stream: 1
  - Requirements: [6.3](requirements.md#6.3)
  - References: internal/api/handler.go, internal/api/adapter.go, internal/api/middleware.go

- [x] 13. Write tests for POST /devices: body validation, tzUpdatedAt monotonic guard, token nullability <!-- id:p3nx2vw -->
  - Body: {deviceId, platform, apnsToken?, tzIdentifier, tzUpdatedAt}
  - Reject malformed JSON (400), missing required fields (400), platform not in {ios,macos} (400)
  - Apply tzUpdatedAt monotonic guard: ConditionExpression attribute_not_exists(tzUpdatedAt) OR tzUpdatedAt < :incoming; a stale incoming tzUpdatedAt SHALL NOT overwrite TZ
  - Stream: 1
  - Requirements: [2.3](requirements.md#2.3), [4.1](requirements.md#4.1), [4.4](requirements.md#4.4), [4.5](requirements.md#4.5)
  - References: internal/api/devices_test.go

- [x] 14. Implement handleRegisterDevice (idempotent upsert by deviceId, tzUpdatedAt condition) <!-- id:p3nx2vx -->
  - Files: internal/api/devices.go, internal/api/devices_handler.go
  - Idempotent: same payload produces the same row; partial payloads (e.g., token absent because permission denied) preserve existing values
  - 200 response returns the canonical stored DeviceItem
  - Blocked-by: p3nx2vl (Implement DeviceItem, SoCRuleItem, SoCFireStateItem types and stores (PutIfAbsent for fire-state)), p3nx2vv (Refactor handler.go to http.ServeMux + Lambda adapter; preserve /status, /history, /day, /note behaviour), p3nx2vw (Write tests for POST /devices: body validation, tzUpdatedAt monotonic guard, token nullability)
  - Stream: 1
  - Requirements: [2.3](requirements.md#2.3), [4.1](requirements.md#4.1), [4.4](requirements.md#4.4), [4.5](requirements.md#4.5)
  - References: internal/api/devices.go, internal/api/devices_handler.go

- [x] 15. Write tests for rules CRUD: GET sort-by-createdAt, POST cap=10 (409), PUT/DELETE fire-state cleanup, field validation parity with AC 1.3 <!-- id:p3nx2vy -->
  - GET: response is server-sorted by createdAt ascending (AC 1.6) — Dynamo Query returns by SK = ruleId UUID which is random, so sort in the handler
  - POST cap: 409 with {"error":"rule cap reached"} when 11th rule is attempted on a device
  - PUT/DELETE: after rule mutation, Query flux-soc-fire-state by deviceRule = {deviceId}#{ruleId} and DeleteItem each row; failure of the cleanup logs flux_lambda_firestate_cleanup_failed but PUT/DELETE still returns success (evaluator self-corrects via UpdatedAt tag)
  - Validation parity with AC 1.3: thresholdPercent 1..99; HH:MM 00:00..23:59; start != end; label length <= 40
  - Stream: 1
  - Requirements: [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.5](requirements.md#1.5), [1.6](requirements.md#1.6), [1.7](requirements.md#1.7), [5.3](requirements.md#5.3), [5.4](requirements.md#5.4)
  - References: internal/api/socrules_test.go

- [x] 16. Implement handleListRules, handleCreateRule, handleUpdateRule, handleDeleteRule plus fire-state Query+DeleteItem cleanup <!-- id:p3nx2vz -->
  - Files: internal/api/socrules.go, internal/api/socrules_handler.go
  - Server-assigned ruleId (uuid), createdAt, updatedAt (RFC3339 UTC); updatedAt bumps on every PUT
  - Fire-state cleanup runs even on PUT that only flips Enabled; the version bump is what drives the poller's prev reset
  - Blocked-by: p3nx2vl (Implement DeviceItem, SoCRuleItem, SoCFireStateItem types and stores (PutIfAbsent for fire-state)), p3nx2vv (Refactor handler.go to http.ServeMux + Lambda adapter; preserve /status, /history, /day, /note behaviour), p3nx2vy (Write tests for rules CRUD: GET sort-by-createdAt, POST cap=10 (409), PUT/DELETE fire-state cleanup, field validation parity with AC 1.3), cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup, cleanup
  - Stream: 1
  - Requirements: [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.5](requirements.md#1.5), [1.6](requirements.md#1.6), [1.7](requirements.md#1.7), [5.3](requirements.md#5.3), [5.4](requirements.md#5.4)
  - References: internal/api/socrules.go, internal/api/socrules_handler.go

## Backend: poller wiring + GC

- [x] 17. Wire Evaluator, Queue, and Notifier into Poller.New and cmd/poller/main.go (load /flux/apns/* from SSM) <!-- id:p3nx2w0 -->
  - File: cmd/poller/main.go — loads /flux/apns/key (SecureString), /flux/apns/key-id, /flux/apns/team-id, /flux/apns/bundle-id, /flux/apns/env via the same SSM pattern as existing /flux/api-token
  - Poller.New gains optional Evaluator + Queue + Notifier dependencies; tests inject no-ops, production wires real implementations
  - Evaluator call lives inside fetchAndStoreLiveData immediately after store.WriteReading succeeds (design.md Integration points)
  - Blocked-by: p3nx2vp (Implement Evaluator with mutex-protected prev, RulesCache (30s refresh), fire-state PutIfAbsent, collapse-id hash helper), p3nx2vr (Implement Notifier wrapping sideshow/apns2 (token auth, env switch, retry policy)), p3nx2vt (Implement Queue (buffered chan, 4 workers, ErrQueueFull on capacity)), workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers, workers
  - Stream: 1
  - Requirements: [3.1](requirements.md#3.1), [3.7](requirements.md#3.7), [6.3](requirements.md#6.3)
  - References: cmd/poller/main.go, internal/poller/poller.go

- [x] 18. Write tests for orphan device GC: conditional delete on lastRegisteredAt, 24h fire-state guard, cascade ordering <!-- id:p3nx2w1 -->
  - Conditional DeleteItem on the device row uses ConditionExpression lastRegisteredAt = :scanned; assert ConditionalCheckFailedException path is exercised and logs flux_orphan_gc_skipped_reregistered
  - 24h fire-state guard: device with any fire-state row newer than 24h is skipped this pass
  - Cascade order: fire-state first, then rules, then device row (so a crash leaves the device visible to the next pass)
  - Stream: 1
  - Requirements: [4.6](requirements.md#4.6)
  - References: internal/poller/orphan_gc_test.go

- [x] 19. Implement orphan GC step in runMidnightFinalizer (Scan flux-devices, conditional cascade-delete) <!-- id:p3nx2w2 -->
  - File: internal/poller/orphan_gc.go (new); called as a new step at the end of runMidnightFinalizer in internal/poller/poller.go:225
  - Emits flux_orphan_gc_scanned, _deleted, _skipped_reregistered, _skipped_recent_firestate observability events
  - Blocked-by: p3nx2vl (Implement DeviceItem, SoCRuleItem, SoCFireStateItem types and stores (PutIfAbsent for fire-state)), p3nx2w1 (Write tests for orphan device GC: conditional delete on lastRegisteredAt, 24h fire-state guard, cascade ordering)
  - Stream: 1
  - Requirements: [4.6](requirements.md#4.6), [6.4](requirements.md#6.4)
  - References: internal/poller/orphan_gc.go, internal/poller/poller.go

## Backend: integration

- [x] 20. Write integration test: simulated 24h day across poll cycles, simulated poller restart (AC 3.3), mid-day rule edit (AC 5.3), cross-midnight (AC 3.6) <!-- id:p3nx2w3 -->
  - File: internal/integration/socalerts_test.go (new) — follows the existing internal/integration pattern
  - Drives Evaluator + Queue + fake Dynamo + fake APNs over a simulated 24h day
  - Scenarios: (a) cold start, normal day, 17:00-00:00 rule fires once on dip; (b) simulated poller restart mid-window — first reading post-restart only seeds, does not fire (AC 3.3); (c) mid-day rule edit (threshold change) clears fire-state and prev, next dip fires (AC 5.3); (d) cross-midnight 22:00-06:00 rule fires once even when crossing midnight (AC 3.6)
  - Blocked-by: p3nx2w0 (Wire Evaluator, Queue, and Notifier into Poller.New and cmd/poller/main.go (load /flux/apns/* from SSM))
  - Stream: 1
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [3.6](requirements.md#3.6), [5.3](requirements.md#5.3)
  - References: internal/integration/socalerts_test.go

## Backend: infrastructure

- [x] 21. Update CloudFormation template + parameters file: DevicesTable, SocRulesTable, SocFireStateTable (TTL), TaskRole and LambdaExecutionRole grants, Lambda + ECS env vars for /flux/apns/* <!-- id:p3nx2w4 -->
  - File: infrastructure/template.yaml
  - Add DevicesTable, SocRulesTable, SocFireStateTable per design.md New CloudFormation resources table (PITR on devices/rules; TTL on fire-state via expiresAt)
  - Widen TaskRole: dynamodb:GetItem, Query, Scan, PutItem, UpdateItem, DeleteItem on the three new tables; Scan is for the daily orphan GC over flux-devices
  - Widen LambdaExecutionRole: same Dynamo verbs on DevicesTable and SocRulesTable; Query + DeleteItem on SocFireStateTable (Decision 17)
  - Add Lambda env vars TABLE_DEVICES, TABLE_SOC_RULES, TABLE_SOC_FIRESTATE; add ECS task env vars for the same plus APNS_* SSM param paths
  - Existing ssm:GetParameters wildcard ${SSMPathPrefix}/* already covers /flux/apns/* — no IAM change needed for SSM
  - Deploy order from design.md: CFN deploy first (creates tables), poller image push and force-new-deployment second
  - Blocked-by: p3nx2w0 (Wire Evaluator, Queue, and Notifier into Poller.New and cmd/poller/main.go (load /flux/apns/* from SSM)), p3nx2w2 (Implement orphan GC step in runMidnightFinalizer (Scan flux-devices, conditional cascade-delete)), devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices, devices
  - Stream: 1
  - Requirements: [3.1](requirements.md#3.1), [4.1](requirements.md#4.1), [5.3](requirements.md#5.3), [5.4](requirements.md#5.4), [6.3](requirements.md#6.3)
  - References: infrastructure/template.yaml

## Frontend: FluxCore Notifications

- [x] 22. Write tests for DeviceIdentifier: generate-or-read from container UserDefaults, persistence across launches, reset on uninstall analogue <!-- id:p3nx2w5 -->
  - File: Flux/Packages/FluxCore/Tests/FluxCoreTests/DeviceIdentifierTests.swift
  - Use an injected UserDefaults suite per test (UserDefaults(suiteName:)) so tests are hermetic
  - Cases: missing -> generates UUID and persists; present -> returns same value across reads; clearing the suite -> regenerates a new UUID (uninstall analogue)
  - Stream: 2
  - Requirements: [4.2](requirements.md#4.2)
  - References: Flux/Packages/FluxCore/Tests/FluxCoreTests/DeviceIdentifierTests.swift

- [x] 23. Implement DeviceIdentifier in FluxCore/Notifications/ <!-- id:p3nx2w6 -->
  - File: Flux/Packages/FluxCore/Sources/FluxCore/Notifications/DeviceIdentifier.swift
  - Uses UserDefaults.standard (NOT .fluxAppGroup, NOT Keychain) — Decision 8
  - Public init(userDefaults:) for tests; public static let shared for production callers
  - Blocked-by: p3nx2w5 (Write tests for DeviceIdentifier: generate-or-read from container UserDefaults, persistence across launches, reset on uninstall analogue)
  - Stream: 2
  - Requirements: [4.2](requirements.md#4.2)
  - References: Flux/Packages/FluxCore/Sources/FluxCore/Notifications/DeviceIdentifier.swift

- [x] 24. Write tests for SoCAlertRule and SoCAlertRuleDraft: encoding, label length cap, threshold and HH:MM validation <!-- id:p3nx2w7 -->
  - Encoding round-trip with snake/camel keys matching the wire shape in design.md Wire shapes
  - SoCAlertRuleDraft.validate() rejects threshold outside 1..99, HH:MM outside 00:00..23:59, start == end, label > 40 graphemes (use uniseg-equivalent or simple count for now)
  - Equatable / Hashable for SwiftUI diffing
  - Stream: 2
  - Requirements: [1.2](requirements.md#1.2), [1.3](requirements.md#1.3)
  - References: Flux/Packages/FluxCore/Tests/FluxCoreTests/SoCAlertRuleTests.swift

- [x] 25. Implement SoCAlertRule and SoCAlertRuleDraft value types <!-- id:p3nx2w8 -->
  - Files: Flux/Packages/FluxCore/Sources/FluxCore/Notifications/SoCAlertRule.swift, SoCAlertRuleDraft.swift
  - SoCAlertRule.id = server-assigned UUID; createdAt and updatedAt are Date (Codable maps RFC 3339)
  - Blocked-by: p3nx2w7 (Write tests for SoCAlertRule and SoCAlertRuleDraft: encoding, label length cap, threshold and HH:MM validation)
  - Stream: 2
  - Requirements: [1.2](requirements.md#1.2), [1.3](requirements.md#1.3)
  - References: Flux/Packages/FluxCore/Sources/FluxCore/Notifications/SoCAlertRule.swift, Flux/Packages/FluxCore/Sources/FluxCore/Notifications/SoCAlertRuleDraft.swift

- [x] 26. Write tests for URLSessionAPIClient new methods: register, fetch rules, create, update, and delete rule (request shape, response decoding, error mapping) <!-- id:p3nx2w9 -->
  - URLProtocol-based fake URLSession to assert request URL, method, headers (Authorization Bearer), and body bytes
  - Decode 200/201 success bodies; map 400/401/409/500 to FluxAPIError variants consistent with the existing error mapping
  - Verify Content-Type: application/json on POST/PUT
  - Blocked-by: p3nx2w8 (Implement SoCAlertRule and SoCAlertRuleDraft value types)
  - Stream: 2
  - Requirements: [1.7](requirements.md#1.7), [2.3](requirements.md#2.3), [4.1](requirements.md#4.1), [4.4](requirements.md#4.4), [4.5](requirements.md#4.5)
  - References: Flux/Packages/FluxCore/Tests/FluxCoreTests/URLSessionAPIClientNotificationsTests.swift

- [x] 27. Extend FluxAPIClient protocol, URLSessionAPIClient and MockFluxAPIClient with notification endpoints <!-- id:p3nx2wa -->
  - File edits: Flux/Packages/FluxCore/Sources/FluxCore/Networking/FluxAPIClient.swift (protocol additions), URLSessionAPIClient.swift (impl), Flux/Flux/Services/MockFluxAPIClient.swift (mock honouring the 10-rule cap)
  - Method signatures match design.md Components and Interfaces FluxAPIClient additions
  - Mock stores rules in memory, returns 409 equivalent on the 11th, supports test seeding
  - Blocked-by: p3nx2w9 (Write tests for URLSessionAPIClient new methods: register, fetch rules, create, update, and delete rule (request shape, response decoding, error mapping)), request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request, request
  - Stream: 2
  - Requirements: [1.7](requirements.md#1.7), [2.3](requirements.md#2.3), [4.1](requirements.md#4.1), [4.4](requirements.md#4.4), [4.5](requirements.md#4.5)
  - References: Flux/Packages/FluxCore/Sources/FluxCore/Networking/FluxAPIClient.swift, Flux/Packages/FluxCore/Sources/FluxCore/Networking/URLSessionAPIClient.swift, Flux/Flux/Services/MockFluxAPIClient.swift

- [x] 28. Write tests for SoCAlertsService and NotificationAuthService: idempotent registration, denial path (token nil), optimistic CRUD, retry-on-foreground <!-- id:p3nx2wb -->
  - Idempotent registration: second call with same {token, tz} sends no POST (use a 'lastSent' cache in UserDefaults)
  - Denial path: requestAuthorization returns false -> registerDeviceIfNeeded(token: nil, tz: .current) still POSTs so the device row exists
  - Optimistic CRUD: create returns SoCAlertRule with server id; on POST failure local change kept and lastError is set (AC 1.7)
  - Retry-on-foreground: pending changes re-POST when foregroundHook() is called
  - Blocked-by: p3nx2wa (Extend FluxAPIClient protocol, URLSessionAPIClient and MockFluxAPIClient with notification endpoints)
  - Stream: 2
  - Requirements: [1.7](requirements.md#1.7), [1.8](requirements.md#1.8), [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.3](requirements.md#2.3), [2.4](requirements.md#2.4), [4.1](requirements.md#4.1), [4.4](requirements.md#4.4), [4.5](requirements.md#4.5)
  - References: Flux/Packages/FluxCore/Tests/FluxCoreTests/SoCAlertsServiceTests.swift

- [x] 29. Implement SoCAlertsService (with explicit UIApplication/NSApplication registerForRemoteNotifications call) and NotificationAuthService <!-- id:p3nx2wc -->
  - Files: Flux/Packages/FluxCore/Sources/FluxCore/Notifications/SoCAlertsService.swift, NotificationAuthService.swift
  - Singleton SoCAlertsService.shared on the @MainActor (delegate calls into it via Task { @MainActor in ... })
  - requestAuthorizationAndRegister: on grant, iOS calls UIApplication.shared.registerForRemoteNotifications(); macOS calls NSApplication.shared.registerForRemoteNotifications()
  - Idempotency truth is the backend tzUpdatedAt guard; local UserDefaults cache is only an optimisation
  - Blocked-by: p3nx2w6 (Implement DeviceIdentifier in FluxCore/Notifications/), p3nx2wb (Write tests for SoCAlertsService and NotificationAuthService: idempotent registration, denial path (token nil), optimistic CRUD, retry-on-foreground)
  - Stream: 2
  - Requirements: [1.7](requirements.md#1.7), [1.8](requirements.md#1.8), [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.3](requirements.md#2.3), [2.4](requirements.md#2.4), [4.1](requirements.md#4.1), [4.4](requirements.md#4.4), [4.5](requirements.md#4.5)
  - References: Flux/Packages/FluxCore/Sources/FluxCore/Notifications/SoCAlertsService.swift, Flux/Packages/FluxCore/Sources/FluxCore/Notifications/NotificationAuthService.swift

## Frontend: app delegates

- [x] 30. Move FluxiOSAppDelegate to its own file and add iOS plus macOS APNs delegate callbacks that delegate to SoCAlertsService.shared via Task @MainActor <!-- id:p3nx2wd -->
  - Move: Flux/Flux/Charts/Expansion/iOS/OrientationLock.swift KEEPS only OrientationLock; new file Flux/Flux/iOS/FluxiOSAppDelegate.swift hosts the iOS delegate
  - Add to iOS delegate: didRegisterForRemoteNotificationsWithDeviceToken and didFailToRegisterForRemoteNotificationsWithError — both wrap the SoCAlertsService.shared call in Task { @MainActor in ... }
  - Add to macOS Flux/Flux/Mac/FluxAppDelegate.swift: applicationDidFinishLaunching(_:) and the two register-token callbacks
  - Note: macOS NSApplication.registerForRemoteNotifications does NOT prompt for permission; the prompt comes from UNUserNotificationCenter.requestAuthorization via SoCAlertsService
  - Blocked-by: p3nx2wc (Implement SoCAlertsService (with explicit UIApplication/NSApplication registerForRemoteNotifications call) and NotificationAuthService)
  - Stream: 2
  - Requirements: [2.4](requirements.md#2.4), [4.4](requirements.md#4.4)
  - References: Flux/Flux/iOS/FluxiOSAppDelegate.swift, Flux/Flux/Charts/Expansion/iOS/OrientationLock.swift, Flux/Flux/Mac/FluxAppDelegate.swift

## Frontend: settings UI

- [x] 31. Write tests for SoCAlertsViewModel: validation parity with AC 1.3, save/edit/delete flows, 10-rule cap enforcement, banner state on errors <!-- id:p3nx2we -->
  - Validation parity with AC 1.3: invalid threshold / HH:MM / start==end disables Save; label > 40 chars disables Save
  - Cap enforcement: when SoCAlertsService.rules.count >= 10, addAffordanceEnabled == false
  - Banner state: when SoCAlertsService.lastError != nil OR authStatus == .denied, the appropriate banner toggles true; clearError dismisses the error banner
  - Blocked-by: p3nx2wc (Implement SoCAlertsService (with explicit UIApplication/NSApplication registerForRemoteNotifications call) and NotificationAuthService)
  - Stream: 2
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.5](requirements.md#1.5), [1.7](requirements.md#1.7), [1.8](requirements.md#1.8), [2.2](requirements.md#2.2)
  - References: Flux/FluxTests/Settings/SoCAlertsViewModelTests.swift

- [x] 32. Implement SoCAlertsViewModel <!-- id:p3nx2wf -->
  - File: Flux/Flux/Settings/SoCAlerts/SoCAlertsViewModel.swift
  - @Observable wrapping SoCAlertsService; exposes editor draft, validation errors, save action, sheet presentation state
  - Blocked-by: p3nx2we (Write tests for SoCAlertsViewModel: validation parity with AC 1.3, save/edit/delete flows, 10-rule cap enforcement, banner state on errors)
  - Stream: 2
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.5](requirements.md#1.5), [1.7](requirements.md#1.7), [1.8](requirements.md#1.8), [2.2](requirements.md#2.2)
  - References: Flux/Flux/Settings/SoCAlerts/SoCAlertsViewModel.swift

- [x] 33. Implement SoCAlertsView (list, empty / permission-denied / cap-reached states, banner) <!-- id:p3nx2wg -->
  - File: Flux/Flux/Settings/SoCAlerts/SoCAlertsView.swift
  - Empty state, list state, permission-denied banner (matches the validationError red-text pattern in SettingsView.swift), cap-reached disables Add
  - Form + Section on iOS; LiquidGlassSection on macOS — reuses the SettingsView pattern
  - Tapping a row opens SoCAlertEditor (sheet on iOS, sheet/inspector on macOS)
  - Blocked-by: p3nx2wf (Implement SoCAlertsViewModel)
  - Stream: 2
  - Requirements: [1.1](requirements.md#1.1), [1.5](requirements.md#1.5), [1.6](requirements.md#1.6), [2.2](requirements.md#2.2)
  - References: Flux/Flux/Settings/SoCAlerts/SoCAlertsView.swift

- [x] 34. Implement SoCAlertEditor sheet (create/edit) <!-- id:p3nx2wh -->
  - File: Flux/Flux/Settings/SoCAlerts/SoCAlertEditor.swift
  - Fields: threshold (Stepper or TextField), windowStart (DatePicker .hourAndMinute), windowEnd, enabled Toggle, optional label TextField
  - Validation fires on field-edit and on save (per design.md Testing Strategy)
  - Blocked-by: p3nx2wf (Implement SoCAlertsViewModel)
  - Stream: 2
  - Requirements: [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [1.8](requirements.md#1.8)
  - References: Flux/Flux/Settings/SoCAlerts/SoCAlertEditor.swift

- [x] 35. Wire SoC Alerts entry into SettingsView (iOS Form Section + macOS LiquidGlassSection) <!-- id:p3nx2wi -->
  - File edit: Flux/Flux/Settings/SettingsView.swift
  - iOS: add a new Section('Alerts') with a NavigationLink to SoCAlertsView; macOS: add a LiquidGlassSection with the same destination
  - Hide the entry when SoCAlertsService.shared is not yet bound (e.g., FluxApp init order check) — though in practice it's bound in FluxApp.init
  - Blocked-by: p3nx2wg (Implement SoCAlertsView (list, empty / permission-denied / cap-reached states, banner)), reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, reached, p3nx2wh (Implement SoCAlertEditor sheet (create/edit))
  - Stream: 2
  - Requirements: [1.1](requirements.md#1.1)
  - References: Flux/Flux/Settings/SettingsView.swift
