# Flux — V1 Plan

## Overview

Native iOS app (Swift/SwiftUI, iOS 26+) providing a focused, fast view of AlphaESS battery system status. Replaces the official AlphaESS app for day-to-day monitoring. Designed with future iPad, macOS, and widget support in mind.

Three screens: Dashboard (default), History (with day detail drill-down), and Settings.

-----

## Design

The app uses iOS 26 Liquid Glass styling throughout. All UI elements — navigation bars, toolbars, tab bars, segmented controls, grouped lists — should use the native Liquid Glass material and follow iOS 26 conventions for layout, spacing, and interaction patterns.

The design prioritises readability over visual flair. The primary goal is that all important data is immediately scannable without interpretation. Conditional colouring is used sparingly and only where it communicates meaningful state changes.

### Dashboard Layout

Hybrid layout combining a centred battery hero section, a three-column power reading trio (solar/load/grid), and a vertical list for secondary stats and today’s energy summary.

### History Layout

Grouped vertical bar chart (5 bars per day) with a tappable day detail. Segmented control for 7/14/30 day ranges.

### Day Detail Layout

Three stacked time-series charts (SOC, power flows, battery power) with a day summary card at the bottom. Day-to-day navigation via left/right arrows.

### Charts

Use SwiftUI Charts for all graphs. No third-party charting libraries.

-----

## Architecture

The app does not talk to the AlphaESS API directly. Instead, a lightweight backend polls the AlphaESS API on a continuous schedule, stores all data in DynamoDB, and serves pre-computed stats to the app via a Lambda Function URL.

This approach enables features that would otherwise require client-side polling and state accumulation (rolling averages, historical battery low, off-peak statistics), keeps AlphaESS API credentials server-side only, and ensures all devices see identical data without sync.

### Components

**VPC**

- Dedicated VPC with public subnets across 2 AZs for Fargate failover
- No private subnets needed — all workloads run in public subnets with security groups restricting inbound traffic
- DynamoDB Gateway VPC endpoint to keep DynamoDB traffic off the public internet (free)
- Internet Gateway for outbound IPv4 access to AlphaESS API and GHCR

**ECS Fargate Task** (0.25 vCPU, 0.5 GB ARM/Graviton)

- Long-running container polling the AlphaESS API on multiple schedules
- Writes all data to DynamoDB via Gateway VPC endpoint
- Single task, always running, can failover across 2 AZs
- Runs in a public subnet with `assignPublicIp: ENABLED` — the AlphaESS API is IPv4-only so internet access is required. Security group allows outbound only; the container has no listening ports, so the public IP has zero inbound attack surface.

**DynamoDB**

- Stores real-time readings, daily energy summaries, daily power snapshots, off-peak energy snapshots, and system info
- Partition key on serial number, sort key on timestamp for time-series queries
- Future consideration: implement a data compaction strategy for `flux-readings` — 10-second snapshots are useful for recent data but a year of readings is ~3.1 million rows. Could downsample older data to 5-minute or hourly averages while preserving daily summaries indefinitely.

**Lambda Function URL**

- Serves the app API — reads from DynamoDB, computes derived stats, returns a flat JSON response
- No API Gateway needed; Function URL is free and sufficient for a two-user app
- Separated from the poller so the API remains available even if the Fargate task restarts

### Estimated Cost

All covered by existing AWS credits. Without credits:

- Fargate (0.25 vCPU, 0.5 GB, ARM, 24/7): ~$7.11/month
- Public IPv4 address: ~$3.60/month
- DynamoDB (on-demand, ~260K writes/month): ~$0.32/month
- Lambda Function URL: free tier
- Total: ~$11.03/month

-----

## AlphaESS API

Authentication uses three custom headers on every request:

- `appId` — the developer App ID
- `timeStamp` — current Unix timestamp (string)
- `sign` — SHA512 hex digest of `appId + appSecret + timestamp`

The server rejects requests where the timestamp drifts more than 300 seconds from server time. AlphaESS recommends minimum 10-second intervals between polls.

### Endpoints and Polling Schedule

