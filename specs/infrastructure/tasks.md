---
references:
    - specs/infrastructure/requirements.md
    - specs/infrastructure/design.md
    - specs/infrastructure/decision_log.md
---
# Infrastructure Tasks

## Setup

- [x] 1. Create project scaffolding <!-- id:guku1ge -->
  - Create .gitignore with entries for lambda/bootstrap and infrastructure/packaged.yaml
  - Create infrastructure/ directory
  - Stream: 1
  - Requirements: [7.1](requirements.md#7.1)

- [x] 2. Create CloudFormation template skeleton <!-- id:guku1gf -->
  - Create infrastructure/template.yaml with AWSTemplateFormatVersion, Description
  - Add all 6 Parameters: ContainerImageUri, AlphaESSAppId, SystemSerialNumber, OffPeakWindowStart (default 11:00), OffPeakWindowEnd (default 14:00), SSMPathPrefix (default /flux)
  - Add 3 Outputs: FunctionUrl, EcsClusterName, EcsServiceName
  - Blocked-by: guku1ge (Create project scaffolding)
  - Stream: 1
  - Requirements: [7.1](requirements.md#7.1), [7.2](requirements.md#7.2), [7.3](requirements.md#7.3), [7.5](requirements.md#7.5)

## Network Resources

- [x] 3. Add VPC and subnet resources <!-- id:guku1gg -->
  - Add VPC with CIDR 10.0.0.0/24 and DNS enabled
  - Add SubnetA (10.0.0.0/25) in AZ 0 and SubnetB (10.0.0.128/25) in AZ 1
  - Add InternetGateway and VPCGatewayAttachment
  - Blocked-by: guku1gf (Create CloudFormation template skeleton)
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.7](requirements.md#1.7)

- [x] 4. Add route table, VPC endpoints, and security group <!-- id:guku1gh -->
  - Add RouteTable with default route to IGW, associate with both subnets
  - Add DynamoDB Gateway VPC endpoint attached to route table
  - Add S3 Gateway VPC endpoint attached to route table
  - Add SecurityGroup allowing all egress, no ingress
  - Blocked-by: guku1gg (Add VPC and subnet resources)
  - Stream: 1
  - Requirements: [1.4](requirements.md#1.4), [1.5](requirements.md#1.5), [1.6](requirements.md#1.6), [2.4](requirements.md#2.4)

## Storage Resources

- [x] 5. Add DynamoDB table resources <!-- id:guku1gi -->
  - Add flux-readings table: PK sysSn (S), SK timestamp (N), TTL on ttl attribute, DeletionPolicy Retain
  - Add flux-daily-energy table: PK sysSn (S), SK date (S), no TTL, DeletionPolicy Retain
  - Add flux-daily-power table: PK sysSn (S), SK uploadTime (S), TTL on ttl attribute, DeletionPolicy Retain
  - Add flux-system table: PK sysSn (S), no SK, no TTL, DeletionPolicy Retain
  - Add flux-offpeak table: PK sysSn (S), SK date (S), no TTL, DeletionPolicy Retain
  - All tables use PAY_PER_REQUEST billing mode
  - Blocked-by: guku1gf (Create CloudFormation template skeleton)
  - Stream: 1
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [3.4](requirements.md#3.4), [3.5](requirements.md#3.5), [3.6](requirements.md#3.6), [3.7](requirements.md#3.7)

## IAM and Logging

- [x] 6. Add CloudWatch log groups <!-- id:guku1gj -->
  - Add PollerLogGroup with 14-day retention and DeletionPolicy Delete
  - Add ApiLogGroup with 14-day retention and DeletionPolicy Delete
  - Blocked-by: guku1gf (Create CloudFormation template skeleton)
  - Stream: 1
  - Requirements: [8.1](requirements.md#8.1), [8.2](requirements.md#8.2), [8.3](requirements.md#8.3)

- [x] 7. Add IAM roles and policies <!-- id:guku1gk -->
  - Add TaskExecutionRole: ssm:GetParameters on /flux/*, logs:CreateLogStream and logs:PutLogEvents on PollerLogGroup
  - Add TaskRole: dynamodb:PutItem/UpdateItem/BatchWriteItem/GetItem/Query on all 5 table ARNs
  - Add LambdaExecutionRole: dynamodb:GetItem/Query on all 5 table ARNs, ssm:GetParameter on /flux/*, logs on ApiLogGroup
  - All policies reference resources by ARN, no wildcards for service names
  - No IAM users or access keys created
  - Blocked-by: guku1gi (Add DynamoDB table resources), guku1gj (Add CloudWatch log groups)
  - Stream: 1
  - Requirements: [2.8](requirements.md#2.8), [2.9](requirements.md#2.9), [6.1](requirements.md#6.1), [6.2](requirements.md#6.2), [6.3](requirements.md#6.3), [6.4](requirements.md#6.4), [6.5](requirements.md#6.5)

## Compute Resources

- [x] 8. Add SSM parameter resources <!-- id:guku1gl -->
  - Add SSM parameter for app-id (String) sourced from AlphaESSAppId parameter
  - Add SSM parameter for serial (String) sourced from SystemSerialNumber parameter
  - Add SSM parameter for offpeak-start (String) sourced from OffPeakWindowStart parameter
  - Add SSM parameter for offpeak-end (String) sourced from OffPeakWindowEnd parameter
  - All parameters use SSMPathPrefix for path prefix
  - SecureString parameters (app-secret, api-token) are NOT created by the stack
  - Blocked-by: guku1gf (Create CloudFormation template skeleton)
  - Stream: 1
  - Requirements: [5.1](requirements.md#5.1), [5.3](requirements.md#5.3), [5.4](requirements.md#5.4), [5.5](requirements.md#5.5), [5.7](requirements.md#5.7), [5.8](requirements.md#5.8)

- [x] 9. Add ECS cluster, task definition, and service <!-- id:guku1gm -->
  - Add ECS Cluster with default settings
  - Add TaskDefinition: Fargate, ARM64, 256 CPU, 512 Memory, awsvpc network mode
  - Configure container with Secrets (ALPHA_APP_ID, ALPHA_APP_SECRET, SYSTEM_SERIAL from SSM) and Environment (OFFPEAK_START, OFFPEAK_END, AWS_REGION)
  - Add health check: CMD /poller healthcheck, interval 60, timeout 10, retries 3, startPeriod 120
  - Configure awslogs log driver pointing to PollerLogGroup
  - Add ECS Service: Fargate launch type, desiredCount 1, both subnets, assignPublicIp ENABLED, security group
  - Blocked-by: guku1gh (Add route table, VPC endpoints, and security group), guku1gk (Add IAM roles and policies), guku1gl (Add SSM parameter resources)
  - Stream: 1
  - Requirements: [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.3](requirements.md#2.3), [2.5](requirements.md#2.5), [2.6](requirements.md#2.6), [2.7](requirements.md#2.7), [2.10](requirements.md#2.10), [2.11](requirements.md#2.11)

- [x] 10. Add Lambda function, Function URL, and permission <!-- id:guku1gn -->
  - Add Lambda function: provided.al2023 runtime, arm64, 128 MB memory, 10s timeout, handler bootstrap
  - Set Code property to ./lambda/ (rewritten by cloudformation package)
  - Configure environment variables: API_TOKEN_PARAM, SYSTEM_SERIAL_PARAM, OFFPEAK_START, OFFPEAK_END, all 5 TABLE_* names
  - Add Function URL with AuthType NONE
  - Add Lambda Permission allowing lambda:InvokeFunctionUrl from * with FunctionUrlAuthType NONE
  - Wire FunctionUrl output to ApiFunctionUrl.FunctionUrl
  - Blocked-by: guku1gk (Add IAM roles and policies), guku1gl (Add SSM parameter resources)
  - Stream: 1
  - Requirements: [4.1](requirements.md#4.1), [4.2](requirements.md#4.2), [4.3](requirements.md#4.3), [4.4](requirements.md#4.4), [4.5](requirements.md#4.5), [4.6](requirements.md#4.6), [4.7](requirements.md#4.7), [4.8](requirements.md#4.8)

## Validation

- [ ] 11. Validate template with cfn-lint <!-- id:guku1go -->
  - Install cfn-lint if not present (pip install cfn-lint)
  - Run cfn-lint infrastructure/template.yaml
  - Fix any errors or warnings
  - Run aws cloudformation validate-template on the template
  - Blocked-by: guku1gm (Add ECS cluster, task definition, and service), guku1gn (Add Lambda function, Function URL, and permission)
  - Stream: 1
  - Requirements: [7.1](requirements.md#7.1), [7.4](requirements.md#7.4)

## Documentation

- [ ] 12. Create deployment README <!-- id:guku1gp -->
  - Create infrastructure/README.md with prerequisites section
  - Document SecureString parameter creation commands (app-secret, api-token)
  - Document build, package, and deploy commands
  - Document update procedures for Lambda code, container image, configuration, and infrastructure changes
  - Blocked-by: guku1go (Validate template with cfn-lint)
  - Stream: 2
  - Requirements: [5.2](requirements.md#5.2), [5.6](requirements.md#5.6), [5.9](requirements.md#5.9), [7.4](requirements.md#7.4), [7.6](requirements.md#7.6)
