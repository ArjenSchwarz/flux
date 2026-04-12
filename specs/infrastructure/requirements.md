# Infrastructure Requirements

## Introduction

The Flux infrastructure provides the AWS backend that powers the Flux iOS app. It consists of a continuously-running poller that collects data from the AlphaESS API, stores it in DynamoDB, and a Lambda function that serves pre-computed stats to the app via a Function URL.

The infrastructure is deployed as a single CloudFormation stack covering networking (VPC), compute (ECS Fargate + Lambda), storage (DynamoDB), configuration (SSM Parameter Store), and access control (IAM). It targets a single AWS region and serves a two-user personal app.

---

## Requirements

### 1. Network Infrastructure

**User Story:** As a developer, I want a VPC with public subnets across two availability zones, so that the Fargate task has internet access for AlphaESS API calls and can failover between AZs.

**Acceptance Criteria:**

1. <a name="1.1"></a>The stack SHALL create a VPC with a /24 CIDR block (sufficient for 2 public subnets with minimal hosts)  
2. <a name="1.2"></a>The stack SHALL create public subnets in exactly 2 availability zones  
3. <a name="1.3"></a>The stack SHALL create an Internet Gateway attached to the VPC  
4. <a name="1.4"></a>The stack SHALL create a route table with a default route (0.0.0.0/0) to the Internet Gateway, associated with both public subnets  
5. <a name="1.5"></a>The stack SHALL create a DynamoDB Gateway VPC endpoint attached to the route table, so that DynamoDB traffic stays off the public internet  
6. <a name="1.6"></a>The stack SHALL create an S3 Gateway VPC endpoint attached to the route table, for future use by services that access S3  
7. <a name="1.7"></a>The stack SHALL NOT create any private subnets or NAT Gateways  

### 2. ECS Fargate Poller Service

**User Story:** As a developer, I want a long-running Fargate task that polls the AlphaESS API on multiple schedules, so that real-time and periodic data is continuously written to DynamoDB without client-side polling.

**Acceptance Criteria:**

1. <a name="2.1"></a>The stack SHALL create an ECS cluster with a single Fargate service running 1 desired task  
2. <a name="2.2"></a>The Fargate task SHALL use ARM64 (Graviton) architecture with 0.25 vCPU and 0.5 GB memory  
3. <a name="2.3"></a>The Fargate task SHALL run in a public subnet with `assignPublicIp: ENABLED`  
4. <a name="2.4"></a>The stack SHALL create a security group that allows all outbound traffic and no inbound traffic  
5. <a name="2.5"></a>The ECS service SHALL list both AZ subnets in its network configuration so that a replacement task can launch in either AZ  
6. <a name="2.6"></a>The task definition SHALL reference a public GHCR container image (no pull credentials required)  
7. <a name="2.7"></a>The task definition SHALL pass AlphaESS credentials, system serial number, and off-peak window configuration to the container via environment variables sourced from SSM Parameter Store  
8. <a name="2.8"></a>The stack SHALL create a task execution IAM role with permissions to read SSM parameters and write CloudWatch Logs  
9. <a name="2.9"></a>The stack SHALL create a task IAM role with permissions to read and write to all 5 DynamoDB tables  
10. <a name="2.10"></a>The ECS service SHALL configure CloudWatch Logs with the `awslogs` log driver using a dedicated log group  
11. <a name="2.11"></a>The task definition SHALL include a health check command that verifies the poller is actively running, so that ECS can detect and replace hung containers  

### 3. DynamoDB Tables

**User Story:** As a developer, I want 5 DynamoDB tables with appropriate key schemas and TTL settings, so that the poller can write structured data and the Lambda can query it efficiently.

**Acceptance Criteria:**

1. <a name="3.1"></a>The stack SHALL create a `flux-readings` table with partition key `sysSn` (String) and sort key `timestamp` (Number), with TTL enabled on a `ttl` attribute set to 30 days from write time  
2. <a name="3.2"></a>The stack SHALL create a `flux-daily-energy` table with partition key `sysSn` (String) and sort key `date` (String), with no TTL  
3. <a name="3.3"></a>The stack SHALL create a `flux-daily-power` table with partition key `sysSn` (String) and sort key `uploadTime` (String), with TTL enabled on a `ttl` attribute set to 7 days from write time  
4. <a name="3.4"></a>The stack SHALL create a `flux-system` table with partition key `sysSn` (String) and no sort key, with no TTL  
5. <a name="3.5"></a>The stack SHALL create a `flux-offpeak` table with partition key `sysSn` (String) and sort key `date` (String), with no TTL  
6. <a name="3.6"></a>All tables SHALL use on-demand (PAY_PER_REQUEST) billing mode  
7. <a name="3.7"></a>All tables SHALL use the DeletionPolicy `Retain` to prevent accidental data loss on stack deletion  

### 4. Lambda Function and Function URL

**User Story:** As a developer, I want a Lambda function with a Function URL that serves the app API, so that the iOS app can fetch dashboard, history, and day detail data without needing API Gateway.

**Acceptance Criteria:**

