# Infrastructure Implementation Explanation

## Beginner Level

### What This Does

This branch creates all the cloud infrastructure needed to run the Flux backend on AWS. Flux is a personal app (2 users) that monitors a home solar/battery system via the AlphaESS API.

There are two main pieces:

1. **A poller** — a small program that runs continuously in the cloud, checking the AlphaESS API every few seconds for new solar/battery readings, and saving them to a database.
2. **An API** — a function that the Flux iOS app calls to get dashboard data. It reads from the same database the poller writes to.

Think of the poller as a robot that checks your solar panel readings and writes them in a notebook. The API is the librarian that reads from the notebook when the app asks for data.

Everything is defined in a single configuration file (`infrastructure/template.yaml`) that tells AWS exactly what to create. This "infrastructure as code" approach means the entire backend can be created or torn down with a single command.

### Why It Matters

Without this infrastructure, the iOS app has nothing to talk to. The poller collects data in the background so the app can show real-time and historical solar/battery stats. Defining it as code means it's reproducible, version-controlled, and documented.

### Key Concepts

- **CloudFormation** — AWS's service for defining infrastructure in YAML files. You describe what you want, and AWS creates it.
- **VPC (Virtual Private Cloud)** — A private network in AWS where your resources run. Like having your own isolated section of a data centre.
- **ECS Fargate** — A way to run containers (packaged applications) without managing servers. AWS handles the underlying machine.
- **Lambda** — A function that runs only when called. You pay per invocation rather than for a running server.
- **DynamoDB** — A database service. Data is stored in tables with simple key-value access patterns.
- **SSM Parameter Store** — A place to store configuration values (like API keys) separately from your code.
- **IAM** — Identity and Access Management. Controls what each component is allowed to do (e.g., the API can read from the database but not write to it).

---

## Intermediate Level

### Changes Overview

8 commits add a complete CloudFormation stack defined in `infrastructure/template.yaml` (504 lines), plus spec documents and deployment documentation. The stack contains:

- **Networking**: VPC (10.0.0.0/24), 2 public subnets across AZs, IGW, route table, DynamoDB and S3 gateway VPC endpoints, security group (egress-only)
- **Storage**: 5 DynamoDB tables (readings, daily-energy, daily-power, system, offpeak) — all PAY_PER_REQUEST with Retain deletion policy
- **Compute**: ECS Fargate service (ARM64, 0.25 vCPU, 512 MB) running the poller container, Lambda function (provided.al2023, ARM64, 128 MB) serving the API via Function URL
- **Configuration**: 4 SSM String parameters managed by the stack; 2 SecureString parameters created manually before deployment
- **IAM**: 3 least-privilege roles — TaskExecutionRole (SSM read, logs), TaskRole (DynamoDB read/write), LambdaExecutionRole (DynamoDB read, SSM read, logs)

### Implementation Approach

**Single-template architecture**: All resources in one file. No nested stacks, no CDK/Pulumi abstraction layer. For a personal app with ~25 resources, this keeps deployment simple (two commands: `package` + `deploy`).

**SSM for secrets, parameters for config**: Sensitive values (app secret, API token) live in SSM SecureString parameters created outside the stack. Non-sensitive config (app ID, serial number, off-peak times) are template parameters that flow into SSM String parameters, then get injected into the ECS task via the `Secrets` property at container startup.

**Lambda code via `cloudformation package`**: The Lambda binary is built locally, and `aws cloudformation package` handles zipping and uploading to S3. The template references `Code: ./lambda/` which gets rewritten to an S3 URI in the packaged template.

**Health check design**: The poller binary doubles as a health checker via a `healthcheck` subcommand. It queries DynamoDB for the most recent reading and checks recency. This verifies end-to-end data flow (not just process liveness), so ECS can detect and replace hung containers.

### Trade-offs

