# Decision Log: iOS App

## Decision 1: Adaptive Layout from the Start

**Date**: 2026-04-15
**Status**: accepted

### Context

The V1 plan targets iPhone only but states that "architectural decisions should not preclude iPad and macOS." The question is whether to build with NavigationStack (simpler, iPhone-only) or NavigationSplitView (adaptive, works across form factors) from the start.

### Decision

Use NavigationSplitView as the root navigation container from day one. On iPhone it collapses to a single-column push-navigation experience; on iPad/macOS it provides sidebar + detail layout without a rewrite.

### Rationale

Retrofitting NavigationSplitView later requires restructuring the entire navigation hierarchy. Starting with it adds minimal complexity on iPhone (it behaves like NavigationStack in compact width) while making iPad/macOS support a matter of enabling the target rather than rearchitecting navigation.

### Alternatives Considered

- **NavigationStack, migrate later**: Simpler iPhone code now — Rejected because migration from NavigationStack to NavigationSplitView is a non-trivial rewrite affecting every navigation link and view hierarchy
- **TabView with per-tab NavigationStack**: Standard iPhone pattern — Rejected because it doesn't scale to sidebar-based iPad layout and the app only has three screens, not enough to justify tabs

### Consequences

**Positive:**
- iPad/macOS support requires no navigation rewrite
- Single navigation model across all platforms
- Minimal overhead on iPhone (collapses to single column)

**Negative:**
- Slightly more complex initial setup than NavigationStack
- Need to handle sidebar content and detail coordination even for V1

---

## Decision 2: No Third-Party Dependencies

**Date**: 2026-04-15
**Status**: accepted

### Context

The app needs HTTP networking, JSON parsing, charting, local persistence, and secure credential storage. Third-party libraries exist for all of these (Alamofire, Charts, Realm, etc.).

### Decision

Use only Apple frameworks: URLSession for networking, Codable for JSON, SwiftUI Charts for charting, SwiftData for persistence, and Keychain Services for credentials. No third-party dependencies.

### Rationale

A two-user personal app does not benefit from the abstraction layers that libraries like Alamofire provide. SwiftUI Charts covers all the chart types needed (area, line, bar). SwiftData handles caching. Keeping zero dependencies simplifies builds, avoids version conflicts, and reduces maintenance burden.

### Alternatives Considered

- **Alamofire for networking**: Richer API, retry policies — Rejected because URLSession with async/await is sufficient for three simple GET endpoints
- **DGCharts (formerly Charts)**: More chart types and customisation — Rejected because SwiftUI Charts covers all required chart types (bar, line, area) and integrates natively with SwiftUI
- **KeychainAccess for Keychain**: Simpler API than raw Security framework — Rejected because the app only stores one credential; a small Keychain wrapper is sufficient

### Consequences

**Positive:**
- Zero dependency management
- No version conflicts or breaking updates
- Smaller app binary
- No supply chain risk

**Negative:**
- More boilerplate for Keychain access compared to a wrapper library
- SwiftUI Charts has fewer customisation options than DGCharts (acceptable for V1)

---

## Decision 3: SwiftData for Caching, Not Network Responses

**Date**: 2026-04-15
**Status**: accepted

### Context

The app needs to cache history data so that navigating back to the History screen doesn't require a network request. Dashboard data should always be fresh. The caching layer could cache raw API responses or domain models.

### Decision

Cache history data (daily energy summaries) in SwiftData as domain models. Dashboard status responses are never cached — always fetched fresh.

### Rationale

Historical days are immutable — once a day is complete, its energy totals never change. Caching these in SwiftData makes History screen loads instant for previously viewed days. Dashboard data changes every 10 seconds, so caching it would add staleness risk with no benefit. Caching domain models rather than raw JSON avoids re-parsing and keeps the cache queryable.

### Alternatives Considered

- **Cache all API responses**: Cache both status and history — Rejected because dashboard data is stale within 10 seconds, making cache invalidation more complex than just fetching fresh
- **URLSession cache (HTTP caching)**: Let URLSession handle caching via HTTP headers — Rejected because the Lambda doesn't set cache headers, and we need fine-grained control (cache old days forever, always refresh today)
- **No caching**: Always fetch from network — Rejected because it creates unnecessary latency when navigating back to History for data that hasn't changed

