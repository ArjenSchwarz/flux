# Decision Log: SoC Alerts

## Decision 1: Server-side evaluation with APNs push

**Date**: 2026-05-19
**Status**: accepted

### Context

The iOS app polls `/status` only while a relevant view is alive. macOS adds a 60 s inactive cadence but stops on app quit. iOS background app refresh is best-effort and runs at intervals far longer than the 10 s polling cadence the feature needs. The feature only delivers value when alerts fire while the user is *not* using the app — which is most of the time.

### Decision

Evaluate threshold crossings in the existing Go poller running on ECS Fargate and deliver alerts via APNs.

### Rationale

The poller already runs every 10 s, already reads the live SoC, and is the only component guaranteed to be running when the user is away from the app. APNs is the standard delivery channel for iOS/macOS push and the project's entitlements are already provisioned (`aps-environment` in `Flux.entitlements`, `remote-notification` in `Info.plist`), so the only new piece is the sender.

### Alternatives Considered

- **Local notifications scheduled by the app**: Rejected — the app cannot reliably evaluate live SoC when not running, so alerts would only fire when the user is already looking at the app.
- **Silent background pushes that wake the app to evaluate locally**: Rejected — adds a remote-trigger leg without removing the need for server-side state and push, and still depends on background execution budgets that iOS does not guarantee.

### Consequences

**Positive:**
- Alerts fire on time regardless of app state.
- Reuses the existing poll cycle and AlphaESS read path.
- Per-rule state (last fired, previous SoC) lives next to the readings it depends on.

**Negative:**
- Introduces a new external dependency (APNs) with key rotation and stale-token handling.
- Adds two new DynamoDB tables and a per-poll evaluation pass.

---

## Decision 2: "At most once per local day" re-fire policy

**Date**: 2026-05-19
**Status**: accepted

### Context

A naive "fire every time SoC crosses the threshold downward" implementation produces notification spam when SoC oscillates around the threshold (loads pulsing on and off, brief solar bursts). A naive "fire while below" produces continuous spam.

### Decision

Each rule fires at most once per local-day-of-window for the device, regardless of how many times the threshold is re-crossed.

### Rationale

The user picked the simplest re-arm semantics in the scope assessment: "Once per window per day." A daily counter is straightforward to express as `(deviceId, ruleId, localDateOfWindowStart)` and does not require choosing a hysteresis delta. The trade-off is explicit: if the battery recovers and dips again later in the same window, the user does not get a second alert. This was accepted.

### Alternatives Considered

- **Edge + hysteresis (rearm after rising threshold + 5%)**: Rejected as more configuration surface than needed; behaviour is also harder to reason about when the user looks at the day's chart and sees multiple crossings but only some alerts.
- **Level-triggered (continuous)**: Rejected — produces spam.

### Consequences

**Positive:**
- No hysteresis delta to expose or tune.
- Idempotent: replaying a poll cycle cannot double-fire.

**Negative:**
- A second within-window crossing is silently swallowed.

---

## Decision 3: Per-device rule scope, not per-account

**Date**: 2026-05-19
**Status**: accepted

### Context

The Flux backend has no user identity model today — both users share a single bearer token. Notes and the off-peak window are shared state. The question is whether SoC alert rules should also be shared (both users get every alert) or scoped per device.

### Decision

Each device manages its own list of rules, keyed by a stable device identifier independent of the APNs token.

### Rationale

Sleep schedules and personal preferences differ between the two users; one user editing another user's rules would be a poor UX. Per-device scope also lets the user run different rules on their iPhone vs. iPad vs. Mac if they want. A stable device identifier separate from the APNs token means token rotation does not lose rules.

### Alternatives Considered

- **Shared between both users on the bearer token**: Rejected — see Rationale.
- **Identity introduced (e.g., named profiles, iCloud user)**: Rejected — over-engineered for two users; can be added later if needed.

### Consequences

**Positive:**
- Independent rules per device match user expectation.
- No new identity concept needed in the backend.

**Negative:**
- Rules do not follow the user across devices or survive reinstall — accepted in Decision 5.

---

## Decision 4: Device-local time zone interpretation

**Date**: 2026-05-19
**Status**: accepted

