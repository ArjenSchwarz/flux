# Specs Overview

| Name | Creation Date | Status | Summary |
|------|---------------|--------|---------|
| [Infrastructure](#infrastructure) | 2026-04-12 | Done | Single CloudFormation stack for the AWS backend: VPC, ECS Fargate, Lambda, DynamoDB, SSM. |
| [Poller](#poller) | 2026-04-14 | Done | Go ECS Fargate task that polls AlphaESS, writes five DynamoDB tables, computes off-peak deltas. |
| [Lambda API](#lambda-api) | 2026-04-15 | Done | Go Lambda behind a Function URL serving /status, /history, /day from DynamoDB. |
| [iOS App](#ios-app) | 2026-04-16 | Done | SwiftUI iOS 26+ app: Dashboard, History, Day Detail, Settings; reads only the Lambda API. |
| [Realtime Energy](#realtime-energy) | 2026-04-16 | Done | Compute today's energy by integrating live readings instead of relying on 6-hourly snapshots. |
| [Peak Usage Periods](#peak-usage-periods) | 2026-04-17 | Done | Day detail card highlighting top high-consumption periods outside the off-peak window. |
| [Add Widgets](#add-widgets) | 2026-04-21 | Done | WidgetKit home and lock-screen widgets surfacing battery state and live power data. |
| [History Multi Card](#history-multi-card) | 2026-04-26 | Done | History screen rewrite: solar / grid (peak vs off-peak) / battery cards with shared selection. |
| [Evening / Night Stats](#evening--night-stats) | 2026-04-26 | Done | Day detail card showing usage during the no-solar evening (sunset → midnight) and night (midnight → sunrise) periods. |
| [Peak Usage Stats](#peak-usage-stats) | 2026-04-27 | Done | Day detail card replacing Evening/Night with five chronological load blocks (Night, Morning Peak, Off-Peak, Afternoon Peak, Evening). |
| [Day Notes](#day-notes) | 2026-04-28 | Done | Per-date free-text note (≤200 graphemes) shared across users; new `flux-notes` DynamoDB table and PUT /note endpoint; rendered on Dashboard, History, Day Detail; edited only on Day Detail. |

---

## Infrastructure

Single CloudFormation stack for the AWS backend: VPC, ECS Fargate, Lambda, DynamoDB, SSM.

- [decision_log.md](infrastructure/decision_log.md)
- [design.md](infrastructure/design.md)
- [implementation.md](infrastructure/implementation.md)
- [prerequisites.md](infrastructure/prerequisites.md)
- [requirements.md](infrastructure/requirements.md)
- [tasks.md](infrastructure/tasks.md)

## Poller

Go ECS Fargate task that polls AlphaESS, writes five DynamoDB tables, computes off-peak deltas.

- [decision_log.md](poller/decision_log.md)
- [design.md](poller/design.md)
- [implementation.md](poller/implementation.md)
- [requirements.md](poller/requirements.md)
- [tasks.md](poller/tasks.md)

## Lambda API

Go Lambda behind a Function URL serving /status, /history, /day from DynamoDB.

- [decision_log.md](lambda-api/decision_log.md)
- [design.md](lambda-api/design.md)
- [explanation.md](lambda-api/explanation.md)
- [implementation.md](lambda-api/implementation.md)
- [requirements.md](lambda-api/requirements.md)
- [tasks.md](lambda-api/tasks.md)

## iOS App

SwiftUI iOS 26+ app: Dashboard, History, Day Detail, Settings; reads only the Lambda API.

- [decision_log.md](ios-app/decision_log.md)
- [design.md](ios-app/design.md)
- [implementation.md](ios-app/implementation.md)
- [prerequisites.md](ios-app/prerequisites.md)
- [requirements.md](ios-app/requirements.md)
- [tasks.md](ios-app/tasks.md)

## Realtime Energy

Compute today's energy by integrating live readings instead of relying on 6-hourly snapshots.

- [decision_log.md](realtime-energy/decision_log.md)
- [design.md](realtime-energy/design.md)
- [implementation.md](realtime-energy/implementation.md)
- [requirements.md](realtime-energy/requirements.md)
- [tasks.md](realtime-energy/tasks.md)

## Peak Usage Periods

Day detail card highlighting top high-consumption periods outside the off-peak window.

- [decision_log.md](peak-usage-periods/decision_log.md)
- [design.md](peak-usage-periods/design.md)
- [implementation.md](peak-usage-periods/implementation.md)
- [requirements.md](peak-usage-periods/requirements.md)
- [tasks.md](peak-usage-periods/tasks.md)

## Add Widgets

WidgetKit home and lock-screen widgets surfacing battery state and live power data.

- [decision_log.md](add-widgets/decision_log.md)
- [design.md](add-widgets/design.md)
- [implementation.md](add-widgets/implementation.md)
- [prerequisites.md](add-widgets/prerequisites.md)
- [requirements.md](add-widgets/requirements.md)
- [tasks.md](add-widgets/tasks.md)

## History Multi Card

History screen rewrite: solar / grid (peak vs off-peak) / battery cards with shared selection.

- [pre-push-review.md](history-multi-card/pre-push-review.md)

## Evening / Night Stats

Day detail card showing usage during the no-solar evening (sunset → midnight) and night (midnight → sunrise) periods, computed by `/day` from existing readings with a static Melbourne sunrise/sunset table as fallback.

- [decision_log.md](evening-night-stats/decision_log.md)
- [design.md](evening-night-stats/design.md)
- [requirements.md](evening-night-stats/requirements.md)
- [tasks.md](evening-night-stats/tasks.md)

## Peak Usage Stats

Day detail card replacing Evening/Night with five chronological load blocks (Night, Morning Peak, Off-Peak, Afternoon Peak, Evening), each carrying total kWh, average kWh/h, and percent of day. Replaces the `eveningNight` API field with `dailyUsage`.

- [decision_log.md](peak-usage-stats/decision_log.md)
- [design.md](peak-usage-stats/design.md)
- [requirements.md](peak-usage-stats/requirements.md)
- [tasks.md](peak-usage-stats/tasks.md)

## Day Notes

Per-date free-text note (≤200 graphemes after NFC + trim) shared across users. Adds the Lambda's first write endpoint (`PUT /note`) and a new `flux-notes` DynamoDB table with PITR. Notes bundled into `/status`, `/history`, `/day` responses; rendered read-only on Dashboard (today) and History (selected day); editable on Day Detail.

- [decision_log.md](day-notes/decision_log.md)
- [design.md](day-notes/design.md)
- [requirements.md](day-notes/requirements.md)
- [tasks.md](day-notes/tasks.md)
