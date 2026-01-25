.PHONY: help deps build build-all package package-all test test-go test-cloudfront integration-test jmap-client-test lint init plan show-plan apply plan-destroy destroy clean clean-all fmt validate outputs restore-tfvars help-tfvars invalidate-cache get-token

# Environment selection (test or prod)
ENV ?= test

# Validate environment
ifeq ($(filter $(ENV),test prod),)
$(error ENV must be 'test' or 'prod'. Usage: make <target> ENV=test)
endif

# Lambda definitions - add new lambdas here
LAMBDAS = get-jmap-session jmap-api core-echo blob-upload blob-download key-age-check

# Directories
BUILD_DIR = build
ENV_DIR = terraform/environments/$(ENV)
MODULE_DIR = terraform/modules/jmap-service

# Source tracking
GO_SOURCES := $(shell find . -name '*.go' -not -path './build/*' 2>/dev/null)
TF_FILES := $(shell find terraform -name '*.tf' 2>/dev/null)

# Lambda artifacts (all zips are named lambda.zip)
LAMBDA_ZIPS := $(foreach lambda,$(LAMBDAS),$(BUILD_DIR)/$(lambda)/lambda.zip)

# Get configuration from environment's terraform.tfvars
AWS_REGION ?= $(shell grep '^aws_region' $(ENV_DIR)/terraform.tfvars 2>/dev/null | cut -d'=' -f2 | tr -d ' "' || echo "ap-southeast-2")
ENVIRONMENT ?= $(ENV)

# Ensure state bucket and get its name
STATE_BUCKET = $(shell $(MODULE_DIR)/scripts/ensure-state-bucket.sh | grep TERRAFORM_STATE_BUCKET | cut -d'=' -f2)

# Terraform backend config
BACKEND_CONFIG = -backend-config="bucket=$(STATE_BUCKET)" \
                 -backend-config="key=jmap-service/$(ENVIRONMENT).tfstate" \
                 -backend-config="region=$(AWS_REGION)"

help:
	@echo "JMAP Service Core Infrastructure - Makefile targets:"
	@echo ""
	@$(MAKE) help-tfvars
	@echo ""
	@echo "Build Commands:"
	@echo "  make deps                    - Initialize go.mod and fetch dependencies"
	@echo "  make build                   - Compile all Go lambdas (linux/arm64)"
	@echo "  make package                 - Create all Lambda deployment packages (zip)"
	@echo "  make test                    - Run all tests (Go + CloudFront)"
	@echo "  make test-go                 - Run Go unit tests only"
	@echo "  make test-cloudfront         - Run CloudFront function tests only"
	@echo "  make integration-test ENV=<env> - Run integration tests against deployed env"
	@echo "  make jmap-client-test ENV=<env> - Run JMAP protocol compliance tests (jmapc)"
	@echo "  make get-token ENV=<env>     - Get Cognito JWT token for test user"
	@echo "  make lint                    - Run golangci-lint (required)"
	@echo ""
	@echo "Terraform Commands:"
	@echo "  make init ENV=<env>          - Initialize Terraform (creates state bucket)"
	@echo "  make plan ENV=<env>          - Create Terraform plan file"
	@echo "  make show-plan ENV=<env>     - Display the Terraform plan"
	@echo "  make apply ENV=<env>         - Apply the plan file (requires plan)"
	@echo "  make plan-destroy ENV=<env>  - Create destroy plan file"
	@echo "  make destroy ENV=<env>       - Apply the destroy plan (requires plan-destroy)"
	@echo "  make fmt                     - Format Terraform files"
	@echo "  make validate ENV=<env>      - Validate Terraform configuration"
	@echo "  make outputs ENV=<env>       - Show Terraform outputs"
	@echo ""
	@echo "Cleanup Commands:"
	@echo "  make clean ENV=<env>         - Clean Terraform files only"
	@echo "  make clean-all ENV=<env>     - Clean Terraform + Go build artifacts"
	@echo ""
	@echo "Configured lambdas: $(LAMBDAS)"
	@echo "Environments: test, prod"
	@echo "Current environment: $(ENV)"
	@echo "Current region: $(AWS_REGION)"
	@echo ""

# Go module management - regenerate when source files change
go.mod: $(GO_SOURCES)
	@echo "Checking Go module dependencies..."
	@if [ ! -f go.mod ]; then \
		echo "Initializing Go module..."; \
		go mod init github.com/jarrod-lowe/jmap-service-core; \
	fi
	@go mod tidy
	@touch go.mod

# Fetch dependencies when go.mod changes
go.sum: go.mod $(GO_SOURCES)
	@echo "Fetching Go dependencies..."
	go get ./...
	go mod tidy

deps: go.sum
	@echo "Dependencies are up to date"

# Pattern rule: build any lambda binary
build/%/bootstrap: go.sum cmd/%/*.go
	@echo "Building Lambda: $*"
	@mkdir -p build/$*
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -o $@ ./cmd/$*
	@echo "Build complete: $@"

# Pattern rule: package any lambda zip
# Includes shared collector.yaml - rebuilds if either bootstrap or config changes
build/%/lambda.zip: build/%/bootstrap collector.yaml
	@echo "Packaging Lambda: $*"
	@cp collector.yaml build/$*/
	@cd build/$* && zip -q lambda.zip bootstrap collector.yaml
	@echo "Package created: $@"

