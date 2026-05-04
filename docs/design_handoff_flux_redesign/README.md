# Handoff: Flux Redesign (Dashboard + Today)

## Overview

This is a redesign of the Flux iOS app's two main screens — **Dashboard** and **Today** (currently called Day Detail in the codebase). The redesign also introduces a **top tab bar** to navigate between Dashboard / Today / History, replacing whatever navigation pattern is currently in place.

The **History** screen redesign is intentionally **deferred** to a later round and is not part of this handoff.

The redesign keeps Flux's existing iOS / SwiftUI / SwiftData architecture and the existing chart implementations on Day Detail. The visible changes are: a new visual system (dark glass), a reorganized field set, an off-peak block surfaced as a first-class concept on every screen, and the tab-bar navigation.

## About the design files

The files in this bundle are **HTML design references** built in React. They are mocks showing intended look, layout, and behaviour — **not production code to copy**.

The task is to recreate these designs in the existing **Flux iOS codebase** using its established patterns:

- SwiftUI views matching the existing folder structure (`Dashboard/`, `DayDetail/`, etc.)
- `@Observable` view-models (already the pattern — see `DashboardViewModel`, `DayDetailViewModel`)
- `FluxCore` types (`LiveData`, `BatteryInfo`, `OffpeakData`, `TodayEnergy`, `DailyUsage`, etc.) — re-use as-is
- SwiftData for persistence (already in place)
- Swift Charts for chart rendering — **keep the existing `PowerChartView`, `BatteryPowerChartView`, `SOCChartView` exactly as they are**

## Fidelity

**High-fidelity.** Colors, spacing, typography, copy, and layout in the mocks are intentional and should be matched closely. The HTML/SVG charts in the mocks are stand-ins to convey shape — the real Day Detail charts already exist in the codebase and should be kept.

## Files in this bundle

- `prototype/Flux Design Review v5.html` — the latest review canvas (open in a browser to see live)
- `prototype/screens/v5.jsx` — the canonical V5 React source. **All measurements, colors, and copy come from this file.**
- `prototype/design-canvas.jsx`, `prototype/ios-frame.jsx` — canvas chrome only (ignore)
- `source/` — copies of the relevant existing Swift files (Dashboard, Day Detail, Navigation) for reference
- `screenshots/` — *(not included by default; ask if you want them)*

Open `Flux Design Review v5.html` in a browser to interact with the live prototype (the "Prototype" artboard at the top of the V5 section has a working tab bar).

---

## Design tokens

All numeric values are in points (iOS native). Colors are listed as hex; alpha-bearing values are listed as RGBA so you can map them to `Color(.sRGB, …)` or asset-catalog colors.

### Colors

| Token | Value | Used for |
|---|---|---|
| `bg` | `#0A0A0C` | App background — near-black, slightly warm |
| `panel` | `rgba(255,255,255,0.04)` | Card / panel fill |
| `border` | `rgba(255,255,255,0.07)` | Card border, hairline dividers |
| `text` | `#FFFFFF` | Primary text |
| `secondary` | `rgba(235,235,245,0.55)` | Row labels, secondary text |
| `tertiary` | `rgba(235,235,245,0.32)` | Eyebrows, units, captions |
| `amber` | `#FFB347` | Solar, battery hero number |
| `offpeak` | `#5AC8FA` | Off-peak (free-grid) accent — the 3-hour cyan |
| `grid` | `#FF6B6B` | Grid imported (paid) |
| `gridExp` | `#7BE0A3` | Grid exported |
| `battery` | `#BF5AF2` | Battery power line / fill |
| `soc` | `#FFD089` | SOC chart line |
| `load` | `#F5E9D8` | House load line |
| `night` | `#5B6CFF` | Night time-block segment |

### Typography

| Role | Font | Reasoning |
|---|---|---|
| **Body, labels, values** | **San Francisco** (system) | iOS-native. Tabular numerals on every value (`fontVariantNumeric: 'tabular-nums'` → `.monospacedDigit()` in SwiftUI). |
| **Hero number** (battery % on Dashboard) | **Geist** (default) | More distinctive than SF for the giant 92pt numeral. |
| **Eyebrow / unit text** | SF (mono variant for short codes like `kWh` is acceptable but not required) | |

#### Hero font picker — handover note

