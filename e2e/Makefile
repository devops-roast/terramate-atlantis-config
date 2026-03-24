BINARY_NAME  := terramate-atlantis-config
E2E_DIR      := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
PROJECT_ROOT := $(abspath $(E2E_DIR)/..)
COMPOSE      := docker compose --project-directory $(E2E_DIR)

export GITEA_URL         ?= http://localhost:3000
export GITEA_ADMIN_USER  ?= e2e-admin
export GITEA_ADMIN_PASS  ?= e2e-password-123
export GITEA_ADMIN_EMAIL ?= admin@e2e.local
export TEST_REPO         ?= e2e-test-repo
export WEBHOOK_SECRET    ?= e2e-webhook-secret
export ATLANTIS_URL      ?= http://localhost:4141
export GITEA_CONTAINER   ?= e2e-gitea

GO_SRC := $(shell find $(PROJECT_ROOT)/cmd $(PROJECT_ROOT)/internal -name '*.go' 2>/dev/null)

.DEFAULT_GOAL := help

##@ E2E Environment

.PHONY: up
up: build ## Start Gitea + Atlantis (runs setup automatically)
	@test -f $(E2E_DIR)/.env.secrets || printf 'ATLANTIS_GITEA_TOKEN=placeholder\n' > $(E2E_DIR)/.env.secrets
	$(COMPOSE) up -d gitea
	@$(E2E_DIR)/scripts/wait-for-url.sh $(GITEA_URL)/api/v1/version 90
	@$(E2E_DIR)/scripts/setup.sh
	$(COMPOSE) up -d --build --force-recreate atlantis
	@$(E2E_DIR)/scripts/wait-for-url.sh $(ATLANTIS_URL)/healthz 90
	@echo ""
	@echo "Environment ready"
	@echo "  Gitea    $(GITEA_URL)  (user: $(GITEA_ADMIN_USER)  pass: $(GITEA_ADMIN_PASS))"
	@echo "  Atlantis $(ATLANTIS_URL)"

.PHONY: down
down: ## Stop all services (data preserved)
	$(COMPOSE) down

.PHONY: setup
setup: ## Re-run Gitea initialisation (idempotent)
	@$(E2E_DIR)/scripts/setup.sh
	$(COMPOSE) up -d --build --force-recreate atlantis
	@$(E2E_DIR)/scripts/wait-for-url.sh $(ATLANTIS_URL)/healthz 90

.PHONY: restart
restart: ## Restart all services
	$(COMPOSE) restart

.PHONY: clean
clean: ## Tear down everything including volumes
	$(COMPOSE) down -v --remove-orphans
	rm -rf $(E2E_DIR)/bin/ $(E2E_DIR)/.env.secrets

##@ Test

.PHONY: test
test: ## Run E2E test (create PR, verify Atlantis comments)
	@$(E2E_DIR)/scripts/run-test.sh

.PHONY: e2e
e2e: up test ## Full pipeline: build → up → setup → test

##@ Build

.PHONY: build
build: $(E2E_DIR)/bin/$(BINARY_NAME) ## Build the binary (linux, static)

$(E2E_DIR)/bin/$(BINARY_NAME): $(PROJECT_ROOT)/main.go $(GO_SRC)
	@mkdir -p $(E2E_DIR)/bin
	cd $(PROJECT_ROOT) && CGO_ENABLED=0 GOOS=linux go build -o $(E2E_DIR)/bin/$(BINARY_NAME) .
	@echo "Built $(E2E_DIR)/bin/$(BINARY_NAME)"

##@ Logs

.PHONY: logs
logs: ## Tail logs (all services)
	$(COMPOSE) logs -f

.PHONY: logs-atlantis
logs-atlantis: ## Tail Atlantis logs
	$(COMPOSE) logs -f atlantis

.PHONY: logs-gitea
logs-gitea: ## Tail Gitea logs
	$(COMPOSE) logs -f gitea

##@ Debugging

.PHONY: status
status: ## Show container status
	@$(COMPOSE) ps

.PHONY: shell-atlantis
shell-atlantis: ## Open a shell inside the Atlantis container
	docker exec -it e2e-atlantis sh

.PHONY: shell-gitea
shell-gitea: ## Open a shell inside the Gitea container
	docker exec -it e2e-gitea bash

##@ Help

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z0-9_-]+:.*?##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)
