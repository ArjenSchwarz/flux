# iOS App Implementation Explanation

## Beginner Level

### What Changed / What This Does
The iOS app is now fully wired to the Flux backend and includes four main screens: Dashboard, History, Day Detail, and Settings. It can show live battery/solar data, historical day totals, and detailed intraday charts, while letting users configure API URL and token.

### Why It Matters
This turns the spec into a working app experience: users can monitor battery behavior, inspect past days, and recover from auth/config issues directly in the app without reinstalling or manual debugging.

### Key Concepts
- **ViewModel (MVVM):** keeps networking/state logic out of SwiftUI views.
- **SwiftData cache:** stores historical day summaries for offline fallback.
- **Keychain:** securely stores the API token.
- **Error states:** unauthorized/config errors now show clear guidance and Settings navigation.

---

## Intermediate Level

### Changes Overview
- Implemented API models and `FluxAPIClient` concrete clients (URLSession + mock).
- Added dashboard auto-refresh flow and stale-data behavior.
- Added history aggregation + chart data generation + SwiftData caching.
- Added day detail charts (SOC/power/battery power) with daily domain helpers.
- Added settings persistence (URL/token/threshold), validation, and dependency reload in app navigation.
- Added targeted pre-push hardening:
  - first-load dashboard error card (retry/settings),
  - history empty-cache error card (retry/settings),
  - day-detail auth/config recovery path (settings CTA),
  - SOC low annotation now includes low time,
  - shared date and error mapping helpers to remove duplication.

### Implementation Approach
Architecture follows `@MainActor @Observable` view models with protocol-based API access and mock injection for previews/tests. Shared date parsing/formatting and error coercion are centralized (`DateFormatting`, `FluxAPIError.from`) to keep behavior consistent across screens.

### Trade-offs
- Chose explicit view-level error cards over global error routing to keep recovery context-specific.
- Kept chart rendering local per feature for readability, accepting some minor repetition.
- SwiftData dedupes by day string and updates existing rows to avoid unique-key collisions during repeated loads.

---

## Expert Level

### Technical Deep Dive
Recent validation-focused updates tighten spec compliance in failure paths:
- Dashboard now distinguishes first-load failures from stale-data refresh failures.
- Unauthorized/not-configured errors expose deterministic recovery (`suggestsSettings`) and user-facing messages (`message`) via `FluxAPIError`.
- History fallback logic now preserves offline behavior while showing actionable error UI when cache is empty.
- Date handling was normalized through `DateFormatting` (single formatter/calendar source), reducing divergence risk across chart domain math and date navigation.
- Cache writes now upsert existing `CachedDayEnergy` models to avoid repeated insert attempts.

### Architecture Impact
These changes reduce duplicated logic and move shared policies into stable primitives, improving long-term maintainability and consistency as additional platforms (iPad/macOS/widgets) are added.

### Potential Issues
- Some lower-priority refactor opportunities remain (chart scaffolding reuse, additional API decoding-focused tests).
- Error copy can be further tuned if product language evolves.

---

## Completeness Assessment

### Fully Implemented
- Dashboard, History, Day Detail, and Settings flows.
- Backend integration + auth token handling + config persistence.
- Offline-ish history fallback using SwiftData cache.
- Core charting and battery status visualizations.
- Main spec-required recovery behaviors for auth/config and inline retry paths.

### Partially Implemented
- A few non-blocking internal simplification opportunities (shared chart container patterns, extra helper consolidation).

### Missing
- No critical or important spec requirements are currently missing after this validation pass.
