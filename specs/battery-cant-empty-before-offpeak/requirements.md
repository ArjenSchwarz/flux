# Requirements: Battery Can't Empty Before Off-Peak

**Transit ticket:** T-1327

## Introduction

When the battery has too much stored energy to physically discharge to the cutoff floor before the next off-peak window starts, the existing `estimatedCutoffTime` is misleading — it extrapolates from live `pbat`, which can be well below the inverter's true ceiling. This feature surfaces an indicator on the Dashboard hero whenever, even at the maximum sustained discharge rate of 5 kW, the battery cannot reach the 5 % cutoff before the next off-peak window opens. The check is `pbat`-independent: it answers "is there any way?" rather than "given current rate, when?".

## Glossary

- **Cutoff SoC**: 5 % (existing `cutoffPercent` constant in `internal/api/status.go`).
- **Max discharge rate**: 5 kW sustained (new Go constant introduced by this feature).
- **Off-peak start (Sydney local)**: the next `offpeak.windowStart` boundary as computed today by `nextOffpeakStart()` in `internal/api/compute.go` (handles same-day or next-day rollover).
- **Live freshness gate**: the existing `liveFresh` check in `handleStatus` (latest reading within `liveDataStalenessThreshold = 90 s`).
- **Capacity (kWh)**: `battery.capacityKwh` as already exposed by `/status`, which falls back to `fallbackCapacityKwh = 13.34` when system metadata is missing.

## Non-Goals

- Configurable discharge ceiling — 5 kW is a baked-in Go constant for this feature.
- Modelling load or solar effects on discharge — the check is a battery-only, ceiling-based physics check.
- Modelling inverter derating at high SoC or high temperature.
- History or Day Detail surfaces — the indicator is Dashboard-only.
- SoC alerts integration / push notifications.
- Showing the indicator while an off-peak window is currently active.
- Supporting off-peak windows that cross midnight — the existing `parseOffpeakWindow` rejects `start ≥ end` and this feature inherits that limit.
- Mirroring the flag on `rolling15min` — it would always equal the hero flag.

## Requirements

### 1. Best-Case Discharge Check

**User Story:** As a Flux user, I want to know when my battery is so full that it cannot reach the cutoff before off-peak starts, so that I can take action (e.g. manual export, higher load) instead of relying on a misleading live cutoff estimate.

**Acceptance Criteria:**

1. <a name="1.1"></a>The system SHALL evaluate, at the moment `/status` is served, whether the battery can reach the cutoff SoC before the next off-peak window opens, assuming a constant max discharge rate from `now`.  
2. <a name="1.2"></a>The check SHALL be `pbat`-independent: it MUST NOT require the battery to be currently discharging.  
3. <a name="1.3"></a>The check SHALL use the same `battery.capacityKwh` value that the `/status` response carries (i.e. system value when present, else the documented fallback).  
4. <a name="1.4"></a>The check SHALL use the off-peak start computed by the existing `nextOffpeakStart()` helper (Sydney local, today or next day as appropriate).  
5. <a name="1.5"></a>The flag SHALL be false WHEN current SoC is at or below the cutoff SoC.  
6. <a name="1.6"></a>The flag SHALL be false WHEN `now` falls inside the configured off-peak window (i.e. the window is currently active).  
7. <a name="1.7"></a>The flag SHALL be false WHEN the live freshness gate fails (`!liveFresh`), since no current SoC is trustworthy.  
8. <a name="1.8"></a>The flag SHALL be false WHEN the off-peak window configuration is missing or invalid (i.e. `nextOffpeakStart` reports no boundary).  

### 2. `/status` API Surface

**User Story:** As a Dashboard view, I want a single boolean from the API saying "battery can't empty before off-peak", so that I can render the indicator without re-implementing the math in Swift.

**Acceptance Criteria:**

1. <a name="2.1"></a>The `/status` response SHALL include a new boolean field on the `battery` object representing the "can't empty before off-peak" condition.  
2. <a name="2.2"></a>The field SHALL be encoded as a nullable Go pointer (no `omitempty`) so that JSON emits `null` when the flag is false and `true` when it holds — matching the existing `estimatedCutoffTime` encoding. `false` SHALL never be emitted.  
3. <a name="2.3"></a>The field name SHALL be stable, snake-free, camelCase, and SHALL NOT collide with `estimatedCutoffTime`, `low24h`, `capacityKwh`, or `cutoffPercent`.  
4. <a name="2.4"></a>Unit tests SHALL cover at least: SoC just above cutoff with a short window-to-open (flag true), SoC well above cutoff with a long window-to-open (flag false), SoC at cutoff (false), SoC below cutoff (false), off-peak active (false), `!liveFresh` (false), missing off-peak config (false), `now == nextOffpeakStart` (false — boundary equality), system-record-missing capacity (uses fallback), and a Sydney DST transition day.  

### 3. Dashboard Hero Indicator

**User Story:** As a Flux user looking at the Dashboard, I want the hero panel to clearly tell me the battery won't drain before off-peak, so that I notice the situation immediately.

**Acceptance Criteria:**

1. <a name="3.1"></a>The Dashboard hero status line SHALL show, at any given time, exactly one of: the existing cutoff-time messaging ("empty by HH:MM") OR the new "won't empty before HH:MM" messaging — never both.  
2. <a name="3.2"></a>The indicator SHALL communicate that the battery cannot reach the cutoff before off-peak begins, and SHALL surface the off-peak start time (HH:MM, Sydney local) using the existing `offpeak.windowStart` value already in `/status`.  
3. <a name="3.3"></a>WHEN the API flag is true, the hero status line SHALL render the new messaging. WHEN the flag is false or absent, the hero SHALL render the existing status line exactly as today.  
4. <a name="3.4"></a>The indicator SHALL render identically on iOS and macOS (the hero panel is shared by both targets).  
5. <a name="3.5"></a>The indicator SHALL be accessible via VoiceOver, exposing the same wording (including the off-peak start time) it shows visually.  