### Context

A rule's window ("17:00–24:00") could be interpreted in the backend's TZ (Sydney) or in the device's local TZ. The backend already uses Sydney for off-peak window boundaries.

### Decision

Each device sends its current IANA time-zone identifier on registration and on TZ change, along with a monotonic `tzUpdatedAt` counter; the backend evaluates each rule's window using the owning device's TZ, captured at the start of the evaluation cycle.

### Rationale

A rule named "evening cooking alert" should mean evening *where the user is*, not 17:00 Sydney time when the user is travelling. Storing TZ per device localises the impact to the registration call path and avoids embedding TZ-conversion logic in the rule itself. The `tzUpdatedAt` counter prevents lost-update races when the user travels through multiple TZs and the app foregrounds out of order. Worked window examples now use "17:00–00:00" (cross-midnight, end-of-day) rather than "17:00–24:00", because HH:MM has no 24:00 — see [Decision 7](#decision-7-end-of-day-is-expressed-as-0000-not-2400).

### Alternatives Considered

- **Server TZ (Australia/Sydney)**: Rejected — wrong when the user travels; couples a user-facing setting to a backend implementation detail.
- **TZ stored on the rule itself**: Rejected — extra UI for something the device already knows, and rules would not follow the user across TZ changes without an edit.

### Consequences

**Positive:**
- Rules behave intuitively across travel.
- Device TZ is a single piece of state that updates cheaply.

**Negative:**
- Backend must do per-device TZ math each poll cycle (cheap, but non-zero).

---

## Decision 5: Rules are lost on app reinstall

**Date**: 2026-05-19
**Status**: accepted

### Context

When a user reinstalls the app or sets up a new device, the device identifier resets. The question is whether rules should be restored from somewhere (e.g., iCloud KVS like the API URL today) or whether the user re-creates them.

### Decision

The device identifier resets on reinstall and the user re-creates rules on the new install.

### Rationale

The two users share an iCloud Family with shared Keychain for credentials. Mirroring rules through `NSUbiquitousKeyValueStore` would either duplicate rules across both users or require an identity concept we have explicitly avoided (Decision 3). Re-creating up to 10 rules on a new device is a small one-time cost.

### Alternatives Considered

- **iCloud KVS rule mirroring**: Rejected — see Rationale.
- **Server-side restore by some identity proxy**: Rejected — no identity model exists.

### Consequences

**Positive:**
- No iCloud sync code; no rule de-duplication logic.
- Old device records garbage-collect naturally once APNs marks the token invalid.

**Negative:**
- Reinstall is a small chore for the user.

---

## Decision 6: Downward-crossing direction only

**Date**: 2026-05-19
**Status**: accepted

### Context

The user story is "notify when battery hits a percentage", which in context (an evening usage alert at 40%) clearly means dropping to. A rising-edge variant ("notify when fully charged to 80% so I can divert excess") is a plausible adjacent feature but was not asked for.

### Decision

Rules fire only on downward crossings of the threshold.

### Rationale

Matches the user story directly. Halves the UI surface (no direction toggle on the rule editor). A rising-edge feature can be added later as a second rule type without breaking existing rules.

### Alternatives Considered

- **User-selectable direction per rule**: Rejected — not in the user story; expands UI and design surface for an unproven need.

### Consequences

**Positive:**
- Smaller rule UI; smaller backend evaluator surface.

**Negative:**
- Rising-edge use cases will require a future feature.

---

## Decision 7: End-of-day is expressed as 00:00, not 24:00

**Date**: 2026-05-19
**Status**: accepted

### Context

A rule like "17:00 to midnight" needs a representation. The user-facing notation "17:00–24:00" is intuitive but HH:MM (24-hour) only ranges over 00:00–23:59, and accepting "24:00" as a special token introduces a parsing exception everywhere the value is read.

### Decision

End-of-day is expressed as `00:00`. Any rule with start strictly later than end (start > end) is interpreted as crossing midnight, ending at the end time on the following local day. "17:00–00:00" therefore means "17:00 today through 00:00 tomorrow" = end of today.

### Rationale

Folds end-of-day cleanly into the cross-midnight rule already needed for "22:00–06:00". No special tokens, no parsing exceptions, no off-by-one at minute 23:59 vs 00:00. The model is uniform: `start > end ⇒ window crosses midnight`.

### Alternatives Considered

- **Accept "24:00" as a synonym for "00:00 next day"**: Rejected — special-cases the parser and every comparison site for one notation that's not actually clearer once users learn the cross-midnight model.
- **End-of-day is "23:59"**: Rejected — silently drops the last 59 seconds of the day.

### Consequences

**Positive:**
- One consistent rule for cross-midnight semantics.
- Parser accepts the strict HH:MM format with no exceptions.

**Negative:**
- "00:00" as an end value reads as "midnight" only after the user internalises the cross-midnight model. The UI can mitigate by labelling the picker option as "Midnight (end of day)".

---

## Decision 8: Stable device identifier in app-container UserDefaults, not Keychain

**Date**: 2026-05-19
**Status**: accepted

### Context

The backend keys rules and fire-state by a stable device identifier separate from the APNs token (Decision 3). The identifier must (a) survive APNs token rotation and OS upgrades, and (b) reset on app uninstall so reinstall starts fresh (Decision 5). The two natural storage options behave differently:

- **Keychain** persists across app uninstall on iOS by default.
- **App-container `UserDefaults`** is deleted with the app.

### Decision

Generate a UUID on first launch and store it in the app's container `UserDefaults` (not Keychain, not the app-group `UserDefaults`).

### Rationale

The container-scoped `UserDefaults` is reset by uninstall, which is exactly Decision 5's intent. The app-group `UserDefaults` is shared with the widget extension and would survive the main app being deleted while the widget remained — so it is also wrong here. Keychain would directly contradict Decision 5. A first-launch UUID is collision-free at the cardinality this project will ever reach.

### Alternatives Considered

- **Keychain**: Rejected — survives uninstall, breaks Decision 5.
- **`identifierForVendor`**: Rejected — undefined on macOS; resets when no vendor apps are installed on iOS, which is OS-implicit rather than user-explicit.
- **App-group `UserDefaults`**: Rejected — survives uninstall of the main app if the widget is still installed.

### Consequences

**Positive:**
- Reinstall reliably resets the identifier across iOS and macOS.
- No platform branch in the storage call.

**Negative:**
- Migrating away from app-container storage later (e.g., to support an explicit "transfer rules to new device" flow) would require a one-shot migrator. Acceptable.

---

## Decision 9: Fire-state row is written before the APNs submit

**Date**: 2026-05-19
**Status**: accepted

### Context

A crash between detecting a crossing, writing fire-state, and submitting the push has three orderings, each with a different failure mode:
1. Submit first, then write — duplicate push on retry.
2. Write first, then submit — silent miss on retry (push lost).
3. Two-phase commit — complex and unjustified at this scale.

### Decision

Write the fire-state row first, then submit the push. Use the `(deviceId, ruleId, windowStartDate)` triple as both the fire-state key and the APNs `apns-collapse-id`.

### Rationale

At this feature's value level — a polite reminder, not a safety-critical alert — a silent miss after a poller crash is preferable to a duplicate notification. The collapse-id provides a second layer of defence: even if a duplicate push slips through (e.g., a rare write-succeeded-but-Dynamo-replied-error case), APNs collapses it on the device.

### Alternatives Considered

- **Submit-then-write**: Rejected — produces duplicates on retry, which is a worse UX than a missed alert.
- **Two-phase commit / outbox**: Rejected — over-engineered for a single-writer poller.

### Consequences

**Positive:**
- At-most-once delivery semantics without distributed coordination.
- Collapse-id makes accidental duplicates user-invisible.

**Negative:**
- A crash between write and submit produces a silent miss for that crossing; the rule will not re-fire today.

---

## Decision 10: APNs transient-failure retry — 3 attempts, exponential backoff, then drop

**Date**: 2026-05-19
**Status**: accepted

### Context

APNs returns 5xx, 429, and connection errors transiently. A no-retry policy throws away delivery; an unbounded retry queue blocks the next poll cycle. The poller is a single process; there is no dead-letter queue today.

### Decision

Retry up to 3 times with exponential backoff (base 1 s, factor 2, jittered 0.5×–1.5×). If retries are exhausted, drop the push and emit an observability event. The fire-state row is kept, so the rule does not re-fire later today.

### Rationale

Three retries with jittered backoff covers the common transient cases (brief connection blips, APNs server hiccups) without delaying the next poll cycle by more than ~7 seconds in the worst case. Keeping the fire-state row on failure prefers a silent miss over a delayed second attempt that could surprise the user hours later when the underlying APNs problem clears.

### Alternatives Considered

- **No retries**: Rejected — throws away easy recoveries.
- **Persistent retry queue / DLQ**: Rejected — adds infrastructure for a low-volume failure mode.
- **Clear fire-state on exhausted retries**: Rejected — would cause re-fire later today, which can land hours late when the user no longer cares.

### Consequences

**Positive:**
- Bounded per-cycle worst-case latency.
- Common transient failures recover invisibly.

**Negative:**
- Sustained APNs outages produce silent misses with no automatic recovery once today's fire-state is set.

---

## Decision 11: Previous-in-window-SoC comparator held in-process

**Date**: 2026-05-19
**Status**: accepted

### Context

[AC 3.2](requirements.md#3.2) initially required the previous-in-window-SoC comparator to "persist across poller restarts." A literal reading mandates a Dynamo write on every poll cycle per rule: at the load cap (10 devices × 10 rules × 6 cycles/min × 60 min × 24 h) this is ~8.6 million writes/day — an order of magnitude above the project's current ~260k writes/month.

### Decision

The comparator is held only in the poller's process memory. On poller restart, the seed-on-first-reading rule ([AC 3.3](requirements.md#3.3)) re-establishes correctness. AC 3.2 was relaxed from "SHALL persist" to "MAY persist; absent persistence, at most one alert per (device, rule) may be lost per restart."

### Rationale

The miss case is small: a downward crossing landing inside the (restart, +10 s) gap. The poller restarts only on deploys (manual) and crashes (rare). The cost of persistence (8M writes/day or a flush job + tiny new table) is disproportionate to the value protected.

### Alternatives Considered

- **Persist on every reading**: Rejected — 8M writes/day at the cap, no benefit beyond covering the ~10 s restart window.
- **Persist on each fire only (piggy-back on the fire-state row)**: Rejected as insufficient — only protects rules that have already fired today; the more common pre-fire crossing is unprotected.
- **Periodic flush every 60 s to a new comparator table**: Rejected as added infrastructure for a small miss window.

### Consequences

**Positive:**
- No new persistence path, no extra table, no flush goroutine.
- Per-cycle work is read-only against the rules cache.

**Negative:**
- A crossing in the first 10 s after a restart is silently missed for that day.

---

## Decision 12: Orphan-device GC piggybacks on the existing midnight finalizer

**Date**: 2026-05-19
**Status**: accepted

### Context

[AC 4.6](requirements.md#4.6) requires garbage-collecting device records that have not successfully registered for 30 days. Three realisations were considered: a separate Lambda on an EventBridge schedule, DynamoDB TTL on `lastRegisteredAt`, or extending the poller's existing midnight pass.

### Decision

Extend the midnight pass in `runMidnightFinalizer` (`internal/poller/poller.go:225`) with a new step that scans `flux-devices`, deletes rows older than 30 days, and cascades the delete to that device's rules and fire-state.

### Rationale

The midnight pass already exists, runs daily, and is the natural home for low-frequency housekeeping. DynamoDB TTL was rejected because it does not cascade — orphan rules and fire-state would need a Streams-driven cleanup Lambda, which is more infrastructure than the pass extension. A separate Lambda is over-engineered for a workload bounded by ~10–20 devices ever.

### Alternatives Considered

- **Separate scheduled Lambda**: Rejected — new resource, new IAM policy, new log group, for one daily Scan + filtered deletes.
- **DynamoDB TTL**: Rejected — only deletes the device row; child rows orphan.

### Consequences

**Positive:**
- Zero new infrastructure.
- Cascading delete keeps the data model clean (no zombie rules pointing at a non-existent device).

**Negative:**
- The midnight pass adds one Scan per day to its critical path. Scan is `flux-devices`, which is small; cost is negligible.

---

## Decision 13: APNs client library is `github.com/sideshow/apns2`

**Date**: 2026-05-19
**Status**: accepted

### Context

The poller needs to deliver pushes to APNs over HTTP/2 with token-based auth (`.p8` + key id + team id). Options: write a custom HTTP/2 + JWT client, or adopt a maintained library.

### Decision

Use `github.com/sideshow/apns2`.

### Rationale

It is the de facto Go APNs library, MIT-licensed, a single dependency, with HTTP/2 connection reuse and built-in JWT refresh on the 50-minute Apple-mandated cadence. Writing a custom client would replicate ~150 LOC of well-trodden behaviour for no benefit at this scale.

### Alternatives Considered

- **Custom client**: Rejected — no benefit, reinvents JWT and HTTP/2 connection management.
- **AWS SNS push**: Rejected — adds an AWS service to the path, decouples retry/observability from the rest of the poller, and SNS's APNs integration is a thin wrapper over the same library protocol anyway.
- **`edganiukov/apns`**: Rejected — older, smaller community, no demonstrated advantage.

### Consequences

**Positive:**
- One additional Go module dependency, with stable releases.
- JWT lifecycle handled by the library.

**Negative:**
- Inherits the library's release cadence. The project tolerates this for AWS SDKs already.

---

## Decision 14: APNs collapse-id is a short hash, not the raw triple

**Date**: 2026-05-19
**Status**: accepted

### Context

The design originally proposed `deviceId|ruleId|YYYY-MM-DD` as the `apns-collapse-id`. With two UUIDs and a date, the encoded form is ~84 bytes. Apple's APNs spec caps `apns-collapse-id` at 64 bytes; longer values cause APNs to reject the push.

### Decision

Use `base64url(SHA-256(deviceId|ruleId|windowStartDate))[:22]` — 22 base64url characters carrying 132 bits of entropy, well under the 64-byte cap and far above the cardinality this feature can ever produce.

### Rationale

A truncated SHA-256 over the same triple gives identical-on-equality, different-on-difference semantics (the only property the collapse-id needs), at a bounded length. Storing the same value on `SoCFireStateItem.APNsCollapseID` lets ops cross-reference an APNs log entry to the fire-state row.

### Alternatives Considered

- **Raw triple**: Rejected — exceeds the 64-byte cap.
- **Truncated raw triple**: Rejected — loses dedup property when two distinct (device, rule) prefixes collide post-truncation.
- **UUIDv5 over the triple**: Rejected — UUIDv5 is 36 characters, larger than the hash and less obvious as a hash to a future reader.

### Consequences

**Positive:**
- Compliant with APNs.
- Same fixed length for all collapse-ids, simplifying log filtering.

**Negative:**
- One extra hash per fire — negligible CPU.

---

## Decision 15: Pushes are dispatched to a bounded worker pool

**Date**: 2026-05-19
**Status**: accepted

### Context

The evaluator originally called `Notifier.Push` synchronously from the live-poll goroutine. With APNs RTT of tens to hundreds of ms (plus up to 3 retries × 5 s timeout × jittered backoff), a single slow push could stall the next 10-second poll cycle. `time.NewTicker` coalesces missed ticks, so a stall causes silent loss of `flux-readings` writes.

### Decision

`Evaluator.Evaluate` writes the fire-state row synchronously (so its idempotence guarantee holds against a crash before push), then `Enqueue`s the push onto a 64-deep bounded queue drained by 4 worker goroutines that call `Notifier.Push` and handle stale-token bookkeeping.

### Rationale

Decouples APNs RTT from the AlphaESS poll cadence. Evaluator per-cycle wall-time is now bounded by Dynamo work plus a non-blocking channel send. The queue cap (64) is far above steady-state demand for a feature with ≤10 rules × ≤20 lifetime devices; overflow logs and drops, leaving the fire-state row in place (no re-fire today).

### Alternatives Considered

- **Synchronous from the evaluator** (original design): Rejected — couples poll cadence to APNs RTT.
- **Unbounded queue**: Rejected — masks back-pressure; an APNs outage produces a memory leak.
- **Per-cycle goroutine spawn**: Rejected — no upper bound on goroutine count; same back-pressure problem.

### Consequences

**Positive:**
- Live-poll goroutine never blocks on APNs.
- Bounded memory and concurrency.

**Negative:**
- One more concurrent component to reason about (logged failures + ordering).

---

## Decision 16: `prev` comparator keyed by `(deviceRule, windowStartDate)` and tagged with rule `UpdatedAt`

**Date**: 2026-05-19
**Status**: accepted

### Context

Two correctness gaps surfaced during design review:
1. **Yesterday → today carry-over.** A `prev` map keyed only by `(deviceId, ruleId)` carries yesterday's last in-window SoC into today's first reading. If yesterday's last reading was above threshold and today's first reading is below, the rule would fire at window open even though SoC did not drop during today's active window.
2. **Rule edits not propagated.** AC 5.3 requires the comparator to reset on rule edit/re-enable, but the Lambda (which handles the edit) cannot reach the poller's in-process map.

### Decision

The `prev` map's key is `(deviceRule, windowStartDate)`; the value carries both the SoC and the rule's `UpdatedAt` value seen when the entry was set. The evaluator checks `UpdatedAt` on every lookup; on mismatch (rule has changed since the entry was set), the entry is deleted and the next in-window reading seeds.

### Rationale

Adding `windowStartDate` to the key closes the cross-day carry-over by construction. Adding the `UpdatedAt` tag closes the cross-process state propagation gap without IPC: the Lambda bumps `UpdatedAt` on every PUT (and the cache picks it up within 30 s), and the evaluator self-corrects on the next cycle. The combination is cheaper and simpler than any of the IPC/streams alternatives.

### Alternatives Considered

- **Key by `(deviceRule)` only**: Rejected — see (1).
- **Stream Lambda mutations to the poller**: Rejected — adds DynamoDB Streams + a Lambda + a poller listener for state that the poller can self-correct from in 30 s.
- **Periodic full reset of `prev`**: Rejected — too aggressive; reseeds across all devices when only one rule changed.

### Consequences

**Positive:**
- Both correctness gaps closed by one structural change to the key + tag.
- No IPC; Lambda mutates its rows and the poller catches up.

**Negative:**
- The poller's `prev` map can briefly disagree with the latest rule definition (up to 30 s + 10 s = 40 s). The seed-on-first-reading rule ensures no spurious fire during this window.

---

## Decision 17: Lambda gains `Query`+`DeleteItem` on `flux-soc-fire-state`

**Date**: 2026-05-19
**Status**: accepted

### Context

AC 5.3 and 5.4 require fire-state to be cleared when a rule is edited, re-enabled, or deleted. The earlier IAM scoping ("Lambda has no read or write reason to touch fire state") was incompatible with this requirement.

### Decision

`LambdaExecutionRole` is granted `dynamodb:Query` and `dynamodb:DeleteItem` on `SocFireStateTable`. The Lambda's PUT and DELETE handlers, after mutating the rule row, perform a `Query` for fire-state rows under `deviceRule = deviceId#ruleId` and `DeleteItem` each. The cleanup is best-effort: failures are logged, and the poller's `UpdatedAt`-tagged `prev` map (Decision 16) guarantees evaluator correctness regardless. Stale fire-state rows TTL out after 7 days.

### Rationale

Without this grant, AC 5.3 and 5.4 are unimplementable. The IAM widening is narrow (no read/write on the comparator-equivalent rows that the poller owns; `Query`+`DeleteItem` only).

### Alternatives Considered

- **Poller-side cleanup only**: Rejected — would require the poller to detect that a rule was deleted (cache absence) and then run cleanup; conflates concerns and delays cleanup by up to one cache refresh.
- **DynamoDB Streams + cleanup Lambda**: Rejected — additional infrastructure for a one-line Query+Delete.

### Consequences

**Positive:**
- AC 5.3 and 5.4 implementable.
- Stale fire-state cleared at the source (the mutation event).

**Negative:**
- Lambda's blast radius increases by one table. Mitigated by the cleanup being scoped to the rule's own `deviceRule` PK.

---
