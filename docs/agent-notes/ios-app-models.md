# iOS App — Models, Services & Helpers

## Project Structure

Xcode project at `Flux/` with app source in `Flux/Flux/` and tests in `Flux/FluxTests/`. Uses `PBXFileSystemSynchronizedRootGroup` for automatic file pickup (new files are discovered without manual Xcode configuration).

**Build settings:** `SWIFT_DEFAULT_ACTOR_ISOLATION = MainActor`, `SWIFT_APPROACHABLE_CONCURRENCY = YES`, deployment target iOS 26.0. Simulator uses iPhone 17 Pro.

**Build & test:**
```bash
xcodebuild build -scheme Flux -destination 'platform=iOS Simulator,name=iPhone 17 Pro' -quiet
xcodebuild test -scheme Flux -destination 'platform=iOS Simulator,name=iPhone 17 Pro' -quiet
```

## Models (Flux/Flux/Models/)

- **APIModels.swift** — Codable structs matching backend JSON. All `Codable + Sendable`. `DayEnergy` and `TimeSeriesPoint` conform to `Identifiable`. Key types: `StatusResponse`, `LiveData`, `BatteryInfo`, `RollingAvg`, `OffpeakData`, `TodayEnergy`, `HistoryResponse`, `DayDetailResponse`, `DaySummary`.
- **FluxAPIError.swift** — Error enum with 7 cases plus three extensions: `.from(_ error:)` for consistent coercion, `.message` for user-facing strings, `.suggestsSettings` flag for recovery routing. This centralises error interpretation so views don't duplicate switch statements.
- **CachedDayEnergy.swift** — SwiftData `@Model` with unique date attribute. Bidirectional conversion via `init(from: DayEnergy)` and `.asDayEnergy` property. Used for offline history fallback.

## Services (Flux/Flux/Services/)

- **FluxAPIClient.swift** — Protocol with `Sendable` conformance. Three async throws methods: `fetchStatus()`, `fetchHistory(days:)`, `fetchDay(date:)`.
- **URLSessionAPIClient.swift** — Concrete client using `URLComponents` with separate `queryItems` array (prevents query encoding bugs). Has `decodeResponse` and `parseErrorMessage` helpers. Accepts either `KeychainService` or hardcoded token.
- **KeychainService.swift** — Uses `[CFString: Any]` dictionary keys (idiomatic Security framework). Service: `"me.nore.ig.flux"`, account: `"api-token"`, App Group: `"group.me.nore.ig.flux"`.
- **MockFluxAPIClient.swift** — `#if DEBUG` guarded actor with static preview data. Used for SwiftUI previews. Test files create their own focused mocks instead.

## FluxCore Helpers (Flux/Packages/FluxCore/Sources/FluxCore/Helpers/)

- **NoteText.swift** — Cross-stack grapheme handling for the day-notes feature. `maxGraphemes = 200`, `normalised(_:)` (NFC + leading/trailing whitespace trim), `graphemeCount(_:)`. Backend Go counterpart in `internal/api/notetext.go` must agree on every entry in `internal/api/testdata/note_lengths.json`; both client (`Flux/FluxTests/NoteTextTests.swift`) and server tests load that fixture from repo root.

## Helpers (Flux/Flux/Helpers/)

- **DateFormatting.swift** — All formatters are `static let` (created once, reused). Two ISO formatters (with/without fractional seconds) with fallback parsing. Sydney timezone throughout. Key methods: `parseTimestamp`, `clockTime`, `todayDateString`, `parseDayDate`, `parseWindowTime`, `isInOffpeakWindow`, `isToday`.
- **BatteryColor.swift** — Defines `ColorTier` enum (green/red/orange/amber/normal) with `.color` computed property. `BatteryColor.forSOC` returns `ColorTier`. This pattern enables unit testing without SwiftUI Color comparisons.
- **GridColor.swift** — `forGrid()` returns `ColorTier`. Red when pgrid > 500 AND sustained AND outside off-peak.
- **CutoffTimeColor.swift** — `forCutoff()` returns `ColorTier`. Red when < 2h to cutoff, orange when cutoff before window start.

## Concurrency Notes

All types default to `@MainActor` via build setting. Service types are explicitly `nonisolated` or actors. All models marked `nonisolated` and `Sendable`. `MockFluxAPIClient` is an `actor` for thread-safe access.

## Testing

Swift Testing framework (`@Test`, `#expect`). `MockURLProtocol` with `NSLock`-protected static vars for thread-safe HTTP interception. UUID-based Keychain isolation. Tests compare `ColorTier` enum values directly (not `SwiftUI.Color`).