The user wants the hero font to be **selectable inside the app** so it can be tested on a real device with real type rendering. **Build a font picker in Settings** that controls only the Dashboard hero numeral (battery %). Body and values stay on San Francisco unconditionally.

Suggested options to expose:
1. Geist (default)
2. San Francisco
3. IBM Plex Sans
4. Inter
5. Fraunces
6. Newsreader
7. Instrument Sans
8. JetBrains Mono

Persist via `UserDefaults`. Bundle the chosen Google Fonts as on-device assets — do not fetch at runtime.

#### Type scale used in the design

| Use | Size | Weight | Letter-spacing |
|---|---|---|---|
| Hero number (e.g. "62") | 92pt | 300 | -3.5 |
| Hero unit ("%") | 28pt | 300 | — |
| Page title ("Battery", "Today") | 30pt | 600 | -0.6 |
| Eyebrow ("NOW · 17:38 · MAY 4") | 10pt | 600 UPPERCASE | 1.6 |
| Panel header ("OFF-PEAK", "POWER") | 10pt | 700 UPPERCASE | 1.2 |
| Panel header right ("kW", "± kW") | 10pt mono | 400 | — |
| Live trio value ("0.42") | 22pt | 500 | — |
| Live trio label ("SOLAR") | 10pt | 700 UPPERCASE | 1.0 |
| Live trio sub ("producing") | 10pt | 400 | — |
| Stat row label / value | 13pt | 400 | — |
| Stat row sub ("paid", "free", "SOC at 11:14") | 9pt mono | 400 | — |
| TOU block name | 13pt | 400 (600 if highlighted) | — |
| TOU block time | 11pt mono | 400 | — |
| Tab bar item | 12pt | 500 (600 active) | 0.1 |

### Spacing & shape

| Token | Value |
|---|---|
| Screen horizontal padding | 16pt |
| Screen bottom padding | 24pt |
| Panel padding | 16pt (20pt for the hero panel; 14pt internal for live-trio columns) |
| Panel corner radius | 18pt |
| Panel border | 0.5pt at `rgba(255,255,255,0.07)` |
| Panel backdrop | `blur(24px) saturate(180%)` — use `.background(.ultraThinMaterial)` or a custom blurred view |
| Stat row vertical padding | 7pt top + 7pt bottom |
| Stat row divider | 0.5pt `border` color |
| Gap between stacked panels | 12pt |
| Tab bar corner radius | 10pt (outer), 8pt (active pill) |
| Tab bar padding | 3pt |
| TOU block colour bar | 3pt × 18pt, 2pt corner radius |

---

## Screens

### Tab bar (universal)

Sits **above** the page title on every screen. Three segments: **Dashboard · Today · History**.

- Container: `display: flex`, `rgba(255,255,255,0.05)` background, 0.5pt border, 10pt corner radius, 3pt padding, 2pt gap between items, backdrop blur 20px.
- Item: `flex: 1`, 12pt SF, weight 500. Active item: weight 600, white text, `rgba(255,255,255,0.12)` pill background, 8pt corner radius. Inactive item: secondary text, transparent background.
- Transition: 0.18s ease on background and color.
- SwiftUI: a `Picker` styled as `.segmented` will not match this aesthetic — implement as a custom `HStack` of `Button`s or a `ZStack` with a sliding selection pill.

### Dashboard

**Purpose:** "Are we ok right now?" — a glance at battery state, current power flow, what off-peak did, and totals so far today.

**Sections, top to bottom:**

1. **Header** (no card): tab bar → eyebrow ("Now · 17:38 · May 4") → title ("Battery").

2. **Hero panel** — battery % numeral.
   - Padding 20pt.
   - 92pt hero number in the chosen hero font (Geist by default), color `amber`, weight 300.
   - 28pt "%" sign, color `tertiary`, weight 300.
   - Below: 13pt secondary line. Format: `Discharging · 1.4 kW · empty by [HH:MM]` — the time is colored `amber` and uses tabular numerals. Charging case: `Charging · 1.2 kW · full by [HH:MM]`. Idle case: `Idle · battery holding`.
   - **No graph, no ribbon.** This was reverted from earlier rounds.