|Endpoint              |Interval                  |Params              |Data Retrieved                                                                                                                                                                                                                                                                         |
|----------------------|--------------------------|--------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
|`getLastPowerData`    |10 seconds                |`sysSn`             |Real-time: solar (ppv), battery (pbat), grid (pgrid), load (pload), state of charge (soc), per-phase detail                                                                                                                                                                            |
|`getOneDayPowerBySn`  |~1 hour                   |`sysSn`, `queryDate`|5-minute battery state snapshots (cbat) for the current day. Power fields return 0 — only cbat is usable. Used for 24h battery low and off-peak battery delta calculations                                                                                                             |
|`getOneDateEnergyBySn`|~6 hours + off-peak window|`sysSn`, `queryDate`|Daily energy totals: solar (epv), grid import (eInput), grid export (eOutput), battery charge (eCharge), battery discharge (eDischarge), grid charge (eGridCharge). Also called at the start and end of the off-peak window to compute off-peak deltas (see Off-Peak Calculation below)|
|`getEssList`          |~24 hours                 |—                   |System info: serial number (sysSn), battery capacity (cobat: 13.34 kWh), model, inverter capacity                                                                                                                                                                                      |

### Off-Peak Calculation

The container is configured with the off-peak window (11:00 AM – 2:00 PM) via SSM Parameter Store. It calls `getOneDateEnergyBySn` at the start and end of the off-peak window and stores both snapshots. The diff between the two gives exact off-peak values for every energy metric:

- Off-peak grid usage = `eInput(end) - eInput(start)`
- Off-peak solar = `epv(end) - epv(start)`
- Off-peak battery charge = `eCharge(end) - eCharge(start)`
- Off-peak battery discharge = `eDischarge(end) - eDischarge(start)`
- Off-peak grid export = `eOutput(end) - eOutput(start)`

These computed values are stored in a DynamoDB record and served directly via the `/status` endpoint. No client-side calculation needed.

### DynamoDB Table Design

**Table: `flux-readings`**

- PK: `sysSn` (string)
- SK: `timestamp` (number, Unix epoch)
- Attributes: all fields from `getLastPowerData` (ppv, pbat, pgrid, pload, soc, etc.)
- TTL: 30 days (raw 10-second data is only needed for rolling averages and day detail charts; older data can be pruned)

**Table: `flux-daily-energy`**

- PK: `sysSn` (string)
- SK: `date` (string, YYYY-MM-DD)
- Attributes: all fields from `getOneDateEnergyBySn` (epv, eInput, eOutput, eCharge, eDischarge, eGridCharge)
- No TTL (historical data is small and permanently useful)

**Table: `flux-daily-power`**

- PK: `sysSn` (string)
- SK: `uploadTime` (string, from API response)
- Attributes: cbat, ppv, load, feedIn, gridCharge (as returned by API, though ppv and load are 0)
- TTL: 7 days (only needed for recent 24h low and off-peak calculations; older data superseded by daily energy summaries)

**Table: `flux-system`**

- PK: `sysSn` (string)
- Attributes: cobat, mbat, minv, popv, poinv, emsStatus, lastUpdated

**Table: `flux-offpeak`**

- PK: `sysSn` (string)
- SK: `date` (string, YYYY-MM-DD)
- Attributes: startSnapshot (all energy fields at off-peak start), endSnapshot (all energy fields at off-peak end), computed deltas (gridUsageKwh, solarKwh, batteryChargeKwh, batteryDischargeKwh, gridExportKwh, batteryDeltaPercent)
- No TTL (small records, useful for historical off-peak reporting)

### Lambda Function URL — App API

The app makes API calls to get data for the dashboard, history, and day detail. The Lambda reads from DynamoDB (including pre-computed off-peak deltas) and returns stats.

**`GET /status`**

The serial number is configured server-side (SSM Parameter Store) — the app doesn’t need to specify it since there’s only one system.

Returns:

