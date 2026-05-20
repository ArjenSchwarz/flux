# Requirements: SoC Alerts

## Introduction

Adds user-defined battery state-of-charge alerts. A user creates rules on a device — each rule pairs a percentage threshold with a daily active time window — and receives a push notification when the live SoC drops to that threshold while the window is active. The Go poller evaluates rules server-side every cycle and dispatches APNs pushes, because the app is not running 24/7 and local-only notifications cannot observe the threshold reliably.

## Non-Goals

- Rising-edge alerts ("notify when charged to X%").
- Alerts while the rule's time window is not active.
- Cross-device rule sync, iCloud restore, or rule sharing between the two account users.
- Snooze, mute, or per-fire dismiss controls (the user deletes or disables the rule instead — see [1.1](#1.1), [1.8](#1.8)).
- Rich notification payloads (action buttons, attached images, thread grouping beyond the dedup collapse-id).
- Alerts to iOS or macOS versions below the current app deployment target.
- Configurable re-fire policy per rule — re-fire is fixed at "at most once per rule per local-window-start day" (see [3.4](#3.4)).
- Rising-edge variants, all-day windows (start == end is rejected), and sub-minute granularity (HH:MM only).
- Mitigation against a misbehaving device using the shared bearer token to post on behalf of another device's identifier. The two-user shared-token model treats both users as trusted; per-device integrity is not enforced.

## Requirements

### 1. Rule Management on the Device

**User Story:** As a user, I want to create, edit, and delete multiple SoC alert rules on my device, so that I can be notified about different battery levels in different time windows.

**Acceptance Criteria:**

1. <a name="1.1"></a>The app SHALL provide a Settings screen (iOS and macOS) listing the device's existing SoC alert rules and an entry to create a new one.
2. <a name="1.2"></a>The app SHALL allow each rule to specify: an integer percentage threshold in 1–99, a window start time HH:MM (24-hour, device local), a window end time HH:MM (24-hour, device local), an enabled toggle, and an optional free-text label (≤40 characters) shown in the rule list and the notification body.
3. <a name="1.3"></a>The app SHALL reject saving a rule whose threshold is outside 1–99, whose start or end is not parseable as HH:MM in 00:00–23:59, or whose start equals end.
4. <a name="1.4"></a>WHEN a rule's window has start strictly later than end (e.g., 22:00–06:00 or 17:00–00:00) THEN the app SHALL treat the window as crossing midnight, ending at the given end time on the following local day; an end of 00:00 therefore means "end of the day after the start."
5. <a name="1.5"></a>The app SHALL cap the number of rules per device at 10 (a deliberately small cap reflecting the expected number of distinct daily patterns) and SHALL disable the "add rule" affordance when the cap is reached.
6. <a name="1.6"></a>The app SHALL persist rules across app launches and SHALL show the list in creation order, preserving each rule's position when it is edited.
7. <a name="1.7"></a>WHEN the user saves an edit or deletion THEN the local list SHALL reflect the change immediately, the app SHALL POST the change to the backend, and IF the POST fails THEN the app SHALL keep the local change pending and retry on the next foreground until it succeeds.
8. <a name="1.8"></a>The app SHALL allow the user to disable a rule without deleting it; disabled rules SHALL NOT trigger notifications.

### 2. Notification Permission

**User Story:** As a user, I want the app to request permission to send notifications when I first open the alerts screen, so that I understand what I'm enabling.

**Acceptance Criteria:**

1. <a name="2.1"></a>WHEN the user opens the SoC alerts Settings screen AND `UNAuthorizationStatus` is `.notDetermined` THEN the app SHALL request notification authorisation (alerts).
2. <a name="2.2"></a>WHEN authorisation is denied THEN the app SHALL display a banner on the SoC alerts Settings screen distinguishing denial from "backend unreachable" and "no rules configured", and SHALL provide a button that deep-links to the system Settings for this app.
3. <a name="2.3"></a>WHEN authorisation is denied THEN the app SHALL still allow viewing, creating, editing, and deleting rules and SHALL register the device record with the backend without an APNs token; the token SHALL attach to the existing device record once authorisation is later granted (see [4.1](#4.1)).
4. <a name="2.4"></a>WHEN authorisation transitions from denied to granted THEN the app SHALL submit the APNs token to the backend before returning control from the next `applicationDidBecomeActive` (iOS) / `applicationDidBecomeActive` (macOS) handler.

### 3. Server-Side Evaluation and Delivery

**User Story:** As a user, I want the backend to detect threshold crossings even when the app is closed, so that I receive alerts on time without keeping the app open.

**Acceptance Criteria:**

1. <a name="3.1"></a>The backend SHALL evaluate every enabled rule for every device once per poller cycle and SHALL skip evaluation entirely for devices with zero enabled rules.
2. <a name="3.2"></a>The backend SHALL fire a rule WHEN the most recent SoC reading is at or below the rule's threshold AND the previous in-window evaluated SoC for the same (device, rule) was strictly above the threshold. "Previous in-window evaluated SoC" is the last SoC observed for this (device, rule) during an active window. The implementation MAY persist this value across poller restarts; absent persistence, correctness on restart relies on AC 3.3 and at most one alert per (device, rule) may be lost per restart.
3. <a name="3.3"></a>WHEN the poller starts with no prior in-window SoC for a (device, rule) THEN the first in-window reading SHALL only seed the comparator and SHALL NOT fire the rule, even if the value is at or below the threshold.
4. <a name="3.4"></a>The backend SHALL skip evaluation for the cycle WHEN the most recent SoC reading is older than 60 seconds, missing, or outside 0–100; in those cases the in-window comparator SHALL NOT advance and the cycle SHALL record a skipped-evaluation observability event ([6.4](#6.4)).
5. <a name="3.5"></a>The backend SHALL skip firing a rule WHEN the device's current local time falls outside the rule's window. The window is interpreted in the device's currently-registered IANA time zone, evaluated at the start of the cycle; subsequent TZ changes within the same cycle do not affect the cycle.
6. <a name="3.6"></a>The backend SHALL fire each rule at most once per "window-start day": the local calendar date in the device's TZ at which the rule's window most recently opened. The counter SHALL reset at the next window opening for that rule, NOT at local midnight. Worked examples: (a) window 17:00–00:00, fire at 21:30 on 2026-06-01 → counter set to 2026-06-01; next eligible fire is at the 17:00 opening on 2026-06-02. (b) window 22:00–06:00, fire at 02:00 on 2026-06-02 → counter set to 2026-06-01 (the opening day); next eligible fire is at the 22:00 opening on 2026-06-02.
7. <a name="3.7"></a>The backend SHALL include in the push payload at minimum: the rule's threshold, the observed SoC at the time of firing, and a stable event identifier `(deviceId, ruleId, windowStartDate)` that SHALL be used as the APNs `apns-collapse-id` so a duplicate fire (e.g., after a poller crash mid-write) is collapsed by APNs.
8. <a name="3.8"></a>The backend SHALL write the `(deviceId, ruleId, windowStartDate)` fire-state record BEFORE submitting the push, so a crash between write and submit leaves at most a silent miss rather than a duplicate, and a re-evaluation post-crash sees the state and skips.
9. <a name="3.9"></a>WHEN APNs reports a token as invalid or unregistered THEN the backend SHALL mark the device's token field as stale and SHALL NOT attempt further pushes to that token until the device re-registers; the device row, its rules, and fire-state SHALL be retained so the next registration recovers them.
10. <a name="3.10"></a>WHEN APNs returns a 5xx, 429, or transport error THEN the backend SHALL retry up to 3 times with exponential backoff (base 1 s, factor 2, jitter 0.5×–1.5×); IF retries are exhausted THEN the push SHALL be dropped, an observability event SHALL record the failure ([6.4](#6.4)), and the fire-state row SHALL remain (so the rule does not re-fire today).
11. <a name="3.11"></a>The backend SHALL submit the push to APNs within 5 seconds of the poll cycle that detected the crossing, and SHALL log the APNs HTTP status and round-trip time per push. End-user delivery latency is out of scope as it is not server-observable.

### 4. Device Registration and Lifecycle

**User Story:** As a user, I want my device to be tracked by the backend so it knows where to send notifications, but I don't want my rules tied to a token that rotates without me knowing.

**Acceptance Criteria:**

1. <a name="4.1"></a>The app SHALL register the device with the backend on first foreground after opening the SoC alerts Settings screen, sending: a stable device identifier, the current APNs device token (or null if authorisation has not been granted), the device's current IANA time-zone identifier, the platform (iOS or macOS), and a monotonic `tzUpdatedAt` counter (or wall-clock timestamp) that increases with each TZ change.
2. <a name="4.2"></a>The stable device identifier SHALL be a UUID generated on first launch and stored in the app's container `UserDefaults` (NOT in Keychain), so it is reset by app uninstall as required by [4.5](#4.5).
3. <a name="4.3"></a>The backend SHALL key all device-scoped records (rules, fire-state, token) by the stable device identifier; the APNs token SHALL NEVER be used as a primary key, rule owner key, or fire-state key.
4. <a name="4.4"></a>WHEN the APNs device token changes THEN the app SHALL send the new token to the backend, keyed by the same stable device identifier, on the next foreground.
5. <a name="4.5"></a>WHEN the device's time zone changes THEN the app SHALL send the new TZ and an incremented `tzUpdatedAt` to the backend on the next foreground; the backend SHALL accept the change only if the incoming `tzUpdatedAt` is greater than the stored one.
6. <a name="4.6"></a>WHEN the app is reinstalled THEN the device's stable identifier SHALL be regenerated (the prior `UserDefaults` container is gone), the backend SHALL treat the device as a new device with no rules, and the prior device's record SHALL be garbage-collected if no successful registration call has occurred from that device for 30 consecutive days.

### 5. Multiple Concurrent Rules

**User Story:** As a user with several rules active in overlapping windows, I want each applicable rule to fire independently, so that I'm not silently missing alerts.

**Acceptance Criteria:**

1. <a name="5.1"></a>WHEN multiple rules on the same device satisfy their conditions in the same poll cycle THEN the backend SHALL fire each rule independently as a separate push.
2. <a name="5.2"></a>The backend SHALL maintain the previous-in-window-SoC and the fire-state independently per (device, rule), so rules do not interfere with each other.
3. <a name="5.3"></a>WHEN a rule is edited or re-enabled THEN the fire-state for that rule's current window-start day SHALL be cleared, so a subsequent in-window crossing under the new configuration fires once; the previous-in-window-SoC SHALL also be cleared so the new configuration re-seeds the comparator on the next reading (no fire on the seed reading, per [3.3](#3.3)).
4. <a name="5.4"></a>WHEN a rule is deleted THEN its (device, rule)-keyed fire-state and previous-SoC SHALL be deleted; a recreated rule SHALL have a fresh `ruleId` and therefore fresh state.

### 6. Non-Functional

**Acceptance Criteria:**

1. <a name="6.1"></a>For the steady-state load of 10 registered devices with 10 enabled rules each (100 evaluations), the added per-cycle work SHALL complete within 50 ms wall-time on the existing Fargate task size (excluding DynamoDB call latency, which is measured separately).
2. <a name="6.2"></a>Per-cycle DynamoDB writes added by this feature SHALL average no more than one write per fired rule per local day per device; the in-window-SoC comparator SHALL be held in poller-process memory (not written every cycle) and only persisted on a controlled cadence sufficient to recover correctness on restart (see [3.3](#3.3)).
3. <a name="6.3"></a>APNs credentials (private key, key ID, team ID, bundle ID) SHALL be stored encrypted at rest, mirroring the existing `/flux/api-token` pattern. The device registration and rules endpoints SHALL use the same bearer-token auth as `/status`, `/history`, and `/day`.
4. <a name="6.4"></a>The backend SHALL emit per-cycle observability events: number of rules evaluated, number fired, number of pushes submitted, number of pushes failed (by HTTP status class), number of skipped-evaluation cycles, and number of devices marked stale. Each fired-rule event SHALL include the stable event identifier `(deviceId, ruleId, windowStartDate)` and the APNs HTTP status.
5. <a name="6.5"></a>WHEN Australia/Sydney or any device's local TZ undergoes a DST transition THEN windows SHALL be interpreted using the platform's wall-clock arithmetic: skipped local times (spring-forward gap) are not in any window; repeated local times (fall-back) fire at most once per rule per window-start day, per [3.6](#3.6).
