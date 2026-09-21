GO ?= go

MODULES := \
	. \
	cmd/benchmark \
	integrations/aerospike \
	integrations/prometheus \
	integrations/redis \
	integrations/valkey \
	example/basic \
	example/prebid-module

GO_FILES := $(shell git ls-files --cached --others --exclude-standard '*.go')

.DEFAULT_GOAL := help

.PHONY: help smoke check build-basic benchmark test lint vet fmt fmt-check \
	deps deps-tidy deps-check deps-update

help: ## Show available commands
	@awk 'BEGIN {FS = ":.*## "; print "Available commands:"} /^[a-zA-Z0-9_-]+:.*## / {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

smoke: fmt-check ## Run fast checks for local development
	$(GO) test ./...
	$(GO) vet ./...

check: deps-check test lint ## Run the complete validation suite

build-basic: ## Build the basic example into bin/basic
	@mkdir -p bin
	$(GO) build -C example/basic -o ../../bin/basic .

# Benchmark defaults. Override them on the command line, for example:
# make benchmark ENDPOINT=https://s2s.example.test/profiles PARTNER=example-partner ITERATIONS=100
ITERATIONS ?= 1000
VUS ?= 16
TIMEOUT ?= 400ms
HOOK_TIMEOUT_MS ?= 300
MAX_BACKGROUND_S2S ?= 2000
FIXTURE ?= testdata/sample.jsonl
CASES ?= 1 2 3 4 5 6
OUT ?= ./results
ERROR_THRESHOLD ?= 0.01

benchmark: ## Run the k6 enrichment benchmark
	@test -n "$(ENDPOINT)" || { echo "ENDPOINT is required" >&2; exit 1; }
	@test -n "$(PARTNER)" || { echo "PARTNER is required" >&2; exit 1; }
	cd cmd/benchmark && \
		ENDPOINT="$(ENDPOINT)" \
		PARTNER="$(PARTNER)" \
		ITERATIONS="$(ITERATIONS)" \
		VUS="$(VUS)" \
		TIMEOUT="$(TIMEOUT)" \
		HOOK_TIMEOUT_MS="$(HOOK_TIMEOUT_MS)" \
		MAX_BACKGROUND_S2S="$(MAX_BACKGROUND_S2S)" \
		FIXTURE="$(FIXTURE)" \
		CASES="$(CASES)" \
		OUT="$(OUT)" \
		ERROR_THRESHOLD="$(ERROR_THRESHOLD)" \
		./scripts/run-cases.sh

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
