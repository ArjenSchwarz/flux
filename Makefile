SHELL = /bin/bash
.SHELLFLAGS = -eo pipefail -c

# iOS project settings
IOS_SCHEME = Flux
IOS_PROJECT = Flux/Flux.xcodeproj
IOS_BUNDLE_ID = me.nore.ig.Flux
IOS_CONFIG ?= Debug
IOS_DERIVED_DATA = ./DerivedData

# Pipe through xcbeautify if available, otherwise raw output
XCBEAUTIFY := $(shell command -v xcbeautify 2>/dev/null)
ifdef XCBEAUTIFY
PIPE_PRETTY = | xcbeautify
else
PIPE_PRETTY =
endif

# Device deployment
DEVICE_MODEL ?= iPhone 17 Pro
DEVICE_ID = $(shell tmp=$$(mktemp); \
	xcrun devicectl list devices --json-output "$$tmp" >/dev/null 2>&1; \
	jq -r '.result.devices[] | select(.hardwareProperties.marketingName == "$(DEVICE_MODEL)") | .connectionProperties.potentialHostnames[] | select(startswith("0000"))' "$$tmp" 2>/dev/null | sed 's/.coredevice.local//' | head -1; \
	rm -f "$$tmp")

# Distribution
IOS_ARCHIVE_PATH = ./build/$(IOS_SCHEME).xcarchive
IOS_EXPORT_PATH = ./build/export

.PHONY: help
help:
	@echo "Available targets:"
	@echo ""
	@echo "  Go Backend:"
	@echo "    build           - Build poller binary"
	@echo "    build-api       - Build Lambda API binary (ARM64 Linux)"
	@echo "    test            - Run Go tests"
	@echo "    integration     - Run integration tests (starts DynamoDB Local)"
	@echo "    fmt             - Format Go code"
	@echo "    vet             - Run go vet"
	@echo "    lint            - Run golangci-lint"
	@echo "    modernize       - Tidy and fix Go modules"
	@echo "    check           - Run fmt, vet, lint, and test"
	@echo "    docker-build    - Build Docker image"
	@echo "    docker-dry-run  - Run Docker image in dry-run mode"
	@echo "    deps-tidy       - Tidy Go modules"
	@echo "    deps-update     - Update Go dependencies"
	@echo ""
	@echo "  iOS App (Debug):"
	@echo "    ios-lint        - Run SwiftLint"
	@echo "    ios-lint-fix    - Run SwiftLint with auto-fix"
	@echo "    ios-build       - Build for iOS Simulator"
	@echo "    ios-test        - Run full test suite on iOS Simulator"
	@echo "    ios-test-ui     - Run UI tests only"
	@echo "    ios-install     - Build and install Debug on device"
	@echo "    ios-run         - Build, install, and launch Debug on device"
	@echo ""
	@echo "  iOS App (Release):"
	@echo "    ios-install-release - Build and install Release on device"
	@echo "    ios-run-release     - Build, install, and launch Release on device"
	@echo ""
	@echo "  iOS Distribution:"
	@echo "    ios-archive     - Create xcarchive for iOS"
	@echo "    ios-upload      - Archive and upload to App Store Connect"
	@echo ""
	@echo "  Utilities:"
	@echo "    ios-clean       - Clean iOS build artifacts"
	@echo ""
	@echo "Device targets use DEVICE_MODEL (default: iPhone 17 Pro)"
	@echo "Override with: make ios-install DEVICE_MODEL='iPhone 16'"

.PHONY: build build-api test integration fmt vet lint modernize check docker-build docker-dry-run deps-tidy deps-update

# DynamoDB Local container settings for `make integration`
DYNAMODB_LOCAL_IMAGE ?= amazon/dynamodb-local:latest
DYNAMODB_LOCAL_NAME  ?= flux-dynamodb-local
DYNAMODB_LOCAL_PORT  ?= 8000

build:
	CGO_ENABLED=0 go build -o bin/poller ./cmd/poller

build-api:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o lambda/bootstrap ./cmd/api

test:
	go test ./...

# integration runs the INTEGRATION-gated tests against a DynamoDB Local
# container started for the duration of the run. The container is torn
# down on success, on test failure, and on Ctrl-C (trap EXIT).
#
# DYNAMODB_LOCAL_ENDPOINT is exported so the e2e tests (Task 15 in the
# daily-derived-stats spec) can dial the container without extra config.
# Set DYNAMODB_LOCAL_PORT to a different value if 8000 is in use.
integration:
	@command -v docker >/dev/null 2>&1 || { \
		echo "Error: docker is required for 'make integration'"; \
		exit 1; \
	}
	@echo "Starting DynamoDB Local on port $(DYNAMODB_LOCAL_PORT)..."
	@docker rm -f $(DYNAMODB_LOCAL_NAME) >/dev/null 2>&1 || true
	@docker run -d --rm \
		--name $(DYNAMODB_LOCAL_NAME) \
		-p $(DYNAMODB_LOCAL_PORT):8000 \
		$(DYNAMODB_LOCAL_IMAGE) >/dev/null
	@trap 'echo "Stopping DynamoDB Local..."; docker rm -f $(DYNAMODB_LOCAL_NAME) >/dev/null 2>&1 || true' EXIT INT TERM; \
		INTEGRATION=1 \
		DYNAMODB_LOCAL_ENDPOINT=http://localhost:$(DYNAMODB_LOCAL_PORT) \
		go test ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

lint:
	golangci-lint run

modernize:
	go mod tidy -compat=1.26
	go fix ./...

check: fmt vet lint test

docker-build:
	docker buildx build --platform linux/arm64 -t flux-poller .

