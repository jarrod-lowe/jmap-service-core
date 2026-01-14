# JMAP Service Core

AWS serverless JMAP (JSON Meta Application Protocol) email server implementation.

## Current Status

**Phase 1 Complete**: Infrastructure pipeline validation

- ✅ Go Lambda build pipeline (ARM64)
- ✅ Terraform multi-environment setup (test, prod)
- ✅ Observability with CloudWatch + X-Ray
- ✅ Dummy hello-world Lambda deployed

**Next**: Implement API Gateway, DynamoDB, S3, and JMAP protocol methods

## Quick Start

### Prerequisites

- Go 1.23+
- Terraform 1.0+
- AWS CLI configured with appropriate profile

### Build and Deploy

```bash
# Write Lambda code (cmd/hello-world/main.go)

# Initialize Go dependencies (Makefile handles go.mod and go get)
make deps

# Build and deploy to test environment
make build ENV=test
make package ENV=test
make init ENV=test
make plan ENV=test
make apply ENV=test

# Get outputs
make outputs ENV=test
```

### Available Make Targets

- `make help` - Display all available targets
- `make deps` - Initialize go.mod and fetch dependencies
- `make build ENV=<env>` - Compile Go Lambda (linux/arm64)
- `make package ENV=<env>` - Create Lambda deployment zip
- `make test` - Run Go unit tests
- `make lint` - Run golangci-lint (if installed)
- `make init ENV=<env>` - Initialize Terraform
- `make plan ENV=<env>` - Create Terraform plan
- `make apply ENV=<env>` - Deploy infrastructure
- `make outputs ENV=<env>` - Show deployment outputs
- `make clean ENV=<env>` - Clean Terraform files
- `make clean-all ENV=<env>` - Clean everything

### Project Structure

```text
cmd/hello-world/        - Lambda entry points
terraform/modules/      - Reusable Terraform modules
terraform/environments/ - Environment-specific configs (test, prod)
build/                  - Compiled artifacts (gitignored)
docs/                   - Documentation
```

## Documentation

- [DESIGN.md](DESIGN.md) - Overall architecture and implementation plans
- [CLAUDE.md](CLAUDE.md) - Claude Code guidance for this repository
- [docs/opentelemetry-configuration.md](docs/opentelemetry-configuration.md) - OpenTelemetry, ADOT, and observability setup
