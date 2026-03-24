# -------------------------------------------------------
# terramate-atlantis-config
# -------------------------------------------------------

BINARY  := terramate-atlantis-config
MODULE  := github.com/devops-roast/$(BINARY)/cmd
VERSION := $(shell \
  git describe --tags --always --dirty 2>/dev/null \
  || echo "v0.1.0-dev")
COMMIT  := $(shell \
  git rev-parse --short HEAD 2>/dev/null \
  || echo "unknown")
DATE    := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS := -s -w \
  -X $(MODULE).Version=$(VERSION) \
  -X $(MODULE).Commit=$(COMMIT) \
  -X $(MODULE).BuildDate=$(DATE)

PLATFORMS := \
  linux/amd64 linux/arm64 \
  darwin/amd64 darwin/arm64 \
  windows/amd64

.DEFAULT_GOAL := help

# -------------------------------------------------------
##@ Build
# -------------------------------------------------------

.PHONY: build
build: ## Build the binary
	go build -ldflags '$(LDFLAGS)' -o $(BINARY) .

.PHONY: build-all
build-all: ## Build for all platforms
	@$(foreach platform,$(PLATFORMS), \
	  $(eval OS   = $(word 1,$(subst /, ,$(platform)))) \
	  $(eval ARCH = $(word 2,$(subst /, ,$(platform)))) \
	  $(eval EXT  = $(if $(filter windows,$(OS)),.exe,)) \
	  GOOS=$(OS) GOARCH=$(ARCH) \
	    go build -ldflags '$(LDFLAGS)' \
	    -o dist/$(BINARY)-$(OS)-$(ARCH)$(EXT) . ; \
	)

.PHONY: install
install: ## Install to $$GOPATH/bin
	go build -ldflags '$(LDFLAGS)' \
	  -o $$(go env GOPATH)/bin/$(BINARY) .

.PHONY: clean
clean: ## Remove build artifacts
	rm -f $(BINARY)
	rm -rf dist/ coverage.out coverage.html

# -------------------------------------------------------
##@ Test
# -------------------------------------------------------

.PHONY: test
test: ## Run all tests
	go test ./... -count=1

.PHONY: test-verbose
test-verbose: ## Run tests (verbose)
	go test ./... -v -count=1

.PHONY: test-race
test-race: ## Run tests with race detector
	go test -race ./... -count=1

.PHONY: test-cover
test-cover: ## Run tests with coverage report
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

.PHONY: cover-html
cover-html: test-cover ## Generate HTML coverage report
	go tool cover -html=coverage.out -o coverage.html
	@echo "open coverage.html"

.PHONY: test-integration
test-integration: ## Run integration tests only
	go test -v -run TestIntegration ./...

# -------------------------------------------------------
##@ Quality
# -------------------------------------------------------

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: fmt
fmt: ## Format all Go files
	gofmt -w -s .

.PHONY: fmt-check
fmt-check: ## Check formatting (fails if dirty)
	@test -z "$$(gofmt -l .)" \
	  || { echo "needs formatting:"; gofmt -l .; exit 1; }

.PHONY: check
check: vet fmt-check lint test ## Full CI check

# -------------------------------------------------------
##@ Dependencies
# -------------------------------------------------------

.PHONY: deps
deps: ## Download module dependencies
	go mod download

.PHONY: tidy
tidy: ## Tidy go.mod and go.sum
	go mod tidy

.PHONY: verify
verify: ## Verify module checksums
	go mod verify

# -------------------------------------------------------
##@ Release
# -------------------------------------------------------

.PHONY: release-dry
release-dry: ## Goreleaser dry-run (snapshot)
	goreleaser release --snapshot --clean

.PHONY: release
release: ## Release via goreleaser
	goreleaser release --clean

# -------------------------------------------------------
##@ Generate
# -------------------------------------------------------

.PHONY: generate-example
generate-example: build ## Generate sample atlantis.yaml
	./$(BINARY) generate --root . 2>/dev/null \
	  || echo "no stacks found in current directory"

# -------------------------------------------------------
##@ E2E
# -------------------------------------------------------

.PHONY: e2e
e2e: ## Full E2E: build, up, setup, test
	$(MAKE) -C e2e e2e

.PHONY: e2e-up
e2e-up: ## Start Gitea + Atlantis
	$(MAKE) -C e2e up

.PHONY: e2e-down
e2e-down: ## Stop E2E environment
	$(MAKE) -C e2e down

.PHONY: e2e-test
e2e-test: ## Run E2E tests
	$(MAKE) -C e2e test

.PHONY: e2e-clean
e2e-clean: ## Tear down E2E (including volumes)
	$(MAKE) -C e2e clean

.PHONY: e2e-logs
e2e-logs: ## Tail E2E service logs
	$(MAKE) -C e2e logs

.PHONY: e2e-status
e2e-status: ## Show E2E container status
	$(MAKE) -C e2e status

# -------------------------------------------------------
##@ Help
# -------------------------------------------------------

.PHONY: help
help: ## Show this help
	@awk '\
	  BEGIN { \
	    FS = ":.*##"; \
	    printf "\nUsage:\n  make \033[36m<target>\033[0m\n" \
	  } \
	  /^[a-zA-Z0-9_-]+:.*?##/ { \
	    printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 \
	  } \
	  /^##@/ { \
	    printf "\n\033[1m%s\033[0m\n", substr($$0, 5) \
	  }' $(MAKEFILE_LIST)