docker-dry-run:
	docker run --rm \
		-e DRY_RUN=true \
		-e ALPHA_APP_ID=$${ALPHA_APP_ID} \
		-e ALPHA_APP_SECRET=$${ALPHA_APP_SECRET} \
		-e SYSTEM_SERIAL=$${SYSTEM_SERIAL} \
		-e OFFPEAK_START=11:00 \
		-e OFFPEAK_END=14:00 \
		-e TZ=Australia/Sydney \
		flux-poller

deps-tidy:
	go mod tidy

deps-update:
	go get -u ./...
	go mod tidy

# =============================================================================
# iOS App
# =============================================================================

.PHONY: ios-lint
ios-lint:
	cd Flux && swiftlint lint --strict

.PHONY: ios-lint-fix
ios-lint-fix:
	cd Flux && swiftlint lint --fix --strict

.PHONY: ios-build
ios-build:
	xcodebuild build \
		-project $(IOS_PROJECT) \
		-scheme $(IOS_SCHEME) \
		-destination 'platform=iOS Simulator,name=iPhone 17 Pro' \
		-configuration $(IOS_CONFIG) \
		-derivedDataPath $(IOS_DERIVED_DATA) \
		$(PIPE_PRETTY)

.PHONY: ios-test
# Runs FluxTests and FluxUITests. FluxCoreTests live in the local Swift Package
# (Flux/Packages/FluxCore) and are not picked up by xcodebuild from this scheme
# — Xcode's Test Navigator runs them fine from the IDE, but from the CLI they
# need to be run separately (see `make ios-test-core`).
ios-test:
	xcodebuild test \
		-project $(IOS_PROJECT) \
		-scheme $(IOS_SCHEME) \
		-destination 'platform=iOS Simulator,name=iPhone 17 Pro' \
		-configuration Debug \
		-derivedDataPath $(IOS_DERIVED_DATA) \
		-parallel-testing-worker-count 1 \
		-maximum-concurrent-test-simulator-destinations 1 \
		$(PIPE_PRETTY)

.PHONY: ios-test-core
# Builds the Flux app (which pulls in the FluxCore package) and then tells the
# user to run FluxCoreTests from Xcode's Test Navigator. xcodebuild from the
# CLI cannot currently run package test targets that aren't bound to a project
# target — see the `ios-test` note above.
ios-test-core:
	@echo "FluxCoreTests are run from Xcode's Test Navigator."
	@echo "Open Flux/Flux.xcodeproj, select the Flux scheme, then ⌘U on FluxCoreTests."

.PHONY: ios-test-ui
ios-test-ui:
	xcodebuild test \
		-project $(IOS_PROJECT) \
		-scheme $(IOS_SCHEME) \
		-destination 'platform=iOS Simulator,name=iPhone 17 Pro' \
		-configuration Debug \
		-derivedDataPath $(IOS_DERIVED_DATA) \
		-only-testing:FluxUITests \
		-parallel-testing-worker-count 1 \
		-maximum-concurrent-test-simulator-destinations 1 \
		$(PIPE_PRETTY)

.PHONY: ios-install
ios-install:
	@if [ -z "$(DEVICE_ID)" ]; then \
		echo "Error: No $(DEVICE_MODEL) device found"; \
		exit 1; \
	fi
	@echo "Building $(IOS_CONFIG) for device $(DEVICE_ID)..."
	xcodebuild build \
		-project $(IOS_PROJECT) \
		-scheme $(IOS_SCHEME) \
		-destination 'id=$(DEVICE_ID)' \
		-configuration $(IOS_CONFIG) \
		-derivedDataPath $(IOS_DERIVED_DATA) \
		$(PIPE_PRETTY)
	@echo "Installing on device..."
	xcrun devicectl device install app \
		--device $(DEVICE_ID) \
		$(IOS_DERIVED_DATA)/Build/Products/$(IOS_CONFIG)-iphoneos/$(IOS_SCHEME).app

.PHONY: ios-run
ios-run: ios-install
	@echo "Launching app..."
	xcrun devicectl device process launch --device $(DEVICE_ID) $(IOS_BUNDLE_ID)

# Release builds — delegate to base targets with IOS_CONFIG=Release
.PHONY: ios-install-release
ios-install-release:
	$(MAKE) ios-install IOS_CONFIG=Release

.PHONY: ios-run-release
ios-run-release:
	$(MAKE) ios-run IOS_CONFIG=Release

# Distribution
.PHONY: ios-archive
ios-archive:
	xcodebuild archive \
		-project $(IOS_PROJECT) \
		-scheme $(IOS_SCHEME) \
		-destination 'generic/platform=iOS' \
		-configuration Release \
		-archivePath $(IOS_ARCHIVE_PATH) \
		-allowProvisioningUpdates \
		$(PIPE_PRETTY)
	@echo "Archive created at $(IOS_ARCHIVE_PATH)"

# To re-upload an existing archive without rebuilding:
#   xcodebuild -exportArchive -archivePath ./build/Flux.xcarchive \
#     -exportOptionsPlist Flux/ExportOptions.plist -exportPath ./build/export -allowProvisioningUpdates
.PHONY: ios-upload
ios-upload: ios-archive
	xcodebuild -exportArchive \
		-archivePath $(IOS_ARCHIVE_PATH) \
		-exportOptionsPlist Flux/ExportOptions.plist \
		-exportPath $(IOS_EXPORT_PATH) \
		-allowProvisioningUpdates
	@echo "Uploaded to App Store Connect"

# Cleaning
.PHONY: ios-clean
ios-clean:
	xcodebuild clean \
		-project $(IOS_PROJECT) \
		-scheme $(IOS_SCHEME)
	rm -rf $(IOS_DERIVED_DATA) build
