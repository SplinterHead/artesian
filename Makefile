# ==============================================================================
# Docker Compose Management Makefile
# ==============================================================================

COMPOSE ?= docker compose

.DEFAULT_GOAL := help

.PHONY: help up start down stop restart build run logs ps status pull clean clean-data wipe-data reset format format-go format-node lint lint-go lint-node check

## ----------------------------------------------------------------------
## Help
## ----------------------------------------------------------------------
help: ## Display this help screen
	@echo "Usage: make [target]"
	@echo ""
	@echo "Available targets:"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

## ----------------------------------------------------------------------
## Lifecycle Targets
## ----------------------------------------------------------------------
up: ## Start the stack in detached mode
	$(COMPOSE) up -d --build

start: up ## Alias for 'up'

down: ## Stop and remove stack containers and networks
	$(COMPOSE) down

stop: ## Stop running containers without removing them
	$(COMPOSE) stop

restart: ## Restart all containers in the stack
	$(COMPOSE) restart

build: ## Build or rebuild container images
	$(COMPOSE) build

run: ## Rebuild images and restart the stack in detached mode
	$(COMPOSE) up -d

## ----------------------------------------------------------------------
## Monitoring & Maintenance Targets
## ----------------------------------------------------------------------
logs: ## Follow live logs from all containers
	$(COMPOSE) logs -f

ps: ## View the status of containers in the stack
	$(COMPOSE) ps

status: ps ## Alias for 'ps'

pull: ## Pull the latest images for defined services
	$(COMPOSE) pull

clean: ## Stop and remove containers, networks, volumes, and orphan containers
	$(COMPOSE) down -v --remove-orphans

clean-data: ## Wipe all local cached game content and database state
	@echo "Stopping stack..."
	$(COMPOSE) down
	@echo "Removing local cache and database files..."
	rm -rf ./data/cache ./data/db ./data/*.db*
	mkdir -p ./data/cache ./data/db
	@echo "All local data successfully cleaned."
	sleep 2

wipe-data: clean-data ## Alias for clean-data

reset: clean-data run ## Wipe local data and restart the stack fresh
 
## ----------------------------------------------------------------------
## Code Quality & Formatting Targets
## ----------------------------------------------------------------------
format: format-go format-node ## Format all Go and Node/Vue code

format-go: ## Format Go source code using gofmt
	@echo "Formatting Go source files..."
	@if command -v gofmt >/dev/null 2>&1; then \
		gofmt -s -w cmd internal; \
	else \
		docker run --rm -v "$$(pwd)":/build -w /build golang:1.24-alpine gofmt -s -w cmd internal; \
	fi
	@echo "✓ Go formatting complete."

format-node: ## Format Node/Vue source code using Prettier
	@echo "Formatting Node/Vue source files..."
	@cd web && npm run format
	@echo "✓ Node/Vue formatting complete."

lint: lint-go lint-node ## Lint all Go and Node/Vue code

lint-go: ## Lint Go packages using go vet
	@echo "Linting Go source files..."
	@if command -v go >/dev/null 2>&1; then \
		go vet ./cmd/... ./internal/...; \
	else \
		docker run --rm -v "$$(pwd)":/build -w /build golang:1.24-alpine go vet ./cmd/... ./internal/...; \
	fi
	@echo "✓ Go linting passed."

lint-node: ## Lint Node/Vue source code using ESLint
	@echo "Linting Node/Vue source files..."
	@cd web && npm run lint
	@echo "✓ Node/Vue linting passed."

check: lint ## Run code quality and linting checks across all code