# Build all lambdas
build-all: $(foreach lambda,$(LAMBDAS),build/$(lambda)/bootstrap)

# Package all lambdas
package-all: $(LAMBDA_ZIPS)

# Default targets
build: build-all
package: package-all

# Run Go tests
test-go:
	@echo "Running Go tests..."
	go test -v ./...

# CloudFront function test dependencies
$(MODULE_DIR)/cloudfront-functions/node_modules: $(MODULE_DIR)/cloudfront-functions/package.json
	@echo "Installing CloudFront function test dependencies..."
	cd $(MODULE_DIR)/cloudfront-functions && npm install
	@touch $@

# Run CloudFront function tests
test-cloudfront: $(MODULE_DIR)/cloudfront-functions/node_modules
	@echo "Running CloudFront function tests..."
	cd $(MODULE_DIR)/cloudfront-functions && npm test

# Run all tests
test: test-go test-cloudfront

# Run integration tests against deployed environment
integration-test:
	@echo "Running integration tests for $(ENV) environment..."
	@./scripts/integration-test.sh $(ENV)

# Python venv for jmap-client tests
scripts/.venv: scripts/jmap-client/requirements.txt
	@echo "Creating Python virtual environment..."
	python3 -m venv scripts/.venv
	scripts/.venv/bin/pip install -q -r scripts/jmap-client/requirements.txt
	@echo "Python venv created at scripts/.venv"

# Run JMAP protocol compliance tests using jmapc
jmap-client-test: scripts/.venv
	@echo "Running JMAP protocol compliance tests for $(ENV) environment..."
	@./scripts/jmap-client-test.sh $(ENV)

# Run linter - MUST be installed
# PATH includes ~/go/bin for go-installed tools
lint:
	@PATH="$(HOME)/go/bin:$$PATH"; \
	if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "ERROR: golangci-lint is not installed"; \
		echo "Install it with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
		exit 1; \
	fi; \
	echo "Running golangci-lint..."; \
	golangci-lint run ./...

# Format Terraform files - depends on actual .tf files
terraform/.fmt: $(TF_FILES)
	@echo "Formatting Terraform files..."
	cd terraform && terraform fmt -recursive
	@touch terraform/.fmt
	@echo "Terraform files formatted"

fmt: terraform/.fmt

# Initialize Terraform - depends on .tf files
$(ENV_DIR)/.terraform: $(MODULE_DIR)/scripts/ensure-state-bucket.sh $(TF_FILES)
	@if [ ! -f "$(ENV_DIR)/terraform.tfvars" ]; then \
		echo "⚠️  terraform.tfvars not found!"; \
		echo "    Run: make restore-tfvars ENV=$(ENV)"; \
		echo "    Or create from template (first time)"; \
		exit 1; \
	fi
	@echo "Ensuring state bucket exists..."
	@$(MODULE_DIR)/scripts/ensure-state-bucket.sh > /dev/null
	@echo "Initializing Terraform for $(ENV) environment..."
	cd $(ENV_DIR) && terraform init $(BACKEND_CONFIG)
	@touch $(ENV_DIR)/.terraform
	@echo "Terraform initialized successfully"

init: $(ENV_DIR)/.terraform

