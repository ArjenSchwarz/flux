# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added

- VPC (`10.0.0.0/24`) with DNS support and two public subnets across availability zones (`10.0.0.0/25`, `10.0.0.128/25`)
- Internet Gateway with VPC attachment
- Route table with default route to IGW, associated with both subnets
- DynamoDB and S3 Gateway VPC endpoints attached to route table
- Security group allowing all egress and no ingress for Fargate tasks
- 5 DynamoDB tables: `flux-readings` (TTL), `flux-daily-energy`, `flux-daily-power` (TTL), `flux-system`, `flux-offpeak` — all PAY_PER_REQUEST with DeletionPolicy Retain
- `.gitignore` with entries for `lambda/bootstrap` and `infrastructure/packaged.yaml`
- CloudFormation template skeleton (`infrastructure/template.yaml`) with 6 parameters (ContainerImageUri, AlphaESSAppId, SystemSerialNumber, OffPeakWindowStart, OffPeakWindowEnd, SSMPathPrefix) and 3 outputs (FunctionUrl, EcsClusterName, EcsServiceName)
- Infrastructure spec: requirements document with 8 requirement groups and 42 acceptance criteria covering VPC, ECS Fargate, DynamoDB, Lambda, SSM, IAM, CloudFormation deployment, and CloudWatch Logs
- Infrastructure spec: design document with full architecture diagram, CloudFormation resource definitions, IAM policies, health check design, deployment procedure, and testing strategy
- Infrastructure spec: decision log with 11 documented decisions (single template, ARM64, bearer token auth, on-demand DynamoDB, manual SecureString creation, cfn package deploy, DynamoDB health check, etc.)
- Infrastructure spec: task list with 12 implementation tasks across 6 phases, dependency-ordered with requirement traceability
- Infrastructure spec: prerequisites document listing manual setup steps required before deployment
