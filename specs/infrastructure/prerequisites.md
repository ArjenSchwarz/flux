# Prerequisites for Infrastructure

These tasks must be completed by the user before or during implementation.

## Before Starting

- [ ] AWS CLI installed and configured with credentials that have permissions to create CloudFormation stacks, VPCs, ECS clusters, DynamoDB tables, Lambda functions, IAM roles, and SSM parameters
- [ ] An S3 bucket exists in the target AWS region for Lambda code artifact storage (used by `aws cloudformation package`)
- [ ] Go toolchain installed (for building the Lambda binary during deployment)

## Before First Deployment

- [ ] Create SSM SecureString parameter `/flux/app-secret` with the AlphaESS App Secret value:
  ```bash
  aws ssm put-parameter --name "/flux/app-secret" --type SecureString --value "YOUR_APP_SECRET"
  ```
- [ ] Create SSM SecureString parameter `/flux/api-token` with the bearer token for API authentication:
  ```bash
  aws ssm put-parameter --name "/flux/api-token" --type SecureString --value "YOUR_API_TOKEN"
  ```
- [ ] Have the following values ready for stack parameters: AlphaESS App ID, system serial number, GHCR container image URI
