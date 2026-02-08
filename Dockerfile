FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY cmd/ cmd/
COPY internal/ internal/

# Build
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o manager ./cmd/main.go

# Final image
FROM gcr.io/distroless/static:nonroot

WORKDIR /

COPY --from=builder /app/manager .

USER 65532:65532

ENTRYPOINT ["/manager"]