- **Public subnets, no NAT Gateway**: Saves ~$30/month. The poller gets a public IP directly. Acceptable because it has no inbound ports — the security group blocks all ingress.
- **Function URL with auth type NONE**: No API Gateway, WAF, or rate limiting. The Lambda validates a bearer token itself. Sufficient for 2 users; avoids $3.50+/month for API Gateway.
- **On-demand DynamoDB**: Slightly more expensive per request than provisioned, but at ~260K writes/month the difference is cents. Eliminates capacity planning entirely.
- **ARM64 everywhere**: Both Fargate and Lambda use Graviton/ARM64. Single build target, ~20% cheaper Lambda pricing, faster Go cold starts.

---

## Expert Level

### Technical Deep Dive

**IAM policy scoping**: All three roles are ARN-scoped. The TaskExecutionRole uses `ssm:GetParameters` (plural) because ECS batch-fetches secrets at task start. The LambdaExecutionRole uses `ssm:GetParameter` (singular) because the Lambda reads one parameter at a time during cold start. Log permissions use `!Sub "${LogGroup.Arn}:*"` to cover log stream ARNs (the `:*` suffix is required — `!GetAtt LogGroup.Arn` alone would cause Access Denied on `CreateLogStream`/`PutLogEvents`).

**VPC endpoint strategy**: The DynamoDB gateway endpoint keeps all table traffic off the public internet — it routes through the VPC endpoint via the route table. The S3 endpoint is included for future use (requirement 1.6) at zero cost (gateway endpoints are free). Lambda runs outside the VPC entirely, accessing DynamoDB via the public endpoint, which is correct for a function with no VPC resources to reach.

**ECS health check timing**: Interval 60s, timeout 10s, retries 3, start period 120s. The start period covers container pull + initialization. After that, a hung container is detected within ~4 minutes (3 × 60s intervals) and replaced. The health check queries the `flux-readings` table for the most recent timestamp — this reuses the task role's DynamoDB read permission.

**DynamoDB TTL**: Two tables have TTL enabled (`flux-readings` at 30 days, `flux-daily-power` at 7 days). TTL values are computed at write time by the poller. DynamoDB deletes expired items asynchronously (typically within 48 hours of expiry). No GSIs are needed — all access patterns use partition key (serial number) with sort key conditions.

**Lambda logging**: Uses `LoggingConfig.LogGroup` rather than the traditional approach of naming the log group `/aws/lambda/{function-name}`. This gives explicit control over the log group lifecycle (14-day retention, Delete policy) without relying on Lambda's auto-created log group.

### Architecture Impact

The poller and Lambda communicate exclusively through DynamoDB — no direct connection. This decoupling means either can be updated independently. The poller can restart, crash, or be replaced without affecting API availability (the Lambda serves whatever data is in DynamoDB).

The template hardcodes resource names (table names, cluster name, service name, function name). This is intentional for a single-deployment personal app but means you cannot deploy two stacks in the same account/region. If that becomes necessary, names would need to be parameterised with `!Sub "${AWS::StackName}-*"` patterns.

### Potential Issues

- **Cold start after SSM parameter update**: If you update an SSM parameter directly (e.g., rotate the API token), the Lambda continues using the cached value until its execution environment is recycled. A configuration change requires either a code deploy (forces new environments) or manually updating the function configuration to trigger recycling.
- **Single-task poller**: DesiredCount is 1. During deployments or AZ failures, there's a brief gap in polling while ECS launches a replacement. The app handles this via staleness indicators.
- **No alarms**: If the poller dies and ECS can't replace it (e.g., Fargate capacity issue), there's no notification. The only signal is stale data in the app. This is a documented decision (Decision 3) appropriate for V1 of a personal app.

---

## Completeness Assessment

### Fully Implemented

All 42 acceptance criteria across 8 requirement groups are implemented in the template:
- Network infrastructure (1.1–1.7)
- ECS Fargate poller (2.1–2.11)
- DynamoDB tables (3.1–3.7)
- Lambda function and Function URL (4.1–4.8)
- SSM parameters (5.1–5.9)
- IAM security (6.1–6.5)
- CloudFormation deployment (7.1–7.6)
- CloudWatch logs (8.1–8.3)

### Not Applicable to This Spec

- CI/CD pipeline (deferred to separate spec, Decision 2)
- CloudWatch alarms and dashboards (deferred, Decision 3)
- Application code (poller and Lambda Go code are separate specs)