```json
{
  "live": {
    "ppv": 0.0,
    "pload": 207.0,
    "pbat": 216.0,
    "pgrid": -9.0,
    "pgridSustained": false,
    "soc": 41.2,
    "timestamp": "2026-04-11T21:47:00Z"
  },
  "battery": {
    "capacityKwh": 13.34,
    "cutoffPercent": 10,
    "estimatedCutoffTime": "2026-04-12T04:03:00Z",
    "low24h": {
      "soc": 38.2,
      "timestamp": "2026-04-11T18:45:00Z"
    }
  },
  "rolling15min": {
    "avgLoad": 243.0,
    "avgPbat": 220.0,
    "estimatedCutoffTime": "2026-04-12T03:41:00Z"
  },
  "offpeak": {
    "windowStart": "11:00",
    "windowEnd": "14:00",
    "gridUsageKwh": 6.1,
    "solarKwh": 0.0,
    "batteryChargeKwh": 5.4,
    "batteryDischargeKwh": 0.1,
    "gridExportKwh": 0.0,
    "batteryDeltaPercent": 42.3
  },
  "todayEnergy": {
    "epv": 14.3,
    "eInput": 0.25,
    "eOutput": 5.94,
    "eCharge": 5.7,
    "eDischarge": 6.8
  }
}
```

**`GET /history?days=7`**

Returns daily energy summaries for the bar chart:

```json
{
  "days": [
    {
      "date": "2026-04-10",
      "epv": 14.3,
      "eInput": 0.25,
      "eOutput": 5.94,
      "eCharge": 5.7,
      "eDischarge": 6.8
    }
  ]
}
```

**`GET /day?date=2026-04-10`**

Returns time-series readings for the day detail charts. Uses data from `flux-readings` (10-second granularity). For days before the backend was running, falls back to `flux-daily-power` (5-minute cbat only — power charts will be empty).

```json
{
  "date": "2026-04-10",
  "readings": [
    {
      "timestamp": "2026-04-10T00:00:10Z",
      "ppv": 0.0,
      "pload": 185.0,
      "pbat": 190.0,
      "pgrid": -5.0,
      "soc": 62.4
    }
  ],
  "summary": {
    "epv": 14.3,
    "eInput": 0.25,
    "eOutput": 5.94,
    "eCharge": 5.7,
    "eDischarge": 6.8,
    "socLow": 38.2,
    "socLowTime": "2026-04-10T18:45:00Z"
  }
}
```

### App Authentication

The app does not need AlphaESS credentials. It authenticates to the Lambda Function URL with a simple shared secret in a header (e.g. `Authorization: Bearer {token}`). Sufficient for a two-user personal app. The token is configured in the app’s Settings screen.

-----

## Screen 1: Dashboard

The default screen. Displays data from the `/status` endpoint.

### Data Displayed

**Primary section — battery state (centred hero):**

- **Battery percentage** — `live.soc`, colour-coded: green >60%, default 30-60%, amber 15-30%, red <15%
- **Status line** — contextual: “Discharging · cutoff ~4:03 AM” or “Charging · full ~2:45 PM” or “Idle”
- **Progress bar** — matches battery percentage colour

**Power readings (three-column row):**

- **Solar** — `live.ppv` in watts. Green when generating, muted when 0
- **Household load** — `live.pload` in watts. Red above configurable threshold (e.g. 3000W)
- **Grid** — `live.pgrid` in watts, with direction label (exporting/importing)

**Secondary stats (vertical list):**

- **24h low** — `battery.low24h.soc` with timestamp
- **Off-peak grid usage** — `offpeak.gridUsageKwh` (exact grid import during off-peak window, computed from energy snapshot diffs)
- **Off-peak battery delta** — `offpeak.batteryDeltaPercent` (battery % gained during off-peak window)
- **15-min average load** — `rolling15min.avgLoad` with its own cutoff estimate

**Today’s energy summary (vertical list, section header “Today”):**

- Solar generated — `todayEnergy.epv` in kWh
- Grid imported — `todayEnergy.eInput` in kWh
- Grid exported — `todayEnergy.eOutput` in kWh
- Battery charged — `todayEnergy.eCharge` in kWh
- Battery discharged — `todayEnergy.eDischarge` in kWh

The today section uses default text colour only — no conditional colouring on these values.

**Estimated cutoff times** are displayed as clock times (e.g. “~4:03 AM”), not countdowns. The 10% cutoff is the actual threshold where the AlphaESS battery stops discharging.

