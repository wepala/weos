.PHONY: help test build run clean lint fmt vet coverage

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

build: ## Build the weos binary
	go build -o bin/weos ./cmd/weos

run: ## Run the API server
	go run ./cmd/weos serve

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

dev-seed: build ## Seed database with test users, presets, and sample data
	./bin/weos seed

dev-serve: build ## Run server in dev mode (no OAuth required)
	GOOGLE_CLIENT_ID= GOOGLE_CLIENT_SECRET= ./bin/weos serve

dev-setup: dev-seed dev-serve ## Full dev setup: seed then start server

dev-test-api: build dev-seed ## Run Newman API regression tests (requires: npm install -g newman)
	GOOGLE_CLIENT_ID= GOOGLE_CLIENT_SECRET= ./bin/weos serve & \
	SERVER_PID=$$!; \
	sleep 2; \
	newman run tests/newman/tasks-api.postman_collection.json \
		-e tests/newman/dev-environment.json \
		--color on; \
	EXIT_CODE=$$?; \
	kill $$SERVER_PID 2>/dev/null; \
	exit $$EXIT_CODE

dev-build-frontend: ## Build Nuxt frontend into web/dist/
	cd web/admin && npx nuxt generate
	rm -rf web/dist && cp -r web/admin/.output/public web/dist

dev-test-ui: build dev-build-frontend dev-seed ## Run Playwright UI tests (headless)
	cd tests/browser && npx playwright test

dev-clean: ## Remove dev database, seed manifest, and build artifacts
	rm -f weos.db .dev-seed.json
	rm -rf bin/
