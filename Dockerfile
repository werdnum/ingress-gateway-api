# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy go mod files first for better layer caching
COPY go.mod go.sum ./

# Download dependencies with cache mount
# This cache persists across builds even when go.mod changes
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy source code
COPY cmd/ cmd/
COPY internal/ internal/

# Build with cache mounts for module and build caches
# - /go/pkg/mod: module cache (dependencies)
# - /root/.cache/go-build: build cache (compiled packages)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -o manager ./cmd/main.go

# Final image
FROM gcr.io/distroless/static:nonroot

WORKDIR /

COPY --from=builder /app/manager .

USER 65532:65532

ENTRYPOINT ["/manager"]
