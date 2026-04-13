# Builder stage
FROM golang:1.26-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build \
    -trimpath -ldflags="-s -w" \
    -o /poller ./cmd/poller

# Runtime stage — distroless provides CA certs and timezone data.
FROM gcr.io/distroless/static:nonroot
COPY --from=builder /poller /poller
ENTRYPOINT ["/poller"]