### Conditional Colouring

- **Battery %**: green >60%, default 30-60%, amber 15-30%, red <15%. Progress bar colour matches.
- **Grid**: green when exporting (pgrid < 0). Red only when importing above 500W during peak hours AND sustained (i.e. not a brief blip from battery settling — the backend flags this via `pgridSustained` by checking if grid import has been >500W for multiple consecutive readings). Default otherwise, including off-peak import.
- **Load**: red above configurable threshold, default otherwise
- **Solar**: green when generating, muted/tertiary when 0
- **Cutoff time**: red when less than 2 hours away. Amber when cutoff is estimated to occur before the off-peak window starts (i.e. battery will hit 10% before 11:00 AM, meaning no free charging will rescue it).

### Refresh Behaviour

- Fetch `/status` on app launch / foreground
- Pull-to-refresh
- Auto-refresh every 10 seconds while foregrounded
- The backend handles all AlphaESS polling; the app just reads from the Lambda

### Edge Cases

- Single-phase system — no per-phase breakdown needed in V1
- Battery at 100% and charging: show “Full” instead of cutoff estimate
- Backend unreachable: show last known data with a staleness indicator and timestamp
- No configured API token: redirect to Settings

**Navigation:** “View history →” link at the bottom navigates to the History screen.

-----

## Screen 2: History

Accessed via navigation from Dashboard. Shows daily energy totals.

### Data Source

`/history` endpoint, which reads from the `flux-daily-energy` DynamoDB table.

### Display

Grouped vertical bar chart showing the last 7 days (default), with a segmented control to switch to 14 or 30 days.

Each day shows 5 side-by-side bars:

- Solar generated (kWh) — green
- Grid imported (kWh) — red
- Grid exported (kWh) — blue
- Battery charged (kWh) — amber
- Battery discharged (kWh) — purple

Today’s bars are slightly faded to indicate partial data. The selected day has a subtle background highlight behind its bar group.

Tapping a day updates the summary card below the chart with that day’s exact numbers. A “View day detail →” link navigates to the day detail screen.

### Day Detail (Drill-Down)

Accessed by tapping “View day detail” from the history screen. Shows three stacked time-series charts for the selected day, using data from the `/day` endpoint.

**Day navigation**: left/right arrows to move between days, with the current day name and date displayed between them.

**Chart 1 — Battery SOC:**

- Filled area chart showing state of charge (%) throughout the day
- Y-axis: 0–100%
- 10% cutoff threshold shown as a dashed line
- 24h low point annotated with a dot and label showing the percentage and time

**Chart 2 — Power:**

- Multi-line chart showing solar (green filled area), load (dark line), grid import (red line), and grid export (blue line)
- Y-axis: watts/kilowatts
- Solar shown as a filled area to emphasise it as the primary generation source

**Chart 3 — Battery power:**

- Single line chart showing battery charge (positive, above zero line) and discharge (negative, below zero line)
- Zero line clearly marked
- Y-axis: +/- kilowatts

**Day summary card** at the bottom showing the same kWh totals as the history view, plus the SOC low point.

Note: day detail charts use data from `flux-readings` (10-second snapshots). For days before the backend started running, only the SOC chart will have data (from `flux-daily-power` at 5-minute intervals). The power charts will be empty for pre-backend days.

All time axes show 00:00 through 00:00 with intermediate labels.

### Caching

- The app caches history responses in SwiftData
- Historical days (before today) are immutable — cache indefinitely
- Today’s entry refreshes on each visit to the History screen

-----

## Screen 3: Settings

### Backend Configuration

- **API URL** — the Lambda Function URL (pre-filled or entered once)
- **API Token** — shared secret for authentication (secure entry, stored in Keychain)

The system serial number and off-peak window are configured server-side in SSM Parameter Store, not in the app.

### Off-Peak Window

Configured server-side in SSM Parameter Store (currently 11:00 AM – 2:00 PM). The container uses this to schedule energy snapshot calls and compute off-peak deltas. The app reads the window times from the `/status` response for display purposes only.

### Load Alert Threshold

- Configurable watt value above which load is highlighted in red
- Stored locally in UserDefaults (purely a display preference)

