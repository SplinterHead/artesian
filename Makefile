# ==============================================================================
# Docker Compose Management Makefile
# ==============================================================================

COMPOSE ?= docker compose

.DEFAULT_GOAL := help

.PHONY: help up start down stop restart build run logs ps status pull clean clean-data wipe-data reset

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

