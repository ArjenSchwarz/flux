# Decision Log: Infrastructure

## Decision 1: Single CloudFormation Template

**Date**: 2026-04-12
**Status**: accepted

### Context

The infrastructure could be structured as a single monolithic template or as nested stacks (network, storage, compute). The project is small (single VPC, one Fargate service, one Lambda, 5 tables) and serves 2 users.

### Decision

Use a single CloudFormation YAML template for all resources.

### Rationale

The infrastructure is small enough that splitting into nested stacks adds complexity without meaningful benefit. A single template is easier to reason about, deploy, and debug. Nested stacks make sense when multiple teams manage different layers or when stacks are reused — neither applies here.

### Alternatives Considered

- **Nested stacks (network/storage/compute)**: More modular — Rejected because the project is too small to benefit; adds S3 bucket management for nested template storage and cross-stack reference complexity.
- **CDK or Pulumi**: Infrastructure-as-code with type safety — Rejected because it adds a dependency and build step for a template that will rarely change.

### Consequences

**Positive:**
- Single file to read, deploy, and version
- No S3 bucket needed for template storage
- Simpler debugging (one stack, one set of events)

**Negative:**
- Template will be long (~400-600 lines)
- Cannot independently update network vs. compute layers

---

## Decision 2: CI/CD as Separate Spec

**Date**: 2026-04-12
**Status**: accepted

### Context

The infrastructure spec could include the GitHub Actions workflow for building and pushing the container image to GHCR, or treat CI/CD as a separate concern.

### Decision

Exclude CI/CD (GitHub Actions) from this spec. It will get its own spec.

### Rationale

CI/CD is a distinct concern with its own requirements (build triggers, image tagging, testing steps). Keeping it separate allows the infrastructure spec to focus purely on AWS resources and deployment. The infrastructure template references the container image URI as a parameter — it doesn't need to know how the image is built.

### Alternatives Considered

- **Include CI/CD in this spec**: Single spec covers everything — Rejected because it mixes deployment pipeline concerns with infrastructure resource definitions.

### Consequences

**Positive:**
- Cleaner separation of concerns
- Infrastructure spec stays focused
- CI/CD can be specced and implemented independently

**Negative:**
- Manual image push needed until CI/CD spec is implemented
- Two specs to coordinate (image URI parameter)

---

## Decision 3: Minimal Observability for V1

**Date**: 2026-04-12
**Status**: accepted

### Context

The backend could ship with varying levels of observability: just logs, logs + alarms, or a full dashboard with custom metrics.

### Decision

V1 includes CloudWatch Logs only — no alarms, custom metrics, or dashboards.

### Rationale

For a 2-user personal app, CloudWatch Logs provide sufficient debugging capability. Alarms and dashboards add CloudFormation complexity and ongoing maintenance for a system that will be monitored casually. Can be added later if needed.

### Alternatives Considered

- **Basic alarms (task failures, Lambda errors)**: Simple alerting — Deferred because the app will show staleness indicators if the backend is down, serving as an implicit alert.
- **Full observability (structured logging, metrics, dashboard)**: Production-grade monitoring — Rejected as over-engineered for the use case.

### Consequences

**Positive:**
- Simpler template
- No alarm configuration or SNS topics needed
- Logs are sufficient for debugging a personal app

**Negative:**
- No proactive alerting if the poller stops
- Must manually check CloudWatch if something seems wrong

---

## Decision 4: ARM64 for Both Fargate and Lambda

**Date**: 2026-04-12
**Status**: accepted

### Context

The Fargate task uses ARM64/Graviton per the plan. The Lambda could use ARM64 or x86_64.

### Decision

Use ARM64 architecture for both the Fargate task and the Lambda function.

### Rationale

Consistency between compute environments simplifies the build process (single architecture target). ARM64 Lambda has faster cold starts for Go, is ~20% cheaper, and aligns with the Fargate choice.

### Alternatives Considered

- **x86_64 for Lambda**: Default architecture — Rejected because ARM64 is cheaper, faster for Go, and avoids maintaining two build targets.

### Consequences

**Positive:**
- Single architecture target for Go builds
- Lower Lambda cost
- Faster Go cold starts on ARM64

**Negative:**
- None significant — Go cross-compilation to ARM64 is trivial

---

## Decision 5: Bearer Token Authentication Without Additional Protection

