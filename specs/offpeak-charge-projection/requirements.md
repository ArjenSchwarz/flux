# Requirements: Off-peak Charge Projection

## Introduction

During the off-peak charging window the battery is charged from the grid, but the Dashboard gives no indication of how full it will be when cheap charging ends. This feature projects the battery's state of charge (SoC) at the off-peak window end, assuming charging continues at maximum capability, and surfaces that single figure on the Dashboard. The projection is computed server-side so every screen shows the same value (T-1533).

## Non-Goals

- Showing the projection outside the off-peak window (no pre-window preview).
- Basing the projection on the live measured charge rate or accounting for charging/round-trip losses — it is an idealised best-case figure.
- Reporting a "battery full by HH:MM" completion time — only the projected SoC at window end is shown.
- Making the charge-curve parameters (rates, threshold) user-configurable.
- Writing or altering any stored/historical data — the projection is a live derived value only.
- Adding the projection to the History or Day Detail screens.

## Requirements

### 1. Projected End-of-Window SoC Computation

**User Story:** As a Flux user, I want the system to project my battery's SoC at the end of the off-peak window, so that I can see how full it will be when cheap charging ends.

**Acceptance Criteria:**

1. <a name="1.1"></a>WHEN the current time is within the off-peak window [start, end) AND live data is fresh, the system SHALL compute a projected SoC for the off-peak window end time.  
2. <a name="1.2"></a>The projection SHALL charge the current SoC at 4.5 kW while SoC is below 95%, and at 500 W while SoC is at or above 95%; the SoC gained over a duration is the charged energy divided by the battery capacity, expressed as a percentage (kW × hours ÷ capacityKwh × 100).  
3. <a name="1.3"></a>The projection SHALL advance the SoC only for the time remaining between the current time and the off-peak window end, applying the 4.5 kW rate until SoC reaches 95% and the 500 W rate thereafter within that remaining time.  
4. <a name="1.4"></a>The projection SHALL use the same battery capacity value and fallback as the cutoff estimate (`EstimatedCutoff`), so the two figures never disagree about capacity.  
5. <a name="1.5"></a>The projected SoC SHALL be clamped to the range [current SoC, 100%].  
6. <a name="1.6"></a>IF the battery would reach 100% before the window end, THEN the projected SoC SHALL be 100% and no earlier-completion time SHALL be reported.  
7. <a name="1.7"></a>WHEN the current SoC is in the range [95%, 100%), the projection SHALL advance only at the 500 W rate.  
8. <a name="1.8"></a>WHEN the current SoC is at or above 100%, the projected SoC SHALL be 100%.  
9. <a name="1.9"></a>Given identical current SoC, capacity, and time remaining, the projected SoC SHALL be the same regardless of the battery's current charge/discharge power (Pbat).  
10. <a name="1.10"></a>The projected SoC SHALL be rounded server-side to one decimal place, consistent with other SoC values in the response.  

### 2. Visibility and Gating

**User Story:** As a Flux user, I want the projection shown only when it is meaningful, so that I am never presented a stale or irrelevant number.

**Acceptance Criteria:**

1. <a name="2.1"></a>WHEN the current time is outside the off-peak window, the system SHALL NOT return a projected SoC.  
2. <a name="2.2"></a>The projection SHALL be returned only on the fresh-live branch — the same gate as `EstimatedCutoff` (at least one reading exists AND the latest reading is within the live-data staleness threshold). When no live data is available, the projection SHALL be absent.  
3. <a name="2.3"></a>WHEN the off-peak window is unconfigured or unparseable, or the battery capacity is non-positive, the system SHALL NOT return a projected SoC.  
4. <a name="2.4"></a>WHEN a load simulation is active (`simulateLoadWatts` > 0), the projected SoC SHALL be unchanged, because the projection is independent of Pbat and simulation models only added load.  

### 3. Single Source of Truth

**User Story:** As a Flux user, I want the projected value computed once on the server, so that every screen that shows it displays the same number.

**Acceptance Criteria:**

1. <a name="3.1"></a>The projected SoC SHALL be computed server-side and returned in the `/status` response.  
2. <a name="3.2"></a>The projected SoC SHALL be returned as a nullable percentage; absence (not computed) SHALL be encoded as an explicit null rather than a defaulted value, so clients can distinguish "no projection" from a real value.  
3. <a name="3.3"></a>Any client that displays the projection SHALL render the value from the `/status` response without re-deriving it locally.  

### 4. Dashboard Display

**User Story:** As a Flux user, I want to see the projected SoC on the Dashboard during the off-peak window, so that I know how full the battery will be when it ends.

**Acceptance Criteria:**

1. <a name="4.1"></a>WHEN the `/status` response includes a projected SoC AND the battery is charging, the Dashboard SHALL display it as a percentage appended to the hero panel's charging subline (e.g. `Charging · 4.50 kW · ~99% by 14:00`). See [Decision 10](decision_log.md).  
2. <a name="4.2"></a>The Dashboard SHALL label the projection using the off-peak window end time from the same `/status` response, not a client-side constant, so the labelled time matches the time the projection targets.  
3. <a name="4.3"></a>WHEN the `/status` response does not include a projected SoC, the Dashboard SHALL NOT display the projection or any placeholder for it.  
4. <a name="4.4"></a>The projection display SHALL render on both iOS and macOS via the shared FluxCore views.  
