GO ?= go
CONFIG ?= ./pop.example.json
BIN_DIR ?= ./bin
BIN ?= $(BIN_DIR)/pop
MCP_PROMPT ?= docs/mcp-webconsole-smoke.prompt.md

.PHONY: help build run run-bg stop test test-console test-integration fmt vet tidy lint clean mcp-smoke mcp-smoke-path

help: ## Show available commands
	@awk 'BEGIN {FS = ":.*## "; printf "\nUsage:\n  make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_.-]+:.*## / {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build POP binary to ./bin/pop
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN) ./cmd/pop

run: ## Run POP with example config
	$(GO) run ./cmd/pop -config $(CONFIG)

run-bg: ## Run POP in background, write pid/log
	@mkdir -p .tmp
	@nohup $(GO) run ./cmd/pop -config $(CONFIG) > .tmp/pop.log 2>&1 & echo $$! > .tmp/pop.pid
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
