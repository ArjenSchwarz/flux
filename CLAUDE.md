# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Flux is a personal AlphaESS battery monitoring system with two main components:

1. **AWS Backend** — Go-based ECS Fargate poller + Lambda API that polls the AlphaESS API, stores data in DynamoDB, and serves pre-computed stats via Lambda Function URL
2. **iOS App** — Swift/SwiftUI iOS 26+ app with Dashboard, History, Day Detail, and Settings screens

The app never talks to AlphaESS directly. The backend handles all API polling and serves three endpoints: `/status`, `/history?days=N`, and `/day?date=YYYY-MM-DD`. App authenticates to the Lambda via a bearer token stored in SSM Parameter Store.

## Architecture

```
AlphaESS API <-- ECS Fargate Poller (Go, ARM64) --> DynamoDB (5 tables)
                                                         ^
iOS App --> Lambda Function URL (Go, ARM64) -------------+
```

- **Poller** polls AlphaESS on multiple schedules (10s for live data, hourly/6h/24h for summaries) and writes to DynamoDB
- **Lambda API** reads from DynamoDB and computes derived stats (rolling averages, cutoff estimates, off-peak deltas, peak usage periods)
- **DynamoDB tables** (11, all PAY_PER_REQUEST): `flux-readings` (TTL 30d), `flux-daily-power` (TTL 30d), `flux-daily-energy`, `flux-system`, `flux-offpeak`, `flux-notes` (PITR), `flux-devices` (PITR), `flux-soc-rules` (PITR), `flux-soc-fire-state` (TTL 7d), `flux-pricing` (PITR), `flux-simulation-presets` (PITR)
- Off-peak energy is computed by diffing `getOneDateEnergyBySn` snapshots at window start/end (configured via SSM: 11:00-14:00)

## Infrastructure Deployment

Single CloudFormation stack. All infra is in `infrastructure/template.yaml`.

**First-time setup** — create SecureString params manually (CloudFormation can't manage SecureString):
```bash
aws ssm put-parameter --name "/flux/app-secret" --type SecureString --value "SECRET"
aws ssm put-parameter --name "/flux/api-token" --type SecureString --value "TOKEN"
```

**Build and deploy:**
```bash
# Build Lambda binary (ARM64 Linux)
GOOS=linux GOARCH=arm64 go build -o lambda/bootstrap ./cmd/api

# Package (uploads Lambda zip to S3)
aws cloudformation package \
  --template-file infrastructure/template.yaml \
  --s3-bucket BUCKET \
  --output-template-file infrastructure/packaged.yaml

# Deploy
aws cloudformation deploy \
  --template-file infrastructure/packaged.yaml \
  --stack-name flux \
  --capabilities CAPABILITY_IAM \
  --parameter-overrides ContainerImageUri=ghcr.io/OWNER/flux-poller:latest \
    AlphaESSAppId=APP_ID SystemSerialNumber=SERIAL
```

**Force container redeploy** (e.g. after pushing a new image):
```bash
aws ecs update-service --cluster flux --service flux-poller --force-new-deployment
```

## Specs and Features

Feature specs live in `specs/<feature-name>/` with: `requirements.md`, `design.md`, `tasks.md`, `decision_log.md`, and optionally `prerequisites.md` and `implementation.md`. All files in a feature's directory should be considered together when discussing that feature.

The V1 plan (`docs/flux-v1.md`) is the authoritative product spec covering both the backend and iOS app.

## Key Design Decisions

- **Backend language**: Go — smaller container images, faster Lambda cold starts
- **ARM64 everywhere** — both Fargate and Lambda use Graviton for cost savings
- **No API Gateway** — Lambda Function URL is sufficient for a two-user app
- **Container images on GHCR** — public images, no pull credentials needed in ECS
- **DynamoDB on-demand billing** — low volume (~260K writes/month), on-demand is cheaper than provisioned
- **DeletionPolicy: Retain** on DynamoDB tables, **Delete** on log groups

## Data Consistency

A value that appears on more than one screen MUST be computed the same way and show the same number on every screen. This applies to today's live values as much as to historical ones — there is no acceptable reason for today's peak grid import (or any other shared metric) to read differently on the Dashboard, Day Detail, and History.

- Compute a shared metric once — server-side where possible, otherwise in a single FluxCore helper — and have every view consume that one source rather than re-deriving it locally.
- If a metric can only be approximated when the canonical value is absent (a fallback), every screen showing that metric MUST use the same fallback, so the screens never disagree.

## iOS App

- iOS 26+ with Liquid Glass styling
- SwiftUI Charts for all graphs (no third-party charting)
- SwiftData for caching, Keychain for credentials
- Auto-refresh every 10s on Dashboard
- Architecture should support future iPad/macOS/widgets without rewrite

## macOS App

- macOS 26+ native build (not Catalyst, not Designed-for-iPad)
- Shares FluxCore, views, and widget extension with iOS
- Dedicated Settings scene (⌘,), ⌘R refresh, ←/→ Day Detail navigation
- Refresh tiers: 10s active, 60s inactive (via `appearsActive`)
- Credentials sync via iCloud Keychain + `NSUbiquitousKeyValueStore`
- Control Center widget (`ControlWidget` protocol)
- Build/test: `make macos-build`, `make macos-test`, `make macos-lint`