3. **Live trio panel** — 3 columns side-by-side, no padding on the parent; each column has 14pt internal padding.
   - Columns: **Solar / House / Grid**. Hairline left-border between columns.
   - Per column: 10pt `tertiary` UPPERCASE label → 22pt value (500 weight, tabular nums) + 10pt unit (`tertiary`) → 10pt secondary subline.
   - Label colors: Solar value = `amber`, House = `text`, Grid = `gridExp` when exporting / `grid` when importing.
   - **Important: when exporting, do NOT show a minus sign.** Show `0.05 kW` and use the word `exporting` in the subline. The colour and word do the work.
   - Subline copy: `producing` / `using` / `exporting` or `importing` (match the verb to direction).

4. **Off-peak block** — see **Off-peak block (universal)** below.

5. **Today so far** — see **Summary block (universal)** below; pass `title="Today so far"`, `right` = current time `17:38`.

### Today (Day Detail)

**Purpose:** "What happened today, in detail?" — three charts (kept as-is from the existing app), summary, off-peak panel, and a TOU breakdown.

**Sections, top to bottom:**

1. **Header**: tab bar → eyebrow ("Sun · May 4 · 2026") → title ("Today").

2. **Power chart panel** — keep the existing `PowerChartView`. Card around it has panel header `Power` (left) + `kW` (right). Below the chart, a 6pt-margin row of 10pt secondary legend chips: 8pt-square swatches in `amber`, `load`, `grid` next to "Solar", "House", "Grid".

3. **Battery power chart panel** — keep `BatteryPowerChartView`. Header: `Battery power` / `± kW`.

4. **Battery SOC chart panel** — keep `SOCChartView`. Header: `Battery SOC` / `%`.

5. **Summary block** — `title="Summary"`, `right="May 4"` (or whichever date the day represents).

6. **Off-peak block** — same component as Dashboard.

7. **The day in five blocks** (TOU panel) — a horizontal stacked bar above five labelled rows.

   - Bar: 8pt tall, 2pt gap between segments. Each segment width is proportional to its kWh.
   - Segments and rows in order: **Night** (00–06:30, `night`), **Morning peak** (06:30–11, `grid`), **Off-peak** (11–14, `offpeak`, **highlighted row**), **Afternoon peak** (14–18:42, `grid`), **Evening** (18:42–24, `night` at 0.4 alpha).
   - Off-peak row: 600 weight, `offpeak` color label.
   - Other rows: regular weight, white label.
   - Each row layout: 3pt-wide colour bar → name (flex 1) → time (mono, tertiary, 11pt) → value (tabular nums, min-width 50pt, right-aligned, "X.X kWh").
   - 7pt vertical padding per row, 0.5pt divider between (none after the last).

### History

**Deferred.** A placeholder card is shown so the tab bar resolves to something, but the redesign is parked. Don't ship a placeholder; either keep the current `HistoryView` untouched behind the new tab, or implement a minimal version of it that matches the new dark visual system without changing its information architecture.

---

## Universal components

### Off-peak block

**No title row.** Four `StatRow`s in a `V5Panel`:

| Label | Value | Sub | Accent |
|---|---|---|---|
| `Free grid in` | `{kWh} kWh` from `OffpeakData` | — | `offpeak` |
| `Battery charged` | `+{Δ}%` SOC delta during the window | — | — |
| `Lowest` | `{minSOC}%` | `SOC at HH:MM` (timestamp of the dip) | — |
| `15m avg load` | `{kW} kW` | — | — |

The same component is used on Dashboard and Today. **Identical fields, identical order.**

> Why no title? The off-peak window is a short, fixed 3-hour band that doesn't need a label every time it appears — the data shape is recognizable. The earlier design had a header reading "OFF-PEAK · 11:00 – 14:00 · DONE TODAY" — all of that has been removed.

### Summary block

A `V5Panel` with a panel header (`label`, optional `right`) and six `StatRow`s. The order and labels are fixed:

| Label | Value | Sub | Accent |
|---|---|---|---|
| `Solar produced` | `{kWh} kWh` | — | `amber` |
| `House used` | `{kWh} kWh` | — | — |
| `Grid in (peak)` | `{kWh} kWh` | `paid` | `grid` |
| `Grid in (off-peak)` | `{kWh} kWh` | `free` | `offpeak` |
| `Grid out` | `{kWh} kWh` | — | `gridExp` |
| `Battery cycle` | `{charged} / {discharged} kWh` | — | — |

Used on:
- Dashboard with `title="Today so far"`, `right=` current time (e.g. `"17:38"`)
- Today with `title="Summary"`, `right=` the day's short date (e.g. `"May 4"`)

### StatRow