# Create plan file - depends on all lambda zips and formatted terraform
$(ENV_DIR)/terraform.plan: $(ENV_DIR)/.terraform $(ENV_DIR)/*.tf $(MODULE_DIR)/*.tf $(LAMBDA_ZIPS) terraform/.fmt
	@echo "Creating Terraform plan for $(ENV) environment..."
	cd $(ENV_DIR) && terraform plan -out=terraform.plan
	@echo "Plan created: $(ENV_DIR)/terraform.plan"

plan: $(ENV_DIR)/terraform.plan

# Show the plan file
show-plan: $(ENV_DIR)/terraform.plan
	@echo "Showing Terraform plan for $(ENV) environment..."
	cd $(ENV_DIR) && terraform show terraform.plan

# Apply the plan file
apply: $(ENV_DIR)/terraform.plan
	@echo "Applying Terraform plan for $(ENV) environment..."
	cd $(ENV_DIR) && terraform apply terraform.plan && rm -f terraform.plan || { rm -f terraform.plan; exit 1; }
	@echo "Plan applied and removed"

# Create destroy plan file
$(ENV_DIR)/terraform.destroy.plan: $(ENV_DIR)/.terraform
	@echo "Creating Terraform destroy plan for $(ENV) environment..."
	cd $(ENV_DIR) && terraform plan -destroy -out=terraform.destroy.plan
	@echo "Destroy plan created: $(ENV_DIR)/terraform.destroy.plan"

plan-destroy: $(ENV_DIR)/terraform.destroy.plan

# Apply the destroy plan
destroy: $(ENV_DIR)/terraform.destroy.plan
	@echo "Applying Terraform destroy plan for $(ENV) environment..."
	cd $(ENV_DIR) && terraform apply terraform.destroy.plan && rm -f terraform.destroy.plan || { rm -f terraform.destroy.plan; exit 1; }
	@echo "Destroy plan applied and removed"

# Validate Terraform
validate: $(ENV_DIR)/.terraform
	@echo "Validating Terraform configuration for $(ENV) environment..."
	cd $(ENV_DIR) && terraform validate

# Show outputs
outputs: $(ENV_DIR)/.terraform
	@echo "Terraform outputs for $(ENV) environment:"
	cd $(ENV_DIR) && terraform output

# Clean Terraform files only
clean:
	@echo "Cleaning Terraform files for $(ENV) environment..."
	rm -rf $(ENV_DIR)/.terraform
	rm -f $(ENV_DIR)/.terraform.lock.hcl
	rm -f $(ENV_DIR)/terraform.plan
	rm -f $(ENV_DIR)/terraform.destroy.plan
	rm -f $(ENV_DIR)/*.tfstate
	rm -f $(ENV_DIR)/*.tfstate.backup
	rm -f terraform/.fmt
	@echo "Cleaned. Build artifacts preserved - use 'make clean-all' to remove builds."

# Full clean - removes everything
clean-all: clean
	@echo "Removing Go build artifacts..."
	rm -rf $(BUILD_DIR)
	rm -f go.sum
	@echo "Complete clean finished."

# tfvars Management
help-tfvars:
	@echo "tfvars Management:"
	@echo "  make restore-tfvars ENV=<env>  - Download terraform.tfvars from S3"
	@echo ""
	@echo "Note: Backup to S3 happens automatically on every 'make apply' via tfvars-backup.tf"
	@echo ""
	@echo "First time setup:"
	@echo "  1. Create terraform.tfvars from template:"
	@echo "     cp terraform/environments/_shared/terraform.tfvars.example \\"
	@echo "        terraform/environments/<env>/terraform.tfvars"
	@echo "  2. Edit values for your environment"
	@echo "  3. Run: make init plan apply ENV=<env>"
	@echo "  4. tfvars will auto-upload to S3 via tfvars-backup.tf resource"
	@echo ""
	@echo "Switching machines:"
	@echo "  1. Run: make restore-tfvars ENV=<env>"
	@echo "  2. Continue with normal workflow"

restore-tfvars:
	@echo "Restoring terraform.tfvars for $(ENV) from S3..."
	@ACCOUNT_ID=$$(AWS_PROFILE=ses-mail aws sts get-caller-identity --query Account --output text); \
	BUCKET="terraform-state-$$ACCOUNT_ID"; \
	KEY="jmap-service/$(ENV)/terraform.tfvars"; \
	TARGET="$(ENV_DIR)/terraform.tfvars"; \
	if AWS_PROFILE=ses-mail aws s3 cp "s3://$$BUCKET/$$KEY" "$$TARGET" 2>/dev/null; then \
		echo "✓ Downloaded terraform.tfvars to $$TARGET"; \
	else \
		echo "✗ Failed to download terraform.tfvars from S3"; \
		echo "  Either the file doesn't exist yet, or you need to run 'make init apply' first"; \
		exit 1; \
	fi

# Invalidate CloudFront cache
invalidate-cache: $(ENV_DIR)/.terraform
	@echo "Invalidating CloudFront cache for $(ENV) environment..."
	@DISTRIBUTION_ID=$$(cd $(ENV_DIR) && terraform output -raw cloudfront_distribution_id 2>/dev/null); \
	if [ -z "$$DISTRIBUTION_ID" ]; then \
		echo "✗ Could not get CloudFront distribution ID"; \
		exit 1; \
	fi; \
	echo "Distribution ID: $$DISTRIBUTION_ID"; \
	AWS_PROFILE=ses-mail aws cloudfront create-invalidation \
		--distribution-id "$$DISTRIBUTION_ID" \
		--paths "/*" \
		--output text; \
	echo "✓ Cache invalidation initiated"

# Get Cognito JWT token for test user
get-token: $(ENV_DIR)/.terraform
	@cd $(ENV_DIR) && \
	USER_POOL_ID=$$(terraform output -raw cognito_user_pool_id) && \
	CLIENT_ID=$$(terraform output -raw cognito_client_id) && \
	REGION=$$(echo "$$USER_POOL_ID" | cut -d'_' -f1) && \
	USERNAME=$$(yq ".env.$(ENV).username" ../../../test-user.yaml) && \
	PASSWORD=$$(yq ".env.$(ENV).password" ../../../test-user.yaml) && \
	aws cognito-idp admin-initiate-auth \
		--user-pool-id "$$USER_POOL_ID" \
		--client-id "$$CLIENT_ID" \
		--auth-flow ADMIN_NO_SRP_AUTH \
		--auth-parameters "USERNAME=$$USERNAME,PASSWORD=$$PASSWORD" \
		--region "$$REGION" \
		--query 'AuthenticationResult.IdToken' \
		--output text
