# Decision Log: Real-Time Today Energy

## Decision 1: Compute Energy from Existing Readings

**Date**: 2026-04-16
**Status**: accepted

### Context

The dashboard's "Today" energy card shows values from the AlphaESS `GetOneDateEnergy` API, polled every 6 hours. This makes the data stale for a real-time monitoring dashboard. The Lambda already fetches all 24h of readings (power samples every 10 seconds) for other computations.

### Decision

Compute today's energy totals by integrating power readings already in memory, rather than relying solely on the AlphaESS energy API.

### Rationale

The readings data is already fetched and in memory for every `/status` call. Adding an integration loop is computationally trivial compared to the DynamoDB query that fetched the data. This gives near-real-time energy totals (10 seconds stale at most) with zero additional API calls or DynamoDB reads.

### Alternatives Considered

- **Poll AlphaESS energy API more frequently (e.g. 5 minutes)**: Adds API calls and still has minutes of staleness — rejected because readings-based computation is free and fresher
- **Store running totals in the poller**: Adds state management complexity and a new DynamoDB write path — rejected as overengineered for a single-user system

### Consequences

**Positive:**
- Energy data refreshes every 10 seconds instead of every 6 hours
- No additional DynamoDB queries or AlphaESS API calls
- Simple implementation (~40 lines of compute logic)

**Negative:**
- Computed values are approximations (trapezoidal integration of discrete samples)
- Small divergence from AlphaESS's internal metering is expected

---

## Decision 2: Per-Field Max for Reconciliation

**Date**: 2026-04-16
**Status**: accepted

### Context

With two data sources (computed from readings, stored from AlphaESS API), we need a strategy to combine them when both are available.

### Decision

Use per-field `max()` — for each energy field, take the higher value from either source.

### Rationale

Energy totals are cumulative and monotonically increasing over a day. A lower value from either source means it hasn't caught up yet, not that it's more accurate. Taking the max gives the best-available value at any point in time.

### Alternatives Considered

- **Always prefer computed**: Ignores AlphaESS's more accurate metering — rejected
- **Always prefer AlphaESS**: Defeats the purpose of real-time computation — rejected
- **Weighted average**: Unnecessary complexity for negligible accuracy gain — rejected

### Consequences

**Positive:**
- Always shows the most up-to-date value for each field
- Simple to implement and reason about
- Self-correcting as both sources converge

**Negative:**
- Could briefly show a value that's slightly higher than either source alone (if they diverge on different fields)

---

## Decision 3: Clamped Directional Integration

**Date**: 2026-04-16
**Status**: accepted

### Context

Power readings for grid and battery can change sign (import/export, charge/discharge). When integrating pairs of readings where one is positive and the other negative, the zero crossing should ideally be handled precisely.

### Decision

Clamp each reading's value to the relevant direction before integrating (e.g. `max(pgrid, 0)` for import). Don't attempt to find exact zero-crossing points.

### Rationale

At 10-second sample intervals, zero crossings are brief transients. The error from clamping vs precise zero-crossing interpolation is negligible. This approach is simpler and matches the existing code style of straightforward per-reading operations.

### Alternatives Considered

- **Proportional zero-crossing split**: Interpolate the exact crossing time and split the trapezoid — rejected as overengineered for 10-second intervals
- **Use only the dominant direction per pair**: Loses energy during transitions — rejected

### Consequences

**Positive:**
- Simple implementation (one `max()` call per field per reading)
- Negligible accuracy impact at 10-second intervals

**Negative:**
- Slightly underestimates energy during sign transitions (loses the small triangle between zero and the crossing point)

---

## Decision 4: Skip Gaps Over 60 Seconds

**Date**: 2026-04-16
**Status**: accepted

### Context

Readings normally arrive every 10 seconds, but gaps can occur if the poller or AlphaESS API is temporarily unavailable.

### Decision

Skip pairs where the time gap exceeds 60 seconds. Don't interpolate across gaps.

### Rationale

A gap likely means the system was offline or the poller was restarting. Interpolating across a gap would assume steady-state power during an unknown period, potentially adding significant phantom energy. Skipping the gap is conservative and honest.

### Alternatives Considered

- **Interpolate across all gaps**: Could add phantom energy during outages — rejected
- **Use a larger threshold (e.g. 5 minutes)**: 60 seconds is already 6x the normal interval, generous enough for minor delays — rejected as unnecessary

### Consequences

**Positive:**
- Conservative — never adds phantom energy
- Simple gap detection (one comparison)

**Negative:**
- Slightly underestimates total energy during gaps (missing data periods)
