# ==============================================================================
# User Service — Makefile
# ==============================================================================

# ---- Config ------------------------------------------------------------------
APP_NAME        := user-service
BINARY          := bin/$(APP_NAME)
MAIN_PKG        := .
GO              := go
GOFLAGS         :=
VERSION         ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT          ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_TIME      ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS         := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_TIME)

# ---- Docker ------------------------------------------------------------------
DOCKER_IMAGE    ?= $(APP_NAME)
DOCKER_TAG      ?= $(VERSION)
DOCKERFILE      ?= Dockerfile

# ---- Redis -------------------------------------------------------------------
REDIS_PORT      ?= 6379
REDIS_CONTAINER ?= $(APP_NAME)-redis

# ---- Tools (installed to ./bin if missing) -----------------------------------
TOOLS_BIN       := $(PWD)/bin
GOLANGCI_LINT   := $(TOOLS_BIN)/golangci-lint
GOTESTSUM       := $(TOOLS_BIN)/gotestsum
AIR             := $(TOOLS_BIN)/air

# ==============================================================================
# Help (default target)
# ==============================================================================
.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} \
		/^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

# ==============================================================================
# Build
# ==============================================================================
.PHONY: build
build: ## Build the binary
	@mkdir -p bin
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BINARY) $(MAIN_PKG)
	@echo "built $(BINARY) (version=$(VERSION) commit=$(COMMIT))"

.PHONY: build-linux
build-linux: ## Cross-compile for linux/amd64
	@mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BINARY)-linux-amd64 $(MAIN_PKG)

.PHONY: install
install: ## Install binary to $GOPATH/bin
	$(GO) install $(GOFLAGS) -ldflags "$(LDFLAGS)" $(MAIN_PKG)

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf bin/ coverage.out coverage.html

# ==============================================================================
# Run / Dev
# ==============================================================================
.PHONY: run
run: ## Run the app locally
	$(GO) run $(MAIN_PKG)

.PHONY: dev
dev: $(AIR) ## Run with hot reload (air)
	$(AIR) -c .air.toml

.PHONY: worker
worker: ## Run the asynq worker (if split into its own cmd)
	$(GO) run ./cmd/worker

# ==============================================================================
# Test
# ==============================================================================
.PHONY: test
test: ## Run tests
	$(GO) test $(GOFLAGS) -race -count=1 ./...

.PHONY: test-verbose
test-verbose: ## Run tests with verbose output
	$(GO) test $(GOFLAGS) -race -count=1 -v ./...

.PHONY: test-cover
test-cover: ## Run tests with coverage report
	$(GO) test $(GOFLAGS) -race -count=1 -coverprofile=coverage.out -covermode=atomic ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@$(GO) tool cover -func=coverage.out | tail -1

.PHONY: test-short
test-short: ## Run tests skipping long ones
	$(GO) test $(GOFLAGS) -short -count=1 ./...

.PHONY: bench
bench: ## Run benchmarks
	$(GO) test $(GOFLAGS) -bench=. -benchmem -run=^$$ ./...

.PHONY: test-integration
test-integration: ## Run integration tests (requires REDIS_ADDR)
	REDIS_ADDR=$${REDIS_ADDR:-localhost:$(REDIS_PORT)} \
		$(GO) test $(GOFLAGS) -race -count=1 -tags=integration ./...

# ==============================================================================
# Lint / Format
# ==============================================================================
.PHONY: fmt
fmt: ## Format code
	$(GO) fmt ./...
	@command -v gofumpt >/dev/null && gofumpt -w . || true

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: lint
lint: $(GOLANGCI_LINT) ## Run golangci-lint
	$(GOLANGCI_LINT) run ./...

.PHONY: lint-fix
lint-fix: $(GOLANGCI_LINT) ## Run golangci-lint with auto-fix
	$(GOLANGCI_LINT) run --fix ./...

.PHONY: tidy
tidy: ## go mod tidy + verify
	$(GO) mod tidy
	$(GO) mod verify

.PHONY: check
check: tidy fmt vet lint test ## Run all checks

# ==============================================================================
# Redis (local dev)
# ==============================================================================
.PHONY: redis-up
redis-up: ## Start local Redis in Docker
	@docker run -d --name $(REDIS_CONTAINER) \
		-p $(REDIS_PORT):6379 \
		redis:7-alpine \
		redis-server --appendonly yes
	@echo "redis running at localhost:$(REDIS_PORT)"

.PHONY: redis-down
redis-down: ## Stop and remove local Redis
	@docker rm -f $(REDIS_CONTAINER) 2>/dev/null || true

.PHONY: redis-cli
redis-cli: ## Open redis-cli against local Redis
	@docker exec -it $(REDIS_CONTAINER) redis-cli

.PHONY: redis-flush
redis-flush: ## Flush local Redis (DANGER: wipes data)
	@docker exec -it $(REDIS_CONTAINER) redis-cli FLUSHALL

# ==============================================================================
# Docker
# ==============================================================================
.PHONY: docker-build
docker-build: ## Build Docker image
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_TIME=$(BUILD_TIME) \
		-t $(DOCKER_IMAGE):$(DOCKER_TAG) \
		-t $(DOCKER_IMAGE):latest \
		-f $(DOCKERFILE) .

.PHONY: docker-run
docker-run: ## Run Docker image (needs Redis reachable)
	docker run --rm -p 8080:8080 \
		-e REDIS_ADDR=host.docker.internal:$(REDIS_PORT) \
		$(DOCKER_IMAGE):$(DOCKER_TAG)

.PHONY: docker-push
docker-push: ## Push Docker image
	docker push $(DOCKER_IMAGE):$(DOCKER_TAG)
	docker push $(DOCKER_IMAGE):latest

# ==============================================================================
# Docker Compose
# ==============================================================================
.PHONY: up
up: ## docker compose up -d
	docker compose up -d --build

.PHONY: down
down: ## docker compose down
	docker compose down

.PHONY: logs
logs: ## Tail compose logs
	docker compose logs -f --tail=100

.PHONY: ps
ps: ## Show compose services
	docker compose ps

# ==============================================================================
# Database / Migrations (placeholder — adjust to your tool)
# ==============================================================================
.PHONY: migrate-up
migrate-up: ## Run DB migrations up
	@echo "TODO: wire up your migration tool (goose, migrate, atlas...)"

.PHONY: migrate-down
migrate-down: ## Roll back last migration
	@echo "TODO: wire up your migration tool"

# ==============================================================================
# Tools (auto-install to ./bin)
# ==============================================================================
$(GOLANGCI_LINT):
	@mkdir -p $(TOOLS_BIN)
	GOBIN=$(TOOLS_BIN) $(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

$(GOTESTSUM):
	@mkdir -p $(TOOLS_BIN)
	GOBIN=$(TOOLS_BIN) $(GO) install gotest.tools/gotestsum@latest

$(AIR):
	@mkdir -p $(TOOLS_BIN)
	GOBIN=$(TOOLS_BIN) $(GO) install github.com/air-verse/air@latest

.PHONY: tools
tools: $(GOLANGCI_LINT) $(GOTESTSUM) $(AIR) ## Install dev tools
	@echo "tools installed in $(TOOLS_BIN)"