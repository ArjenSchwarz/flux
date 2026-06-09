# Requirements: Dashboard Simulation

## Introduction

Dashboard Simulation lets a user toggle a named, predetermined load (in watts) on the Dashboard to see — as a clearly-labelled what-if — how their power flow and battery life would change. For example, activating a "Charge car" preset of 1700W shows house load 1.7kW higher and a correspondingly earlier "empty by" time, without changing any real device. The simulated state is produced by the same backend that serves the real status, so the simulated and real values never use divergent logic. Presets (a label paired with a watt value) are managed at the user level, sync across the user's devices, and the feature is available on both iOS and macOS.

## Non-Goals

- Controlling or sending commands to the real battery, inverter, or any AlphaESS device — the feature is read-only and computes a hypothetical only.
- Simulating a *reduction* in load (turning appliances off); only added load is supported.
- Running more than one preset at the same time (no stacking/summing of presets).
- Simulating changes to solar generation, grid tariff, or off-peak windows.
- Changing the History or Day Detail screens; simulation affects the Dashboard only.
- Persisting an active simulation across an app restart; it is a transient session state.
- Reflecting simulated values in widgets or the Control Center widget; those always show real data.
- Producing a simulated estimate while offline or while live data is stale; simulation requires a fresh server response.

## Requirements

### 1. Manage Simulation Presets

**User Story:** As a user, I want to create, edit, and delete named load presets, so that I can reuse what-if scenarios like charging the car.

**Acceptance Criteria:**

1. <a name="1.1"></a>The system SHALL allow a user to create a simulation preset consisting of a text label and a load value in watts.  
2. <a name="1.2"></a>The system SHALL allow a user to edit the label and watt value of an existing preset, and to delete a preset.  
3. <a name="1.3"></a>The system SHALL reject a preset whose label is empty, whose watt value is not greater than zero, or whose watt value exceeds 20000 (20 kW), and SHALL inform the user why it was rejected.  
4. <a name="1.4"></a>The system SHALL persist presets server-side so that the same presets are available after reinstalling the app and on the user's other devices.  
5. <a name="1.5"></a>The system SHALL present the list of presets within the Settings screen for management on both iOS and macOS.  
6. <a name="1.6"></a>WHEN a create, edit, or delete operation fails or the device is offline, the system SHALL apply the change only after the server confirms it, leave the displayed list unchanged on failure, and surface a visible error, matching the existing SoC Alerts behaviour.  

### 2. Activate a Simulation From the Dashboard

**User Story:** As a user, I want to switch a simulation on or off from the Dashboard, so that I can quickly see a what-if without leaving the main screen.

**Acceptance Criteria:**

1. <a name="2.1"></a>The Dashboard SHALL provide a control to activate one preset or to turn simulation off, on both iOS and macOS.  
2. <a name="2.2"></a>WHEN a preset is activated while another preset is active, the system SHALL replace the active preset so that at most one simulation is active at any time.  
3. <a name="2.3"></a>IF no presets are configured THEN the Dashboard control SHALL offer a path to create one rather than presenting an empty selection.  
4. <a name="2.4"></a>WHEN the currently active preset is deleted — whether deleted on this device or removed via sync from another device — the system SHALL turn simulation off.  
5. <a name="2.5"></a>The active simulation SHALL remain active across the Dashboard's auto-refresh and across navigation between tabs within a session, and SHALL reset to off on a cold app launch.  
6. <a name="2.6"></a>WHILE a simulation is active, each periodic refresh SHALL issue exactly one status request, carrying the active preset's watt value.  
7. <a name="2.7"></a>The watt value simulated SHALL match the active preset's current stored value; WHEN that preset's watts change via sync while it is active, the Dashboard SHALL re-request the simulated status with the updated value so the indicator and the figures never reflect different watts.  

### 3. Simulated Power Flow

**User Story:** As a user, I want the Dashboard's live values to reflect the added load consistently, so that the simulated picture is believable and internally balanced.

**Acceptance Criteria:**

