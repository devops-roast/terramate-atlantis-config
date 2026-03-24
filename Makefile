BINARY_NAME := terramate-atlantis-config
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "v0.1.0-dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-s -w -X github.com/devops-roast/terramate-atlantis-config/cmd.Version=$(VERSION) -X github.com/devops-roast/terramate-atlantis-config/cmd.Commit=$(COMMIT) -X github.com/devops-roast/terramate-atlantis-config/cmd.BuildDate=$(BUILD_DATE)"

GO := go
GOTEST := $(GO) test
GOBUILD := $(GO) build
GOVET := $(GO) vet
GOMOD := $(GO) mod
GOFMT := gofmt

.DEFAULT_GOAL := help

##@ Build

.PHONY: build
build: ## Build the binary
	$(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME) .

.PHONY: build-all
build-all: ## Build for all platforms
	GOOS=linux   GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o dist/$(BINARY_NAME)-linux-amd64 .
	GOOS=linux   GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o dist/$(BINARY_NAME)-linux-arm64 .
	GOOS=darwin  GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o dist/$(BINARY_NAME)-darwin-amd64 .
	GOOS=darwin  GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o dist/$(BINARY_NAME)-darwin-arm64 .
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o dist/$(BINARY_NAME)-windows-amd64.exe .

.PHONY: install
install: ## Install the binary to $GOPATH/bin
	$(GOBUILD) $(LDFLAGS) -o $(shell go env GOPATH)/bin/$(BINARY_NAME) .

.PHONY: clean
clean: ## Remove build artifacts
	rm -f $(BINARY_NAME)
	rm -rf dist/

##@ Test

.PHONY: test
test: ## Run all tests
	$(GOTEST) ./... -count=1

.PHONY: test-verbose
test-verbose: ## Run all tests with verbose output
	$(GOTEST) ./... -v -count=1

.PHONY: test-race
test-race: ## Run tests with race detector
	$(GOTEST) -race ./... -count=1

.PHONY: test-cover
test-cover: ## Run tests with coverage
	$(GOTEST) -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out
	@echo ""
	@echo "To view HTML coverage report: make cover-html"

.PHONY: cover-html
cover-html: test-cover ## Open coverage report in browser
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

.PHONY: test-integration
test-integration: ## Run only integration tests
	$(GOTEST) -v -run TestIntegration ./...

##@ Quality

.PHONY: lint
lint: ## Run linters (requires golangci-lint)
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed. Install with:"; \
		echo "  brew install golangci-lint"; \
		echo "  or: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
		exit 1; \
	fi

.PHONY: vet
vet: ## Run go vet
	$(GOVET) ./...

.PHONY: fmt
fmt: ## Format all Go files
	$(GOFMT) -w -s .

.PHONY: fmt-check
fmt-check: ## Check formatting (CI-friendly)
	@test -z "$$($(GOFMT) -l .)" || { echo "Files need formatting:"; $(GOFMT) -l .; exit 1; }

.PHONY: check
check: vet fmt-check test ## Run all checks (CI target)

##@ Dependencies

.PHONY: deps
deps: ## Download dependencies
	$(GOMOD) download

.PHONY: tidy
tidy: ## Tidy go.mod
	$(GOMOD) tidy

.PHONY: verify
verify: ## Verify dependencies
	$(GOMOD) verify

##@ Release

.PHONY: release-dry
release-dry: ## Dry-run goreleaser
	@if command -v goreleaser >/dev/null 2>&1; then \
		goreleaser release --snapshot --clean; \
	else \
		echo "goreleaser not installed. Install with:"; \
		echo "  brew install goreleaser"; \
		exit 1; \
	fi

.PHONY: release
release: ## Release with goreleaser (requires GITHUB_TOKEN)
	goreleaser release --clean

##@ Generate

.PHONY: generate-example
generate-example: build ## Generate example atlantis.yaml to stdout
	./$(BINARY_NAME) generate --root . 2>/dev/null || echo "No terramate stacks found in current directory"

##@ E2E

.PHONY: e2e-up
e2e-up: ## Start the E2E environment (Gitea + Atlantis)
	$(MAKE) -C e2e up

.PHONY: e2e-down
e2e-down: ## Stop the E2E environment
	$(MAKE) -C e2e down

.PHONY: e2e-test
e2e-test: ## Run E2E tests against live environment
	$(MAKE) -C e2e test

.PHONY: e2e
e2e: ## Full E2E pipeline: build → up → setup → test
	$(MAKE) -C e2e e2e

.PHONY: e2e-clean
e2e-clean: ## Tear down E2E environment including volumes
	$(MAKE) -C e2e clean

.PHONY: e2e-logs
e2e-logs: ## Tail E2E service logs
	$(MAKE) -C e2e logs

.PHONY: e2e-status
e2e-status: ## Show E2E container status
	$(MAKE) -C e2e status

##@ Help

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z0-9_-]+:.*?##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)
