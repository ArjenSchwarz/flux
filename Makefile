.PHONY: build test fmt vet lint modernize check docker-build deps-tidy deps-update

build:
	CGO_ENABLED=0 go build -o bin/poller ./cmd/poller

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

deps-tidy:
	go mod tidy

deps-update:
	go get -u ./...
	go mod tidy
