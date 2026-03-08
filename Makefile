.PHONY: help test build run clean lint fmt vet coverage build-mcp

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

test: ## Run all tests
	go test -v -race -coverprofile=coverage.out ./...

test-unit: ## Run unit tests only
	go test -v -short ./tests/unit/...

test-integration: ## Run integration tests only
	go test -v ./tests/integration/...

test-e2e: ## Run E2E tests
	go test -v ./tests/e2e/...

coverage: test ## Generate coverage report
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

build: build-api build-cli build-mcp ## Build all applications

build-api: ## Build the API server
	go build -o bin/weos-api ./cmd/api

build-cli: ## Build the CLI
	go build -o bin/weos-cli ./cmd/cli

build-mcp: ## Build the MCP server
	go build -o bin/weos-mcp ./cmd/mcp

run: ## Run the API server
	go run ./cmd/api

lint: ## Run linter
	golangci-lint run ./...

fmt: ## Format code
	go fmt ./...
	goimports -w .

vet: ## Run go vet
	go vet ./...

clean: ## Clean build artifacts
	rm -rf bin/ coverage.out coverage.html

deps: ## Download dependencies
	go mod download
	go mod tidy

mocks: ## Generate mocks
	moqup ./...