1. <a name="4.1"></a>The stack SHALL create a Lambda function using the `provided.al2023` runtime with ARM64 architecture  
2. <a name="4.2"></a>The Lambda function SHALL have an IAM role with read permissions on all 5 DynamoDB tables  
3. <a name="4.3"></a>The stack SHALL create a Function URL with auth type `NONE`  
4. <a name="4.4"></a>The Lambda function SHALL receive the SSM parameter paths for the API bearer token and system serial number, plus off-peak window times, as environment variables. The Lambda SHALL read the actual secret values from SSM at cold start and cache them in memory  
5. <a name="4.5"></a>The Lambda function SHALL have a memory size of 128 MB and a timeout of 10 seconds  
6. <a name="4.6"></a>The Lambda function SHALL use a dedicated CloudWatch Logs log group  
7. <a name="4.7"></a>The stack SHALL output the Function URL in the stack outputs  
8. <a name="4.8"></a>The template SHALL use `aws cloudformation package` to upload a local Go binary zip to S3 and reference the resulting S3 location in the Lambda function's `Code` property  

### 5. SSM Parameter Store Configuration

**User Story:** As a developer, I want server-side configuration stored in SSM Parameter Store, so that secrets and settings are managed outside the codebase and can be updated without redeployment.

**Acceptance Criteria:**

1. <a name="5.1"></a>The stack SHALL create an SSM parameter for the AlphaESS App ID (String type)  
2. <a name="5.2"></a>The AlphaESS App Secret SHALL be stored as a SecureString SSM parameter created manually outside the stack (CloudFormation does not support SecureString creation)  
3. <a name="5.3"></a>The stack SHALL create an SSM parameter for the system serial number (String type)  
4. <a name="5.4"></a>The stack SHALL create an SSM parameter for the off-peak window start time (String type, e.g. "11:00")  
5. <a name="5.5"></a>The stack SHALL create an SSM parameter for the off-peak window end time (String type, e.g. "14:00")  
6. <a name="5.6"></a>The API bearer token SHALL be stored as a SecureString SSM parameter created manually outside the stack  
7. <a name="5.7"></a>All SSM parameters SHALL use a consistent path prefix (`/flux/`)  
8. <a name="5.8"></a>The stack SHALL accept the SSM path prefix as a parameter and reference it in IAM policies and task/function environment variable configuration  
9. <a name="5.9"></a>The deployment documentation SHALL include CLI commands for creating the SecureString parameters that must exist before stack deployment  

### 6. IAM Security

**User Story:** As a developer, I want IAM roles that follow least-privilege principles, so that each component only has access to the resources it needs.

**Acceptance Criteria:**

1. <a name="6.1"></a>The ECS task execution role SHALL only have permissions to read SSM parameters under the `/flux/` prefix and write CloudWatch Logs (no ECR permissions needed — GHCR images are pulled directly over HTTPS)  
2. <a name="6.2"></a>The ECS task role SHALL have permissions to read (`GetItem`, `Query`) and write (`PutItem`, `UpdateItem`, `BatchWriteItem`) to the 5 DynamoDB tables  
3. <a name="6.3"></a>The Lambda execution role SHALL only have permissions to read (`GetItem`, `Query`) from the 5 DynamoDB tables, read SSM parameters under the `/flux/` prefix, and write CloudWatch Logs  
4. <a name="6.4"></a>All IAM policies SHALL reference resources by ARN rather than using wildcards for service names  
5. <a name="6.5"></a>The stack SHALL NOT create any IAM users or access keys  

### 7. CloudFormation Deployment

**User Story:** As a developer, I want the entire infrastructure defined in a single CloudFormation template, so that I can deploy and tear down the backend with a single command.

**Acceptance Criteria:**

1. <a name="7.1"></a>The infrastructure SHALL be defined in a single CloudFormation YAML template  
2. <a name="7.2"></a>The template SHALL use parameters for configurable values (off-peak window times, system serial number, container image URI, SSM path prefix)  
3. <a name="7.3"></a>The template SHALL output the Lambda Function URL, ECS cluster name, and ECS service name  
4. <a name="7.4"></a>The template SHALL be deployable via `aws cloudformation package` (to upload the Lambda zip) followed by `aws cloudformation deploy` (to create/update the stack)  
5. <a name="7.5"></a>The template SHALL target a single AWS region (parameterised region selection is not required)  
6. <a name="7.6"></a>The deployment SHALL require an existing S3 bucket (specified as a parameter to `package`) for Lambda code artifact storage  

### 8. CloudWatch Logs

**User Story:** As a developer, I want CloudWatch log groups for both the Fargate task and the Lambda function, so that I can debug issues when they arise.

**Acceptance Criteria:**

1. <a name="8.1"></a>The stack SHALL create a log group for the ECS Fargate task with a retention period of 14 days  
2. <a name="8.2"></a>The stack SHALL create a log group for the Lambda function with a retention period of 14 days  
3. <a name="8.3"></a>Both log groups SHALL use the DeletionPolicy `Delete` (logs are ephemeral and should not persist after stack teardown)  
