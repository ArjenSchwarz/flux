# Infrastructure Design

## Overview

This document describes the AWS infrastructure for the Flux backend, deployed as a single CloudFormation stack. The infrastructure provides two compute workloads (an ECS Fargate poller and a Lambda API function), five DynamoDB tables for data storage, and supporting networking, IAM, and configuration resources.

The design follows a principle of minimal moving parts: no API Gateway, no NAT Gateway, no private subnets, no alarms. Every resource exists because it's required by the two compute workloads or the data they exchange.

---

## Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│ CloudFormation Stack                                             │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐    │
│  │ VPC (10.0.0.0/24)                                        │    │
│  │                                                          │    │
│  │  ┌────────────────────┐   ┌────────────────────┐         │    │
│  │  │ Public Subnet AZ-a │   │ Public Subnet AZ-b │         │    │
│  │  │   10.0.0.0/25      │   │   10.0.0.128/25    │         │    │
│  │  │                    │   │                    │         │    │
│  │  │  ┌──────────────┐  │   │                    │         │    │
│  │  │  │ Fargate Task │  │   │  (failover AZ)     │         │    │
│  │  │  │ (poller)     │  │   │                    │         │    │
│  │  │  └──────┬───────┘  │   │                    │         │    │
│  │  └─────────┼──────────┘   └────────────────────┘         │    │
│  │            │                                              │    │
│  │  ┌─────────┴─────────────────────┐                        │    │
│  │  │ Route Table                    │                        │    │
│  │  │  0.0.0.0/0 → IGW              │                        │    │
│  │  │  DynamoDB → VPC Endpoint       │                        │    │
│  │  │  S3       → VPC Endpoint       │                        │    │
│  │  └───────────────────────────────┘                        │    │
│  └──────────────────────────────────────────────────────────┘    │
│                                                                  │
│  ┌────────────────┐     ┌───────────────────────────────────┐    │
│  │ Lambda Function │────▶│ DynamoDB Tables (5)               │    │
│  │ (API)          │◀────│  flux-readings                     │    │
│  │                │     │  flux-daily-energy                 │    │
│  │ Function URL   │     │  flux-daily-power                  │    │
│  │ (public)       │     │  flux-system                       │    │
│  └────────────────┘     │  flux-offpeak                      │    │
│                         └───────────────────────────────────┘    │
│                                                                  │
│  ┌───────────────────────────────────────────────┐               │
│  │ SSM Parameter Store (/flux/*)                  │               │
│  │  Created by stack: app-id, serial, offpeak-*   │               │
│  │  Created manually: app-secret, api-token       │               │
│  └───────────────────────────────────────────────┘               │
└──────────────────────────────────────────────────────────────────┘
         │                              ▲
         ▼                              │
  ┌──────────────┐              ┌───────────────┐
  │ AlphaESS API │              │ iOS App       │
  │ (external)   │              │ (2 users)     │
  └──────────────┘              └───────────────┘
```

### Data Flow

1. **Poller → DynamoDB**: The Fargate poller calls the AlphaESS API on 4 schedules (10s, ~1h, ~6h, ~24h), writes results to the appropriate DynamoDB tables via the VPC Gateway endpoint.
2. **App → Lambda → DynamoDB**: The iOS app calls the Lambda Function URL. The Lambda reads from DynamoDB, computes derived stats, and returns JSON.
3. **No direct connection** between the poller and Lambda. They communicate exclusively through DynamoDB.

---

## Components and Interfaces

### CloudFormation Template Structure

The template is organised into logical sections. All resources are in a single YAML file (`infrastructure/template.yaml`).

**Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `ContainerImageUri` | String | Full GHCR image URI (e.g., `ghcr.io/owner/flux-poller:latest`) |
| `AlphaESSAppId` | String | AlphaESS developer App ID |
| `SystemSerialNumber` | String | AlphaESS system serial number |
| `OffPeakWindowStart` | String | Off-peak window start (e.g., `11:00`). Default: `11:00` |
| `OffPeakWindowEnd` | String | Off-peak window end (e.g., `14:00`). Default: `14:00` |
| `SSMPathPrefix` | String | SSM parameter path prefix. Default: `/flux` |

Sensitive values (AlphaESS App Secret, API bearer token) are **not** template parameters. They are created as SSM SecureString parameters manually before deployment (see [Deployment Procedure](#deployment-procedure)).

**Outputs:**

| Output | Value |
|--------|-------|
| `FunctionUrl` | Lambda Function URL |
| `EcsClusterName` | ECS cluster name |
| `EcsServiceName` | ECS service name |

### VPC and Networking

**Resources:**

| Resource | Type | Key Properties |
|----------|------|---------------|
| `Vpc` | `AWS::EC2::VPC` | CIDR `10.0.0.0/24`, DNS enabled |
| `SubnetA` | `AWS::EC2::Subnet` | `10.0.0.0/25`, AZ selected via `!Select [0, !GetAZs]` |
| `SubnetB` | `AWS::EC2::Subnet` | `10.0.0.128/25`, AZ selected via `!Select [1, !GetAZs]` |
| `InternetGateway` | `AWS::EC2::InternetGateway` | Attached to VPC |
| `RouteTable` | `AWS::EC2::RouteTable` | Default route to IGW, associated with both subnets |
| `DynamoDBEndpoint` | `AWS::EC2::VPCEndpoint` | Gateway type, `com.amazonaws.{region}.dynamodb` |
| `S3Endpoint` | `AWS::EC2::VPCEndpoint` | Gateway type, `com.amazonaws.{region}.s3` |
| `SecurityGroup` | `AWS::EC2::SecurityGroup` | Egress: all traffic allowed. Ingress: none. |

The security group is shared by the Fargate task. It allows all outbound (needed for AlphaESS API and GHCR image pull) and blocks all inbound (the poller has no listening ports).

### ECS Fargate Poller

**Resources:**

| Resource | Type | Key Properties |
|----------|------|---------------|
| `EcsCluster` | `AWS::ECS::Cluster` | Default settings |
| `TaskDefinition` | `AWS::ECS::TaskDefinition` | Fargate, ARM64, 256 CPU, 512 Memory |
| `PollerService` | `AWS::ECS::Service` | Desired count 1, AZ spread placement |
| `TaskExecutionRole` | `AWS::IAM::Role` | SSM read, CloudWatch Logs write |
| `TaskRole` | `AWS::IAM::Role` | DynamoDB read + write |
| `PollerLogGroup` | `AWS::Logs::LogGroup` | 14-day retention, DeletionPolicy Delete |

**Task Definition Details:**

```yaml
TaskDefinition:
  RequiresCompatibilities: [FARGATE]
  RuntimePlatform:
    CpuArchitecture: ARM64
    OperatingSystemFamily: LINUX
  Cpu: "256"
  Memory: "512"
  NetworkMode: awsvpc
  ExecutionRoleArn: !GetAtt TaskExecutionRole.Arn
  TaskRoleArn: !GetAtt TaskRole.Arn
  ContainerDefinitions:
    - Name: poller
      Image: !Ref ContainerImageUri
      Essential: true
      Secrets:
        - Name: ALPHA_APP_ID
          ValueFrom: !Sub "${SSMPathPrefix}/app-id"
        - Name: ALPHA_APP_SECRET
          ValueFrom: !Sub "${SSMPathPrefix}/app-secret"
        - Name: SYSTEM_SERIAL
          ValueFrom: !Sub "${SSMPathPrefix}/serial"
      Environment:
        - Name: OFFPEAK_START
          Value: !Ref OffPeakWindowStart
        - Name: OFFPEAK_END
          Value: !Ref OffPeakWindowEnd
        - Name: AWS_REGION
          Value: !Ref AWS::Region
      HealthCheck:
        Command:
          - CMD
          - /poller
          - healthcheck
        Interval: 60
        Timeout: 10
        Retries: 3
        StartPeriod: 120
      LogConfiguration:
        LogDriver: awslogs
        Options:
          awslogs-group: !Ref PollerLogGroup
          awslogs-region: !Ref AWS::Region
          awslogs-stream-prefix: poller
```

**Health Check Design:**

The poller Go binary exposes a `healthcheck` subcommand. When invoked, it:

1. Queries the `flux-readings` table for the most recent reading by the configured serial number (a single `Query` with `ScanIndexForward: false`, `Limit: 1`)
2. Checks that the reading timestamp is within the last 300 seconds (5 minutes)
3. Exits with code 0 (healthy) if the timestamp is recent, or code 1 (unhealthy) if stale or missing

ECS runs this command every 60 seconds. After 3 consecutive failures (with a 120-second start-up grace period), ECS marks the task as unhealthy and the service replaces it. The 5-minute staleness window avoids false positives during brief AlphaESS API outages while still catching genuinely hung containers within ~8 minutes.

The health check binary runs inside the same container and reuses the same Go code and AWS SDK configuration as the poller itself. It needs the task role's DynamoDB read permission, which is already granted.

**SSM Parameter Access:**

The task definition uses the `Secrets` property to inject SSM parameters as environment variables at container start. This includes SecureString parameters — ECS decrypts them automatically using the task execution role's SSM permissions.

- `ALPHA_APP_ID` → `/flux/app-id` (String, created by stack)
- `ALPHA_APP_SECRET` → `/flux/app-secret` (SecureString, created manually)
- `SYSTEM_SERIAL` → `/flux/serial` (String, created by stack)

Off-peak window times use the `Environment` property (plain values from template parameters) since they are not sensitive.

**Service Configuration:**

```yaml
PollerService:
  LaunchType: FARGATE
  DesiredCount: 1
  NetworkConfiguration:
    AwsvpcConfiguration:
      AssignPublicIp: ENABLED
      SecurityGroups:
        - !Ref SecurityGroup
      Subnets:
        - !Ref SubnetA
        - !Ref SubnetB
```

Note: Fargate does not support `PlacementStrategies`. AZ distribution is automatic — ECS spreads task launches across the provided subnets. By listing both subnets, a replacement task can launch in either AZ.

### DynamoDB Tables

All 5 tables follow the same pattern: on-demand billing, DeletionPolicy Retain.

| Table | PK | SK | TTL |
|-------|----|----|-----|
| `flux-readings` | `sysSn` (S) | `timestamp` (N) | `ttl` — 30 days |
| `flux-daily-energy` | `sysSn` (S) | `date` (S) | None |
| `flux-daily-power` | `sysSn` (S) | `uploadTime` (S) | `ttl` — 7 days |
| `flux-system` | `sysSn` (S) | — | None |
| `flux-offpeak` | `sysSn` (S) | `date` (S) | None |

No Global Secondary Indexes are needed. All queries use the partition key (serial number) with sort key conditions, which the base table handles directly.

TTL values are computed by the poller at write time: `time.Now().Add(30 * 24 * time.Hour).Unix()` for readings and `time.Now().Add(7 * 24 * time.Hour).Unix()` for daily power.

### Lambda Function and Function URL

**Resources:**

| Resource | Type | Key Properties |
|----------|------|---------------|
| `ApiFunction` | `AWS::Lambda::Function` | `provided.al2023`, ARM64, 128 MB, 10s timeout |
| `ApiFunctionUrl` | `AWS::Lambda::Url` | Auth type NONE |
| `ApiFunctionUrlPermission` | `AWS::Lambda::Permission` | Allow `lambda:InvokeFunctionUrl` for public access |
| `LambdaExecutionRole` | `AWS::IAM::Role` | DynamoDB read, SSM read, CloudWatch Logs write |
| `ApiLogGroup` | `AWS::Logs::LogGroup` | 14-day retention, DeletionPolicy Delete |

**Lambda Configuration:**

```yaml
ApiFunction:
  Runtime: provided.al2023
  Architectures: [arm64]
  Handler: bootstrap
  MemorySize: 128
  Timeout: 10
  Code: ./lambda/  # Rewritten by `aws cloudformation package` to S3 reference
  Role: !GetAtt LambdaExecutionRole.Arn
  Environment:
    Variables:
      API_TOKEN_PARAM: !Sub "${SSMPathPrefix}/api-token"
      SYSTEM_SERIAL_PARAM: !Sub "${SSMPathPrefix}/serial"
      OFFPEAK_START: !Ref OffPeakWindowStart
      OFFPEAK_END: !Ref OffPeakWindowEnd
      TABLE_READINGS: !Ref ReadingsTable
      TABLE_DAILY_ENERGY: !Ref DailyEnergyTable
      TABLE_DAILY_POWER: !Ref DailyPowerTable
      TABLE_SYSTEM: !Ref SystemTable
      TABLE_OFFPEAK: !Ref OffpeakTable
```

The Lambda reads the API token from SSM at cold start (not on every request) and caches it in memory. Table names are passed as environment variables so the Lambda doesn't hardcode them.

The `Code` property points to a local directory (`./lambda/`) containing the compiled Go binary named `bootstrap`. During deployment, `aws cloudformation package` zips this directory, uploads it to S3, and rewrites the template to reference the S3 location.

**Function URL:**

```yaml
ApiFunctionUrl:
  TargetFunctionArn: !GetAtt ApiFunction.Arn
  AuthType: NONE

ApiFunctionUrlPermission:
  Action: lambda:InvokeFunctionUrl
  FunctionName: !Ref ApiFunction
  Principal: "*"
  FunctionUrlAuthType: NONE
```

Auth type NONE means anyone with the URL can invoke the function. The Lambda itself validates the `Authorization: Bearer {token}` header on every request. This is a deliberate trade-off for a 2-user app (see Decision 5).

### SSM Parameter Store

**Created by the stack (String type):**

| Parameter Path | Source |
|---------------|--------|
| `/flux/app-id` | `AlphaESSAppId` template parameter |
| `/flux/serial` | `SystemSerialNumber` template parameter |
| `/flux/offpeak-start` | `OffPeakWindowStart` template parameter |
| `/flux/offpeak-end` | `OffPeakWindowEnd` template parameter |

**Created manually before deployment (SecureString type):**

| Parameter Path | Purpose |
|---------------|---------|
| `/flux/app-secret` | AlphaESS App Secret |
| `/flux/api-token` | Bearer token for Lambda API authentication |

### IAM Roles

Three roles with least-privilege policies:

**1. TaskExecutionRole** (used by ECS agent to set up the task):

```yaml
Policies:
  - Statement:
      - Effect: Allow
        Action:
          - ssm:GetParameters
        Resource:
          - !Sub "arn:aws:ssm:${AWS::Region}:${AWS::AccountId}:parameter${SSMPathPrefix}/*"
      - Effect: Allow
        Action:
          - logs:CreateLogStream
          - logs:PutLogEvents
        Resource:
          - !GetAtt PollerLogGroup.Arn
```

No ECR permissions. GHCR images are pulled via HTTPS directly by the Fargate agent — no AWS-level authorisation is needed.

**2. TaskRole** (used by the poller container at runtime):

```yaml
Policies:
  - Statement:
      - Effect: Allow
        Action:
          - dynamodb:PutItem
          - dynamodb:UpdateItem
          - dynamodb:BatchWriteItem
          - dynamodb:GetItem
          - dynamodb:Query
        Resource:
          - !GetAtt ReadingsTable.Arn
          - !GetAtt DailyEnergyTable.Arn
          - !GetAtt DailyPowerTable.Arn
          - !GetAtt SystemTable.Arn
          - !GetAtt OffpeakTable.Arn
```

Read permissions (`GetItem`, `Query`) are needed for the health check subcommand and for the poller to reconstruct state after restarts (rolling averages, off-peak start snapshots).

**3. LambdaExecutionRole** (used by the Lambda function):

```yaml
Policies:
  - Statement:
      - Effect: Allow
        Action:
          - dynamodb:GetItem
          - dynamodb:Query
        Resource:
          - !GetAtt ReadingsTable.Arn
          - !GetAtt DailyEnergyTable.Arn
          - !GetAtt DailyPowerTable.Arn
          - !GetAtt SystemTable.Arn
          - !GetAtt OffpeakTable.Arn
      - Effect: Allow
        Action:
          - ssm:GetParameter
        Resource:
          - !Sub "arn:aws:ssm:${AWS::Region}:${AWS::AccountId}:parameter${SSMPathPrefix}/*"
      - Effect: Allow
        Action:
          - logs:CreateLogStream
          - logs:PutLogEvents
        Resource:
          - !GetAtt ApiLogGroup.Arn
```

---

## Data Models

This section covers the CloudFormation resource models. Application-level data models (Go structs, API responses) are out of scope for this infrastructure spec — they will be defined in the poller and Lambda specs.

### DynamoDB Key Schemas

All queries from the Lambda use a known partition key (serial number) with sort key conditions:

- **`flux-readings`**: Query by `sysSn`, sort by `timestamp` descending, limit N. Used for day detail charts and rolling averages.
- **`flux-daily-energy`**: Query by `sysSn`, sort key range on `date`. Used for history bar chart.
- **`flux-daily-power`**: Query by `sysSn`, sort by `uploadTime`. Used for 24h low and off-peak calculations.
- **`flux-system`**: GetItem by `sysSn`. Used for battery capacity and system info.
- **`flux-offpeak`**: GetItem by `sysSn` + today's `date`. Used for off-peak stats on the dashboard.

No table requires a GSI because all access patterns use the partition key.

### SSM Parameter Schema

All parameters share the prefix `/flux/` (configurable via `SSMPathPrefix`):

```
/flux/
├── app-id          (String)     — AlphaESS developer App ID
├── app-secret      (SecureString) — AlphaESS developer App Secret
├── serial          (String)     — System serial number
├── offpeak-start   (String)     — "11:00"
├── offpeak-end     (String)     — "14:00"
└── api-token       (SecureString) — Bearer token for Lambda auth
```

---

## Error Handling

Infrastructure-level error handling focuses on what happens when resources fail or become unavailable.

### ECS Task Failures

| Failure Mode | Detection | Recovery |
|-------------|-----------|----------|
| Container crash (process exits) | ECS detects task stop | ECS service auto-launches replacement task |
| Container hang (process alive but not polling) | Health check fails 3 times | ECS marks task unhealthy, service replaces it |
| AZ outage | Task stops in affected AZ | ECS launches replacement in other AZ (both subnets listed) |
| AlphaESS API unreachable | Poller logs errors | Poller retries on next schedule tick; existing data remains in DynamoDB |
| DynamoDB throttling | SDK returns error | AWS SDK default retry with exponential backoff handles this |

### Lambda Failures

| Failure Mode | Detection | Recovery |
|-------------|-----------|----------|
| Cold start timeout (>10s) | Lambda times out | Client retries; unlikely with 128 MB ARM64 Go binary (~100ms cold start) |
| DynamoDB read failure | Lambda returns 500 | Client shows error; Lambda logs the failure |
| Invalid bearer token | Lambda returns 401 | Client prompts for token re-entry |
| Function URL unreachable | HTTPS connection fails | Client shows staleness indicator with last known data |

### CloudFormation Deployment Failures

| Failure Mode | Detection | Recovery |
|-------------|-----------|----------|
| Missing SecureString params | ECS task fails to start (Secrets injection fails) | Deploy docs list params that must exist first |
| Invalid container image URI | ECS task fails to pull image | Check `ContainerImageUri` parameter |
| Lambda zip missing | `cloudformation package` fails | Build the Lambda binary before running `package` |
| Stack rollback | CloudFormation events | DynamoDB tables have Retain policy, so data survives |

---

## Deployment Procedure

### Prerequisites

1. AWS CLI configured with appropriate credentials
2. An S3 bucket for Lambda artifact storage
3. Go toolchain for building the Lambda binary

### First-Time Setup

Create the SecureString parameters that the stack references but cannot create:

```bash
aws ssm put-parameter \
  --name "/flux/app-secret" \
  --type SecureString \
  --value "YOUR_ALPHA_ESS_APP_SECRET"

aws ssm put-parameter \
  --name "/flux/api-token" \
  --type SecureString \
  --value "YOUR_API_BEARER_TOKEN"
```

### Build and Deploy

```bash
# 1. Build the Lambda binary (ARM64 Linux)
GOOS=linux GOARCH=arm64 go build -o lambda/bootstrap ./cmd/api

# 2. Package (uploads Lambda zip to S3, rewrites template)
aws cloudformation package \
  --template-file infrastructure/template.yaml \
  --s3-bucket YOUR_ARTIFACT_BUCKET \
  --output-template-file infrastructure/packaged.yaml

# 3. Deploy
aws cloudformation deploy \
  --template-file infrastructure/packaged.yaml \
  --stack-name flux \
  --capabilities CAPABILITY_IAM \
  --parameter-overrides \
    ContainerImageUri=ghcr.io/owner/flux-poller:latest \
    AlphaESSAppId=YOUR_APP_ID \
    SystemSerialNumber=YOUR_SERIAL \
    OffPeakWindowStart=11:00 \
    OffPeakWindowEnd=14:00
```

### Updating

- **Lambda code change**: Rebuild binary, re-run `package` and `deploy`.
- **Container image change**: Push new image to GHCR, then either update the `ContainerImageUri` parameter or force a new ECS deployment: `aws ecs update-service --cluster flux --service flux-poller --force-new-deployment`.
- **Configuration change**: Update the SSM parameter via CLI, then force a new ECS deployment (SSM values are injected at task start) or redeploy the Lambda.
- **Infrastructure change**: Edit `template.yaml`, re-run `package` and `deploy`.

---

## Testing Strategy

### CloudFormation Template Validation

| Test | Method | Covers |
|------|--------|--------|
| Template syntax | `aws cloudformation validate-template` | YAML validity, parameter references |
| Template linting | `cfn-lint` | CloudFormation best practices, valid resource properties |
| Dry-run deployment | `aws cloudformation deploy --no-execute-changeset` | Resource creation feasibility, IAM validity |

**Acceptance criteria covered:** All of [1.1–1.7](#1.1), [2.1–2.10](#2.1), [3.1–3.7](#3.1), [4.1–4.7](#4.1), [5.1, 5.3–5.5, 5.7–5.8](#5.1), [7.1–7.6](#7.1), [8.1–8.3](#8.1)

### Integration Testing (Post-Deploy)

These tests run against the deployed stack to verify resources are correctly configured:

| Test | Verification | Covers |
|------|-------------|--------|
| VPC connectivity | Fargate task starts and reaches AlphaESS API | [1.1–1.4](#1.1), [2.3](#2.3) |
| DynamoDB Gateway endpoint | Poller writes succeed via VPC endpoint (verify no public DynamoDB traffic in VPC flow logs) | [1.5](#1.5) |
| Security group | Port scan of Fargate task's public IP returns no open ports | [2.4](#2.4) |
| ECS health check | Stop the poller process inside the container; verify ECS replaces the task within health check interval × retries | [2.11](#2.11) |
| Lambda Function URL | `curl` the Function URL with valid bearer token; verify 200 response | [4.3, 4.7](#4.3) |
| Lambda auth | `curl` without bearer token; verify 401 response | [4.4](#4.4) |
| SSM injection | Verify poller container has expected environment variables via `aws ecs execute-command` | [2.7](#2.7), [5.1–5.7](#5.1) |
| DynamoDB TTL | Write a test record to `flux-readings` with a TTL in the past; verify DynamoDB deletes it within 48 hours | [3.1](#3.1) |
| IAM least privilege | Attempt a DynamoDB write from the Lambda role; verify access denied | [6.2, 6.3](#6.2) |
| DeletionPolicy | Delete the stack; verify DynamoDB tables still exist | [3.7](#3.7) |

### Deployment Workflow Testing

| Test | Verification | Covers |
|------|-------------|--------|
| First deploy without SecureString params | Stack creation fails with clear error at ECS task start | [5.2, 5.6, 5.9](#5.2) |
| `cloudformation package` with local binary | Packaged template has S3 references for Lambda code | [4.8](#4.8), [7.4](#7.4) |
| Stack update with parameter change | Update `OffPeakWindowStart`; verify new ECS task picks up the change | [7.2](#7.2) |

---

## File Structure

```
infrastructure/
├── template.yaml          # CloudFormation template (all resources)
└── README.md              # Deployment instructions (prerequisite commands, deploy steps)

lambda/
└── bootstrap              # Compiled Go binary (built before deploy, not committed)

cmd/
├── poller/                # Poller entry point (container)
│   └── main.go
└── api/                   # Lambda entry point
    └── main.go
```

The `infrastructure/` directory contains only the CloudFormation template and deployment documentation. Go source code lives in `cmd/` (entry points) and `internal/` (shared packages) — those are the subject of separate specs (poller, API).

**`.gitignore` entries:**
- `lambda/bootstrap` — compiled binary, built before deploy
- `infrastructure/packaged.yaml` — output of `cloudformation package`, contains build-specific S3 URIs
