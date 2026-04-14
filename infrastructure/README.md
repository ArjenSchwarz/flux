# Flux Infrastructure

Single CloudFormation stack for the Flux backend: ECS Fargate poller, Lambda API with Function URL, DynamoDB tables, VPC networking, and SSM configuration.

## Prerequisites

- AWS CLI configured with credentials that have permission to create IAM roles, ECS, Lambda, DynamoDB, VPC, and SSM resources
- An S3 bucket for Lambda artifact storage (used by `aws cloudformation package`)
- Go toolchain (for building the Lambda binary)
- The poller container image pushed to GHCR

## First-Time Setup

Before deploying the stack, create the SecureString SSM parameters that CloudFormation cannot manage (it only supports String and StringList types):

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

These parameters must exist before the stack is deployed. The ECS task will fail to start if `/flux/app-secret` is missing, and the Lambda will not authenticate requests without `/flux/api-token`.

## Build and Deploy

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

After deployment, retrieve the Lambda Function URL from the stack outputs:

```bash
aws cloudformation describe-stacks \
  --stack-name flux \
  --query "Stacks[0].Outputs[?OutputKey=='FunctionUrl'].OutputValue" \
  --output text
```

## Updating

### Lambda Code

Rebuild the binary, re-run `package` and `deploy`:

```bash
GOOS=linux GOARCH=arm64 go build -o lambda/bootstrap ./cmd/api
aws cloudformation package \
  --template-file infrastructure/template.yaml \
  --s3-bucket YOUR_ARTIFACT_BUCKET \
  --output-template-file infrastructure/packaged.yaml
aws cloudformation deploy \
  --template-file infrastructure/packaged.yaml \
  --stack-name flux \
  --capabilities CAPABILITY_IAM
```

### Container Image

Push the new image to GHCR, then either update the `ContainerImageUri` parameter via `deploy` or force a new ECS deployment:

```bash
aws ecs update-service \
  --cluster flux \
  --service flux-poller \
  --force-new-deployment
```

### Configuration

Update SSM parameters directly, then restart the relevant service to pick up the new values:

```bash
# Update a String parameter (e.g., off-peak window)
aws ssm put-parameter \
  --name "/flux/offpeak-start" \
  --type String \
  --value "10:00" \
  --overwrite

# Update a SecureString parameter (e.g., API token)
aws ssm put-parameter \
  --name "/flux/api-token" \
  --type SecureString \
  --value "NEW_TOKEN" \
  --overwrite

# Force ECS to restart with new SSM values (injected at task start)
aws ecs update-service \
  --cluster flux \
  --service flux-poller \
  --force-new-deployment

# For Lambda, deploy a no-op update or update the function configuration
# to trigger a cold start that re-reads SSM
```

### Infrastructure

Edit `template.yaml`, then re-run `package` and `deploy`:

```bash
aws cloudformation package \
  --template-file infrastructure/template.yaml \
  --s3-bucket YOUR_ARTIFACT_BUCKET \
  --output-template-file infrastructure/packaged.yaml
aws cloudformation deploy \
  --template-file infrastructure/packaged.yaml \
  --stack-name flux \
  --capabilities CAPABILITY_IAM \
  --parameter-overrides \
    ContainerImageUri=ghcr.io/owner/flux-poller:latest \
    AlphaESSAppId=YOUR_APP_ID \
    SystemSerialNumber=YOUR_SERIAL
```

## Stack Outputs

| Output | Description |
|--------|-------------|
| `FunctionUrl` | Lambda Function URL for the API |
| `EcsClusterName` | ECS cluster name |
| `EcsServiceName` | ECS service name |

## Local Dry-Run

To test the poller container locally without writing to DynamoDB:

```bash
# Build the container first
make docker-build

# Run in dry-run mode (requires AlphaESS credentials as env vars)
export ALPHA_APP_ID=your_app_id
export ALPHA_APP_SECRET=your_app_secret
export SYSTEM_SERIAL=your_serial_number
make docker-dry-run
```

This starts the poller with `DRY_RUN=true`, which disables DynamoDB writes and logs API responses instead. Useful for verifying API connectivity and response parsing.

## Notes

- DynamoDB tables use `DeletionPolicy: Retain` -- data survives stack deletion
- Log groups use `DeletionPolicy: Delete` -- logs are cleaned up with the stack
- `infrastructure/packaged.yaml` is gitignored (contains build-specific S3 URIs)
- `lambda/bootstrap` is gitignored (compiled binary built before deploy)
