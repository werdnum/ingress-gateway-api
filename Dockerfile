# syntax=docker/dockerfile:1

# Dependencies stage - cached unless go.mod/go.sum change
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS deps

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Builder stage - rebuilds when source changes
FROM deps AS builder

ARG TARGETOS
ARG TARGETARCH

COPY cmd/ cmd/
COPY internal/ internal/

# Cross-compile for target platform (much faster than QEMU emulation)
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o manager ./cmd/main.go

# Final image
FROM gcr.io/distroless/static:nonroot

WORKDIR /

COPY --from=builder /app/manager .

USER 65532:65532

ENTRYPOINT ["/manager"]