### Validation

On save, call `/status` to verify the backend is reachable and returning data.

-----

## Backend Deployment

The backend is deployed via CloudFormation:

- **VPC** with public subnets across 2 AZs, Internet Gateway, and DynamoDB Gateway VPC endpoint
- **ECS Cluster** with a single Fargate service (1 desired task, 0.25 vCPU, 0.5 GB, ARM), spread across 2 AZs for failover
- **GHCR (GitHub Container Registry)** for the public container image — no pull credentials needed in ECS
- **DynamoDB Tables** (5 tables as described above)
- **Lambda Function** with Function URL enabled
- **IAM Roles** for ECS task execution, Lambda DynamoDB access
- **SSM Parameter Store** for AlphaESS AppID, AppSecret (SecureString), system serial number, and off-peak window configuration

The Fargate container and Lambda are both written in Go. Container images are hosted publicly on GHCR (`ghcr.io`), built and pushed via GitHub Actions on merge to main. No pull credentials needed in ECS since the image is public.

-----

## Platform Considerations

V1 targets iPhone only, but architectural decisions should not preclude iPad and macOS:

- Use SwiftUI’s adaptive layout (NavigationSplitView or similar) so the Dashboard and History can sit side-by-side on wider screens without a rewrite
- Keep the API client, data models, and business logic in a shared layer with no UIKit dependencies
- Store credentials via Keychain with App Group access so a future widget extension can authenticate independently
- Use SwiftData for cached data — this will sync naturally if CloudKit is added later

-----

## Considered and Deferred to V2+

- **Widgets** — home screen and lock screen widgets showing battery %, current load, and solar generation. Will require a shared App Group container for the widget extension to access cached data and Keychain credentials.
- **iPad / macOS** — SwiftUI makes multi-platform straightforward. Dashboard layout should adapt to larger screens (side-by-side metrics + chart). Build as a single target with adaptive layout rather than separate targets.
- **Shortcuts integration** — App Intents for querying battery status, current load, solar generation. Useful for automation (e.g. “if battery below 20%, send notification”)
- **Push notifications** — alert when battery drops below a threshold, or when grid import exceeds a threshold during peak. Would require SNS or APNs integration from the backend.
- **Data export** — markdown or CSV export of historical energy data
- **Data compaction** — downsample older `flux-readings` data from 10-second to 5-minute or hourly averages to manage long-term storage

-----

## Resolved Questions

1. **Multiple systems**: Single system confirmed. V1 assumes single system, no selector needed.
1. **Off-peak energy used**: Computed exactly by diffing `getOneDateEnergyBySn` snapshots taken at the start and end of the off-peak window. No approximation needed.
1. **Off-peak charging**: Same snapshot diff approach gives exact `eCharge` delta during the off-peak window, plus all other energy metrics.
1. **Architecture**: Backend on AWS Fargate chosen over Cloudflare Workers (existing AWS credits make it effectively free). Backend eliminates V1/V2 feature split — rolling averages and off-peak stats are available from day one.
1. **Off-peak window sync**: Server-side config in SSM Parameter Store (11:00 AM – 2:00 PM). The container takes energy snapshots at the start and end of the off-peak window and computes deltas. No client-side involvement.
1. **Backend language**: Go. Smaller container image, faster Lambda cold starts, and fits the existing tooling.
1. **Container image hosting**: GHCR. Code and images are public, so no pull credentials needed in ECS. GitHub Actions has native GHCR integration via `GITHUB_TOKEN`. One fewer AWS resource to manage.
1. **Dashboard layout**: Hybrid — centred battery hero, power trio, then vertical list for secondary stats and today’s energy.
1. **History layout**: Grouped vertical bar chart with day detail drill-down using stacked time-series charts (SOC, power, battery power).
1. **Today’s energy on dashboard**: Included as a “Today” section at the bottom of the dashboard, using default text colour only (no conditional colouring).
1. **Off-peak grid usage calculation**: Resolved by taking `getOneDateEnergyBySn` snapshots at the start and end of the off-peak window and diffing the cumulative `eInput` values. This gives exact off-peak grid usage (and all other energy metrics) without needing to accumulate 10-second readings.