**Date**: 2026-04-12
**Status**: accepted

### Context

The Lambda Function URL is publicly accessible (auth type NONE). The app authenticates with a shared secret in the Authorization header. Additional protection (WAF, throttling) could be added.

### Decision

Use a simple bearer token in the Authorization header. No WAF, rate limiting, or additional protection layers.

### Rationale

The app serves 2 users. A bearer token is sufficient to prevent casual abuse. WAF adds cost (~$5/month minimum) and complexity. Lambda's built-in concurrency limits provide implicit throttling. The Function URL has no meaningful attack surface beyond token brute-forcing, which is impractical against a 256-bit token.

### Alternatives Considered

- **Bearer + reserved concurrency throttle**: Basic rate limiting — Rejected because it could accidentally block legitimate requests during rapid refreshes.
- **IAM auth on Function URL**: AWS-native auth — Rejected because it requires SigV4 signing in the iOS app, adding significant client complexity.

### Consequences

**Positive:**
- Simple implementation on both server and client
- No additional AWS resources or cost
- Easy to rotate token via SSM parameter update

**Negative:**
- No rate limiting (mitigated by token secrecy and 2-user scope)
- Token in transit protected only by TLS (standard for bearer auth)

---

## Decision 6: On-Demand DynamoDB Billing

**Date**: 2026-04-12
**Status**: accepted

### Context

DynamoDB supports on-demand (pay-per-request) and provisioned capacity billing modes.

### Decision

Use on-demand billing for all tables. No consideration of provisioned capacity for V1.

### Rationale

The write volume is predictable but low (~260K writes/month ≈ $0.32/month on-demand). On-demand eliminates capacity planning and auto-scaling configuration. The cost difference versus provisioned is negligible at this scale.

### Alternatives Considered

- **Provisioned capacity**: Lower per-request cost at scale — Rejected because the cost saving is cents per month and adds auto-scaling configuration complexity.

### Consequences

**Positive:**
- No capacity planning needed
- No auto-scaling configuration
- Handles any burst without throttling

**Negative:**
- Slightly higher per-request cost (irrelevant at this scale)

---

## Decision 7: SecureString Parameters Created Manually Outside CloudFormation

**Date**: 2026-04-12
**Status**: accepted

### Context

The AlphaESS App Secret and API bearer token need to be stored securely in SSM Parameter Store. CloudFormation's `AWS::SSM::Parameter` resource only supports `String` and `StringList` types — it cannot create `SecureString` parameters.

### Decision

Create SecureString parameters manually via the AWS CLI before deploying the stack. The stack references these parameters by path but does not create them.

### Rationale

This is a CloudFormation limitation, not a design choice. The only alternatives involve different secret storage mechanisms entirely. Manual creation is acceptable for a 2-user personal project with a small number of secrets (2 parameters).

### Alternatives Considered

- **Secrets Manager**: CloudFormation supports it natively — Rejected because it costs $0.40/secret/month and is overkill for 2 static secrets in a personal app.
- **Plain String SSM for secrets**: Store secrets as plaintext String type — Rejected because it exposes secrets in CloudFormation console and API responses.

### Consequences

**Positive:**
- Secrets are properly encrypted (SecureString uses KMS)
- No additional AWS service costs
- Secrets never appear in CloudFormation template or events

**Negative:**
- Manual step required before first deployment
- Must document the CLI commands for creating these parameters

---

## Decision 8: Lambda Code Deployed via `aws cloudformation package`

**Date**: 2026-04-12
**Status**: accepted

### Context

The Lambda function needs a Go binary. Unlike the Fargate task (which uses a container image from GHCR), Lambda needs either an S3-hosted zip, inline code, or a container image.

### Decision

Use `aws cloudformation package` to automatically upload a local Go binary zip to S3, then deploy the stack with the resulting S3 reference.

### Rationale

`cloudformation package` handles the upload and template rewriting automatically. It's the standard approach for Lambda functions without a dedicated CI pipeline. The CI/CD spec (deferred) will eventually automate this, but for V1 a local deploy workflow is sufficient.

### Alternatives Considered

- **S3 bucket parameter (manual upload)**: Upload zip to S3 manually, pass bucket/key as parameters — Rejected because `cloudformation package` does this automatically with less friction.
- **Container image Lambda**: Package Lambda as a Docker image on GHCR — Rejected because it adds cold start latency, image size, and requires ECR or GHCR pull; overkill for a small Go binary.