Two-baseline-aligned items in a flex row:
- **Left**: 13pt label in `secondary`. If a `sub` is provided, append a 9pt mono `tertiary` chip 6pt to its right.
- **Right**: 13pt value, tabular numerals, color = `accent` (or `text` if none).
- 7pt top + 7pt bottom padding.
- 0.5pt `border` color hairline along the bottom edge — except on the row marked `last`.

### V5Panel

The panel chrome:
- Background `panel` (white at 4% alpha)
- `blur(24px) saturate(180%)` backdrop
- 0.5pt `border` color border
- 18pt corner radius
- 16pt padding (override per use)
- White text default

In SwiftUI: `.background(.ultraThinMaterial.opacity(0.7))` plus `.overlay(RoundedRectangle(...).strokeBorder(...))` is a reasonable starting point; tune until it matches the mocks.

---

## Interactions & behavior

- **Tab switch**: instant. No transition between screens needed (the SwiftUI default `.tabViewStyle` swap is fine, or a 180ms fade if `TabView` chrome is bypassed).
- **Stat rows**: not interactive. Read-only.
- **Hero number**: not interactive in this round. (Future: tap to open battery detail — ignore for now.)
- **Charts**: existing `Chart` interactivity (Swift Charts) is preserved.
- **Empty / no-data states**:
  - Off-peak window not yet started today → grey out the block, show `—` for every value, keep the same row structure.
  - Off-peak window currently running → current implementation already handles this; surface no special state in the redesign, just live values.
  - No solar today (e.g. weather / outage) → `Solar produced` shows `0.00 kWh`, no special copy.

## State / data mapping

The redesign reuses existing types — no new persistence needed.

| Field | Source |
|---|---|
| Hero number (battery %) | `LiveData.batteryPercent` |
| Hero subline (mode + power + ETA) | `LiveData.batteryMode`, `LiveData.batteryPower`, computed ETA from `rolling15min` |
| Live trio: Solar | `LiveData.solarPower` |
| Live trio: House | `LiveData.housePower` |
| Live trio: Grid (sign + verb) | `LiveData.gridPower` (negative → exporting; positive → importing); display absolute value, set verb in subline |
| Off-peak: Free grid in | `OffpeakData.gridImportKWh` |
| Off-peak: Battery charged | `OffpeakData.batterySOCDelta` |
| Off-peak: Lowest + timestamp | min over the window from `parsedReadings` |
| Off-peak: 15m avg load | `rolling15min.averageHouseLoad` (or equivalent existing field) |
| Summary fields | `TodayEnergy` / `DailyUsage` per existing helper `EnergySummaryFormatter` |
| TOU blocks | existing `PeakPeriod` array on `DayDetailViewModel` |

If any of those fields don't exist on the model yet (Lowest SOC + timestamp, 15m avg load), they should be derived in the view-model — not the view.

## Accessibility

- Maintain a contrast ratio of 4.5:1 for body text — the secondary `rgba(235,235,245,0.55)` on `#0A0A0C` passes; tertiary at 0.32 alpha is sub-AA and is acceptable only for **non-essential** text (eyebrows, units). Don't put critical info in tertiary.
- Hero number at 92pt is fine; the "%" at 28pt in tertiary is borderline — acceptable because the value is also screen-reader-announced as "62 percent" via accessibility label.
- All values use tabular figures so screen-reader navigation through the stat list reads predictably.
- Tab bar items get `accessibilityTraits = .isButton` and an `accessibilityLabel` that includes "selected" for the active tab.
- Dynamic Type: scale the type scale proportionally; cap hero at the user's chosen size but never below 64pt.

## Assets

No new images or icons required. The redesign is type + color only.

## Out of scope

- **Widgets** — explicitly excluded from this review round. Don't touch `FluxWidgets/`.
- **Settings** — excluded except for adding the font picker (see handover note above).
- **History** — deferred.

---

## How to run the prototype

1. Open `prototype/Flux Design Review v5.html` in a modern browser.
2. The **V5** section is at the top. The leftmost artboard ("Prototype") has a working tab bar — tap Dashboard / Today / History to see the screens switch.
3. The other rounds (V4, V3, V2, V1) below are kept for context — they document how the design evolved through review.

## Questions for the developer

If anything in the README is ambiguous, the canonical source of truth is **`prototype/screens/v5.jsx`**. Every measurement and color in this README was lifted directly from that file.
