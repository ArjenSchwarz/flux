# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added

- Configuration package (`internal/config`) with `Load()` function that reads all settings from environment variables, validates offpeak HH:MM times and timezone, collects all errors before reporting, and relaxes AWS/DynamoDB requirements in dry-run mode
- `testify` test dependency for assertions
- Go module (`github.com/ArjenSchwarz/flux`) with Go 1.25 and project directory structure (`cmd/poller/`, `internal/alphaess/`, `internal/config/`, `internal/dynamo/`, `internal/poller/`)
- Makefile with targets: build, test, fmt, vet, lint, modernize, check, docker-build, deps-tidy, deps-update
- `.dockerignore` excluding `.git/`, `specs/`, `docs/`, `infrastructure/`, `.github/`
- Poller spec: requirements document with 13 sections and 91 acceptance criteria covering Go application setup, AlphaESS API client, multi-schedule polling, DynamoDB storage, configuration, off-peak energy calculation, health check, process lifecycle, logging, Dockerfile, GitHub Actions CI, dry-run mode, and infrastructure update
- Poller spec: design document with architecture diagram, 9 component designs (entrypoint, config, AlphaESS client, Store interface with DynamoStore/LogStore, DynamoDB models, poller orchestrator, off-peak scheduler), two-context graceful shutdown pattern, error handling strategy, and testing strategy
- Poller spec: decision log with 16 decisions (log-and-skip error handling, structured logs only, off-peak SOC tracking, pgridSustained in Lambda, internal/ packages, os.Args over cobra, Store interface for dry-run, distroless base image, two-context shutdown, off-peak status field)
- Poller spec: task list with 23 tasks across 8 phases and 2 parallel streams, TDD-ordered with dependency tracking and requirement traceability
- CLAUDE.md project instructions for the flux repository

- VPC (`10.0.0.0/24`) with DNS support and two public subnets across availability zones (`10.0.0.0/25`, `10.0.0.128/25`)
- Internet Gateway with VPC attachment
- Route table with default route to IGW, associated with both subnets
- DynamoDB and S3 Gateway VPC endpoints attached to route table
- Security group allowing all egress and no ingress for Fargate tasks
- CloudWatch log groups for poller (`/flux/poller`) and API (`/flux/api`) with 14-day retention and DeletionPolicy Delete
- IAM roles: `TaskExecutionRole` (SSM read, CloudWatch Logs write), `TaskRole` (DynamoDB read/write on all 5 tables), `LambdaExecutionRole` (DynamoDB read, SSM read, CloudWatch Logs write) — all least-privilege, ARN-scoped
- 5 DynamoDB tables: `flux-readings` (TTL), `flux-daily-energy`, `flux-daily-power` (TTL), `flux-system`, `flux-offpeak` — all PAY_PER_REQUEST with DeletionPolicy Retain
- SSM parameters for app-id, serial, offpeak-start, and offpeak-end (String type, stack-managed via `SSMPathPrefix`)
- ECS cluster, Fargate task definition (ARM64, 256 CPU, 512 MB) with SSM secrets injection, health check (`/poller healthcheck`), and awslogs log driver
- ECS service (`flux-poller`) with Fargate launch type, both subnets, public IP enabled
- Lambda function (`flux-api`) with `provided.al2023` runtime, ARM64, 128 MB, 10s timeout, environment variables for SSM paths and DynamoDB table names
- Lambda Function URL (auth type NONE) with public invoke permission
- `.gitignore` with entries for `lambda/bootstrap` and `infrastructure/packaged.yaml`
- CloudFormation template skeleton (`infrastructure/template.yaml`) with 6 parameters (ContainerImageUri, AlphaESSAppId, SystemSerialNumber, OffPeakWindowStart, OffPeakWindowEnd, SSMPathPrefix) and 3 outputs (FunctionUrl, EcsClusterName, EcsServiceName)
- Infrastructure spec: requirements document with 8 requirement groups and 42 acceptance criteria covering VPC, ECS Fargate, DynamoDB, Lambda, SSM, IAM, CloudFormation deployment, and CloudWatch Logs
- Infrastructure spec: design document with full architecture diagram, CloudFormation resource definitions, IAM policies, health check design, deployment procedure, and testing strategy
- Infrastructure spec: decision log with 11 documented decisions (single template, ARM64, bearer token auth, on-demand DynamoDB, manual SecureString creation, cfn package deploy, DynamoDB health check, etc.)
- Infrastructure spec: task list with 12 implementation tasks across 6 phases, dependency-ordered with requirement traceability
- Infrastructure spec: prerequisites document listing manual setup steps required before deployment
- Deployment README (`infrastructure/README.md`) with prerequisites, SecureString setup commands, build/package/deploy workflow, and update procedures for Lambda code, container image, configuration, and infrastructure changes
- Infrastructure spec: implementation explanation at beginner, intermediate, and expert levels with completeness assessment

### Fixed

- CloudWatch Logs IAM policies now use `:*` suffix on log group ARNs for `TaskExecutionRole` and `LambdaExecutionRole`, required for `logs:CreateLogStream` and `logs:PutLogEvents` to match log stream resources
