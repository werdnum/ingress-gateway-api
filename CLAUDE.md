# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

A Kubernetes controller that converts Ingress resources to Gateway API HTTPRoutes for Envoy Gateway. Written in Go using controller-runtime.

## Build Commands

```bash
make build       # Build binary to bin/manager
make test        # Run tests with coverage (runs fmt and vet first)
make run         # Run controller locally
make docker-build IMG=<tag>  # Build container image
```

Run a single test:
```bash
go test ./internal/converter -run TestConvertIngressFull -v
```

## Architecture

### Core Flow
1. **IngressReconciler** (`internal/controller/ingress_controller.go`) watches Kubernetes Ingress resources
2. **Converter** (`internal/converter/converter.go`) transforms Ingress specs into Gateway API resources
3. Generated resources are created with owner references back to the source Ingress

### Key Packages
- `internal/controller/` - Reconciliation loop, finalizer handling, error classification
- `internal/converter/` - Ingress to HTTPRoute conversion, filter generation, policy creation
- `internal/annotations/` - Nginx annotation parsing and extraction
- `internal/config/` - CLI flags and environment variable handling

### Generated Resources
The controller creates these resources from a single Ingress:
- `HTTPRoute` - One per hostname in the Ingress
- `BackendTrafficPolicy` - Timeouts, connection pooling, body size limits
- `ClientTrafficPolicy` - Buffer sizes, keep-alive settings
- `SecurityPolicy` - CORS, external auth (ExtAuth)
- `ReferenceGrant` - Cross-namespace service references

### Annotation Support
Supports `nginx.ingress.kubernetes.io/*` annotations for:
- Timeouts: `proxy-read-timeout`, `proxy-send-timeout`
- CORS: `enable-cors`, `cors-allow-origin`, `cors-allow-methods`, etc.
- Auth: `auth-url`, `auth-signin`, `auth-response-headers`
- Rewrites: `rewrite-target` (with capture group support), `app-root`
- Path handling: `use-regex`

## Configuration

Controller flags (also available as env vars):
- `-gateway-name` / `GATEWAY_NAME` - Target Gateway name (default: "eg-gateway")
- `-gateway-namespace` / `GATEWAY_NAMESPACE` - Target Gateway namespace (default: "envoy-gateway")
- `-ingress-class` / `INGRESS_CLASS` - Filter by IngressClass (empty = all)
