GO ?= go
BIN_DIR ?= ./bin
BIN ?= $(BIN_DIR)/pop
MCP_PROMPT ?= docs/mcp-webconsole-smoke.prompt.md

DOCKER ?= docker
DOCKER_IMAGE ?= pop
GIT_DESCRIBE ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo unknown)
DOCKER_VERSION ?= $(GIT_DESCRIBE)
DOCKER_IMAGE_REF ?= $(DOCKER_IMAGE):$(DOCKER_VERSION)

.PHONY: help build build-linux-arm64 docker-build run run-bg stop test test-console test-integration fmt vet tidy lint clean mcp-smoke mcp-smoke-path

help: ## Show available commands
	@awk 'BEGIN {FS = ":.*## "; printf "\nUsage:\n  make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_.-]+:.*## / {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build POP binary to ./bin/pop
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN) ./cmd/pop

build-linux-arm64: ## Build Linux ARM64 binary to ./bin/pop-linux-arm64
	@mkdir -p $(BIN_DIR)
	GOOS=linux GOARCH=arm64 $(GO) build -o $(BIN_DIR)/pop-linux-arm64 ./cmd/pop

docker-build: ## Build Docker image tagged with git tag + commit id
	$(DOCKER) build -t $(DOCKER_IMAGE_REF) .
	@echo "Built image: $(DOCKER_IMAGE_REF)"

run: ## Run POP with defaults/env/args
	$(GO) run ./cmd/pop $(ARGS)

run-bg: ## Run POP in background, write pid/log
	@mkdir -p .tmp
	@nohup $(GO) run ./cmd/pop $(ARGS) > .tmp/pop.log 2>&1 & echo $$! > .tmp/pop.pid
	@echo "POP started: pid=$$(cat .tmp/pop.pid), log=.tmp/pop.log"

stop: ## Stop background POP started by run-bg
	@if [ -f .tmp/pop.pid ]; then \
		kill "$$(cat .tmp/pop.pid)" 2>/dev/null || true; \
		rm -f .tmp/pop.pid; \
		echo "POP stopped"; \
	else \
		echo "No .tmp/pop.pid found"; \
	fi

test: ## Run all tests
	$(GO) test ./...

test-console: ## Run web console integration tests only
	$(GO) test ./integration -run TestConsole -v

test-integration: ## Run all integration tests
	$(GO) test ./integration -v

fmt: ## Format Go code
	$(GO) fmt ./...

vet: ## Run go vet checks
	$(GO) vet ./...

tidy: ## Tidy go modules
	$(GO) mod tidy

lint: fmt vet test ## Run basic local quality checks

clean: ## Remove build and temp artifacts
	rm -rf $(BIN_DIR) .tmp

mcp-smoke-path: ## Print MCP smoke prompt file path
	@echo $(MCP_PROMPT)

mcp-smoke: ## Print MCP smoke prompt for OpenCode
	@echo "Paste this prompt into OpenCode:"
	@echo
	@sed -n '/^```text$$/,/^```$$/p' $(MCP_PROMPT) | sed '1d;$$d'
