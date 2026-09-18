GO ?= go

MODULES := \
	. \
	integrations/aerospike \
	integrations/prometheus \
	integrations/redis \
	integrations/valkey \
	example/basic \
	example/prebid-module

GO_FILES := $(shell git ls-files --cached --others --exclude-standard '*.go')

.DEFAULT_GOAL := help

.PHONY: help smoke check build-basic test lint vet fmt fmt-check deps \
	deps-tidy deps-check deps-update

help: ## Show available commands
	@awk 'BEGIN {FS = ":.*## "; print "Available commands:"} /^[a-zA-Z0-9_-]+:.*## / {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

smoke: fmt-check ## Run fast checks for local development
	$(GO) test ./...
	$(GO) vet ./...

check: deps-check test lint ## Run the complete validation suite

build-basic: ## Build the basic example into bin/basic
	@mkdir -p bin
	$(GO) build -C example/basic -o ../../bin/basic .

test: ## Run race-enabled tests in every Go module
	@set -e; for module in $(MODULES); do \
		echo "==> $$module: test -race"; \
		(cd "$$module" && $(GO) test -race ./...); \
	done

lint: fmt-check vet ## Check formatting and vet every Go module

vet:
	@set -e; for module in $(MODULES); do \
		echo "==> $$module: vet"; \
		(cd "$$module" && $(GO) vet ./...); \
	done

fmt: ## Format all tracked Go files
	gofmt -w $(GO_FILES)

fmt-check:
	@test -z "$$(gofmt -l $(GO_FILES))" || { \
		echo "Run 'make fmt' to format these files:"; \
		gofmt -l $(GO_FILES); \
		exit 1; \
	}

deps: ## Download dependencies for every Go module
	@set -e; for module in $(MODULES); do \
		echo "==> $$module: mod download"; \
		(cd "$$module" && $(GO) mod download); \
	done

deps-tidy: ## Tidy go.mod and go.sum in every Go module
	@set -e; for module in $(MODULES); do \
		echo "==> $$module: mod tidy"; \
		(cd "$$module" && $(GO) mod tidy); \
	done

deps-check:
	@set -e; for module in $(MODULES); do \
		echo "==> $$module: mod tidy check"; \
		(cd "$$module" && $(GO) mod tidy -diff); \
	done

deps-update: ## Update dependencies in every Go module, then tidy manifests
	@set -e; for module in $(MODULES); do \
		echo "==> $$module: dependency update"; \
		(cd "$$module" && $(GO) get -u ./... && $(GO) mod tidy); \
	done
