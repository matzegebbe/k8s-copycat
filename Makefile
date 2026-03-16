.DEFAULT_GOAL := help

GO ?= go
GOLANGCI_LINT ?= golangci-lint
BINARY_NAME ?= k8s-copycat
BIN_DIR ?= bin
BINARY_PATH := $(BIN_DIR)/$(BINARY_NAME)
GO_PACKAGES := ./...
GO_TEST_FLAGS ?=

.PHONY: help build run fmt lint test verify clean check-golangci-lint

help: ## Show available targets.
	@awk 'BEGIN {FS = ":.*## "}; /^[a-zA-Z0-9_.-]+:.*## / {printf "%-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the controller binary.
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(BINARY_PATH) ./cmd/manager

run: ## Run the controller locally.
	$(GO) run ./cmd/manager

fmt: ## Format Go source files.
	$(GO) fmt $(GO_PACKAGES)

lint: check-golangci-lint ## Run golangci-lint.
	$(GOLANGCI_LINT) run

test: ## Run unit tests.
	$(GO) test $(GO_TEST_FLAGS) $(GO_PACKAGES)

verify: lint test ## Run the local verification suite.

clean: ## Remove generated artifacts.
	rm -rf $(BIN_DIR)

check-golangci-lint:
	@command -v $(GOLANGCI_LINT) >/dev/null 2>&1 || { \
		echo "error: $(GOLANGCI_LINT) is required for 'make lint'"; \
		exit 1; \
	}