### Consequences

**Positive:**
- Instant History loads for previously viewed days
- No stale dashboard data
- SwiftData provides queryable, type-safe persistence

**Negative:**
- Need to manage cache invalidation for today's entry (refresh on each visit)

---

## Decision 4: Keychain with App Group for Credentials

**Date**: 2026-04-15
**Status**: accepted

### Context

The API token needs secure storage. Options include UserDefaults (insecure), Keychain (secure), or SwiftData with encryption. A future widget extension will also need to authenticate to the Lambda.

### Decision

Store the API token in the Keychain with App Group access. Store the API URL in UserDefaults (it's not sensitive).

### Rationale

The Keychain is the standard iOS mechanism for credential storage. App Group access ensures a future widget extension can read the token without requiring the user to configure it separately. The API URL is not sensitive and can live in UserDefaults for simplicity.

### Alternatives Considered

- **Both in UserDefaults**: Simplest — Rejected because storing tokens in UserDefaults is insecure; they're readable from device backups
- **Both in Keychain**: Maximum security — Rejected because the API URL is not sensitive and Keychain access is more complex than UserDefaults for a plain string
- **SwiftData with encryption**: Encrypted database — Rejected because SwiftData encryption is not designed for credential storage and adds unnecessary complexity

### Consequences

**Positive:**
- Token is encrypted at rest by the Keychain
- Widget extension can share credentials via App Group
- API URL is simple to read/write from UserDefaults

**Negative:**
- Keychain API requires a small wrapper for ergonomic access
- App Group must be configured in Xcode signing capabilities

---

## Decision 5: 10-Second Auto-Refresh Interval

**Date**: 2026-04-15
**Status**: accepted

### Context

The backend polls AlphaESS every 10 seconds for live data. The app needs to decide how frequently to refresh the dashboard to show current data.

### Decision

Auto-refresh the dashboard every 10 seconds while the app is foregrounded, matching the backend polling interval.

### Rationale

Refreshing faster than 10 seconds wastes requests since the data won't have changed. Refreshing slower means the user sees stale data that the backend already has. 10 seconds matches the data availability exactly.

### Alternatives Considered

- **30-second refresh**: Lower network usage — Rejected because the user would see data up to 30 seconds stale when the backend has fresher data available
- **Push-based updates (WebSocket/SSE)**: Real-time without polling — Rejected because Lambda Function URLs don't support WebSockets, and SSE would require keeping a connection open, adding complexity for minimal benefit over 10-second polling

### Consequences

**Positive:**
- Data freshness matches backend polling interval
- Simple timer-based implementation

**Negative:**
- ~6 requests per minute per foregrounded device (acceptable for a two-user app with a free-tier Lambda)

---

## Decision 6: Spec Scope Excludes Xcode Project Setup

**Date**: 2026-04-15
**Status**: accepted

### Context

The iOS app is greenfield — no Xcode project exists yet. The spec could include Xcode project creation (bundle ID, signing team, capabilities, deployment target) as requirements and tasks, or focus purely on the app code.

### Decision

The spec covers app code only: views, models, networking, caching, and tests. Xcode project setup (bundle ID, signing, capabilities) is assumed to exist before implementation begins.

### Rationale

Xcode project setup is a mechanical task that doesn't benefit from spec-level requirements. The developer will need to configure signing with their own team credentials, which can't be specified in the spec anyway. Keeping the spec focused on app behaviour makes it more useful as a reference.

### Alternatives Considered

- **Include project setup**: Full end-to-end spec from `xcodebuild` to App Store — Rejected because project creation is a one-time mechanical task and signing configuration is developer-specific

### Consequences

**Positive:**
- Spec stays focused on behaviour and architecture
- No wasted spec surface on one-time setup

**Negative:**
- Developer needs to create the Xcode project independently before starting tasks

---

## Decision 7: Sydney Timezone for All Date Operations

**Date**: 2026-04-15
**Status**: accepted

### Context

The backend uses `Australia/Sydney` for all date operations (off-peak windows, day boundaries, "today" determination). The app needs to determine "today" for history caching, off-peak window comparison, and day detail navigation. Using the device's local timezone would cause mismatches when date boundaries differ between the device and backend.

### Decision

Use a shared `DateFormatting.sydneyTimeZone` constant for all date comparisons, "today" determination, and off-peak window checks. Never use `Calendar.current` or device-local timezone for backend-related date logic.

### Rationale

The backend writes date-keyed records (daily energy, off-peak snapshots) using Sydney time. If the app uses a different timezone, "today" could refer to a different date than the backend's "today", causing cache mismatches, incorrect bar fading, and wrong off-peak window comparisons. For a two-user app in Australia this is a non-issue in practice, but correctness matters.

### Alternatives Considered

- **Device timezone**: Simpler, standard iOS pattern — Rejected because it causes date boundary mismatches with the backend
- **Server-provided timezone**: Backend returns its timezone in API responses — Rejected as unnecessary complexity; the timezone is a deployment constant

### Consequences

**Positive:**
- Date boundaries always match between app and backend
- Off-peak window comparisons are correct
- History caching uses consistent date keys

**Negative:**
- Hardcoded timezone — if the system moves to a different timezone, both backend and app need updating
- Clock time formatting shows Sydney time, which is correct for the battery location but may confuse a user in a different timezone

---

## Decision 8: Token Provider Pattern for Settings Validation

**Date**: 2026-04-15
**Status**: accepted

### Context

The Settings screen needs to validate a new API token by calling `/status` before saving it to the Keychain. The `URLSessionAPIClient` reads the token from `KeychainService` on each request. During initial setup or token change, the new token isn't in the Keychain yet — validating with the old (or missing) token would always fail.

### Decision

Use a `tokenProvider` closure in `URLSessionAPIClient` instead of directly referencing `KeychainService`. The production initializer provides a closure that reads from Keychain. A second initializer accepts an explicit token string for validation.

### Rationale

This avoids the chicken-and-egg problem (can't validate a token that isn't stored, can't store a token that isn't validated) without compromising the normal request flow. The alternative of saving first and deleting on failure risks poisoning the Keychain with an invalid token, breaking the app for both users.

### Alternatives Considered

- **Save to Keychain first, delete on failure**: Simpler but risks leaving an invalid token if the app crashes during validation
- **Separate `validate(url:token:)` method**: Works but duplicates request-building logic from `performRequest`

### Consequences

**Positive:**
- Clean separation — validation doesn't touch persisted state
- No risk of invalid tokens in Keychain
- Same request-building and error-handling code path for both validation and normal use

**Negative:**
- Slightly more complex `URLSessionAPIClient` initializer

---

## Decision 9: Fallback Data Detection via Heuristic

**Date**: 2026-04-15
**Status**: accepted

### Context

The `/day` endpoint falls back to `flux-daily-power` data when `flux-readings` has no data for the requested date. Fallback data has SOC values but all power fields (`ppv`, `pload`, `pbat`, `pgrid`) set to 0. The app needs to distinguish this from real readings to hide the power charts (requirement 10.8). The backend doesn't include a flag indicating which data source was used.

### Decision

Detect fallback data by checking if all readings have `ppv == 0 && pload == 0 && pbat == 0 && pgrid == 0`. When this condition is true for all readings, set `hasPowerData = false` and render only the SOC chart.

### Rationale

While a backend flag would be more reliable, this heuristic is safe in practice: it's impossible for a real day to have zero power on all four metrics across every 5-minute reading. Solar is zero at night, but load is never zero for a running household. The heuristic avoids a backend change for V1.

### Alternatives Considered

- **Backend flag (`dataSource: "readings" | "fallback"`)**: Explicit and reliable — Deferred to V2 to avoid a backend change for V1. Can be added as a non-breaking addition to the `/day` response.
- **Check reading count**: Fallback data has ~288 readings at 5-min intervals vs ~288 downsampled readings — indistinguishable by count alone

### Consequences

**Positive:**
- No backend change required
- Simple implementation
- Safe heuristic for real-world data

**Negative:**
- Theoretically fragile — a day with genuine zero power everywhere would be misclassified (practically impossible)
- Should be replaced with a backend flag in V2

---