1. <a name="3.1"></a>WHEN a preset of W watts is active, the backend SHALL return a status whose house load is the actual load plus W.  
2. <a name="3.2"></a>WHEN a preset of W watts is active, the backend SHALL allocate the added load in priority order — first reducing any grid export toward zero, then increasing battery discharge (reducing charge) up to the battery's maximum sustained discharge rate — so that solar surplus currently being exported is consumed before the battery is drawn down, and the displayed load, battery, and grid remain energy-balanced.  
3. <a name="3.3"></a>WHEN no simulation is active, the Dashboard SHALL display the actual server-provided values unchanged.  
4. <a name="3.4"></a>WHEN the added load exceeds what reducing export and discharging the battery at its maximum sustained rate can supply, the backend SHALL meet the remainder with grid import. The simulated battery discharge SHALL never be shown faster than that maximum rate nor slower than its actual rate (adding load never reduces the displayed discharge).  

### 4. Simulated Battery Life

**User Story:** As a user, I want the "empty by" estimate to update under simulation, so that I can judge how much sooner the battery would run down.

**Acceptance Criteria:**

1. <a name="4.1"></a>WHEN a simulation of W watts is active, the "empty by" estimate SHALL be derived from the battery value *after* the load is allocated per [3.2](#3.2)/[3.4](#3.4) — i.e. after any export is reduced and capped at the battery's maximum sustained discharge rate — and the live discharge shown in the power panel SHALL reflect the same allocated value, so the displayed discharge and the "empty by" are one consistent figure that is never sooner than the battery's real discharge limit allows, computed with the same handler policy (freshness gating, off-peak suppression) as the real estimate.  
2. <a name="4.2"></a>WHEN the added load is zero, the simulated status SHALL equal the non-simulated status computed from the same reading data and time, so the two never diverge.  
3. <a name="4.3"></a>WHEN a simulation is active, the off-peak ("won't empty before off-peak") indicator and the simulated "empty by" SHALL be consistent with each other: the Dashboard SHALL NOT show the real-data off-peak indicator alongside a simulated "empty by", and any indicator shown SHALL be derived from the same simulated state.  
4. <a name="4.4"></a>IF the simulated battery power is not a net discharge, or the state of charge is already at or below the cutoff, THEN no "empty by" estimate SHALL be shown, matching how the real estimate behaves in those conditions.  
5. <a name="4.5"></a>IF the underlying live data is stale (older than the server's freshness threshold) or the simulated-status request fails, THEN the Dashboard SHALL NOT display fabricated simulated values and SHALL indicate the data is unavailable, as it does for the non-simulated stale case.  
6. <a name="4.6"></a>IF a status request carries a load parameter that is unparseable or outside the accepted range (greater than zero and at most 20000), THEN the backend SHALL reject it with a reason rather than return a status, and the Dashboard SHALL treat this as simulation-unavailable (per [4.5](#4.5)) rather than render fabricated values.  

### 5. Clear Simulation Labelling

**User Story:** As a user, I want it to be obvious when I'm looking at a simulation, so that I never mistake a what-if for the real state.

**Acceptance Criteria:**

1. <a name="5.1"></a>WHILE a simulation is active, the Dashboard SHALL display a persistent indicator identifying that values are simulated and naming the active preset.  
2. <a name="5.2"></a>The simulation indicator SHALL be visually distinct from the existing hero status indicators (such as the "won't empty before off-peak" line) so it is not mistaken for a real-data status, while still fitting the Dashboard's visual language.  
3. <a name="5.3"></a>WHILE a simulation is active, the values changed by the simulation SHALL carry a visible marking distinguishing them from real values, so a glance at the figures alone reveals the simulation.  
4. <a name="5.4"></a>The simulation indicator and the simulated values SHALL be announced as simulated to assistive technologies, matching the accessibility treatment of the existing hero status indicator.  
5. <a name="5.5"></a>WHEN simulation is turned off, the indicator and value markings SHALL be removed and all displayed values SHALL return to their actual values.  
