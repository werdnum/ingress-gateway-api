.PHONY: all build run test clean docker-build docker-push install deploy

# Image URL to use all building/pushing image targets
IMG ?= ingress-gateway-api:latest

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

all: build

##@ Development

fmt: ## Run go fmt against code.
	go fmt ./...

vet: ## Run go vet against code.
	go vet ./...

test: fmt vet ## Run tests.
	go test ./internal/annotations/... ./internal/controller/... ./internal/converter/... -coverprofile cover.out

##@ Build

build: fmt vet ## Build manager binary.
	go build -o bin/manager ./cmd/main.go

run: fmt vet ## Run a controller from your host.
	go run ./cmd/main.go

docker-build: ## Build docker image with the manager.
	docker build -t ${IMG} .

docker-push: ## Push docker image with the manager.
	docker push ${IMG}

##@ Deployment

install: ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.2.0/standard-install.yaml

deploy: ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	kubectl apply -f config/rbac/
	kubectl apply -f config/manager/

undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	kubectl delete -f config/manager/
	kubectl delete -f config/rbac/

##@ Dependencies

tidy: ## Run go mod tidy
	go mod tidy

deps: ## Download dependencies
	go mod download

##@ Help

help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
