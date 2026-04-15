.PHONY: build build-api test fmt vet lint modernize check docker-build docker-dry-run deps-tidy deps-update

build:
	CGO_ENABLED=0 go build -o bin/poller ./cmd/poller

build-api:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o lambda/bootstrap ./cmd/api

test:
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
		-e TZ=Australia/Melbourne \
		flux-poller

deps-tidy:
	go mod tidy

deps-update:
	go get -u ./...
	go mod tidy