### Consequences

**Positive:**
- Standard CloudFormation workflow
- Local build-and-deploy with two commands
- No manual S3 upload step

**Negative:**
- Requires an existing S3 bucket for artifact storage
- Local machine must have the compiled binary before deploy

---

## Decision 9: ECS Task Role Has Read and Write Access to DynamoDB

**Date**: 2026-04-12
**Status**: accepted

### Context

The poller computes rolling 15-minute averages, 24h battery low, and off-peak deltas. These calculations require reading recent data from DynamoDB (e.g., last 15 minutes of readings, start-of-window energy snapshot). Without read access, all state must be held in memory and is lost on container restart.

### Decision

Grant the ECS task role both read (`GetItem`, `Query`) and write (`PutItem`, `UpdateItem`, `BatchWriteItem`) permissions on all 5 DynamoDB tables.

### Rationale

The poller needs to survive restarts gracefully. Reading from DynamoDB allows it to reconstruct rolling averages and retrieve off-peak start snapshots without re-polling the AlphaESS API (which may not have historical data). The additional read permissions have no cost and minimal security impact since the task already has write access.

### Alternatives Considered

- **In-memory only**: Simpler, no read permissions needed — Rejected because rolling averages and 24h low calculations reset on every container restart, producing incorrect data until enough time passes.

### Consequences

**Positive:**
- Poller recovers gracefully from restarts
- Off-peak calculations work even if the container restarts mid-window
- Rolling averages are immediately correct after restart

**Negative:**
- Broader IAM permissions (read + write vs. write-only)

---

## Decision 10: ECS Container Health Check for Hung Poller Detection

**Date**: 2026-04-12
**Status**: accepted

### Context

The Fargate poller is a long-running container with no HTTP server. If it hangs (deadlock, memory leak, API timeout accumulation) but doesn't crash, ECS will keep it running indefinitely because there's no health check defined. The app would show stale data but ECS would not replace the container.

### Decision

Include a health check command in the ECS task definition that the container must satisfy to be considered healthy.

### Rationale

A hung-but-running container is worse than a crashed container because ECS won't replace it automatically. A health check (e.g., checking that the last successful poll timestamp is recent) allows ECS to detect and replace unhealthy containers.

### Alternatives Considered

- **No health check (rely on app staleness indicator)**: User notices stale data and manually intervenes — Rejected because the whole point of the backend is unattended operation.

### Consequences

**Positive:**
- Automatic recovery from hung containers
- No manual intervention needed for most failure modes

**Negative:**
- Container must implement a health check mechanism (adds a small amount of code)

---

## Decision 11: Health Check via DynamoDB Timestamp Query

**Date**: 2026-04-12
**Status**: accepted

### Context

The ECS health check needs a mechanism to detect whether the poller is actively running. The poller has no HTTP server, so a simple HTTP endpoint is not available without adding one. Three approaches were considered.

### Decision

The poller Go binary exposes a `healthcheck` subcommand that queries DynamoDB for the most recent reading timestamp and checks that it's within the last 120 seconds.

### Rationale

This approach verifies end-to-end health: the poller must be polling the AlphaESS API *and* writing to DynamoDB for the health check to pass. A heartbeat file would only verify the process is alive, not that it's actually writing data. The DynamoDB query reuses the same SDK configuration and IAM permissions the poller already has.

### Alternatives Considered

- **Heartbeat file**: Poller writes a timestamp to `/tmp/healthcheck`; health check verifies recency — Rejected because it only proves the process is alive, not that data is flowing to DynamoDB.
- **Local HTTP endpoint**: Poller exposes `/healthz` on localhost — Rejected because it adds an HTTP server dependency to a non-HTTP service and still only verifies process liveness, not data flow.

### Consequences

**Positive:**
- Verifies end-to-end data flow (poll → write → DynamoDB)
- No additional runtime dependencies (same Go binary, same SDK)
- No additional ports or files to manage

**Negative:**
- Health check incurs a small DynamoDB read cost (~260K reads/month at 60s intervals, well within free tier)
- Health check fails if DynamoDB is temporarily unreachable (which would also mean the poller can't write — correct behavior)

---
