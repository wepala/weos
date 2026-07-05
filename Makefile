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

test-ui: dev-build-frontend build ## Run Playwright BDD tests against the embedded admin SPA (requires: npx playwright install chromium)
	cd web/admin && npm run test:e2e

coverage: test ## Generate coverage report
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

build: ## Build the weos binary
	@test -f web/dist/index.html || { \
		echo "web/dist/index.html is missing (web/dist is not checked in; //go:embed all:dist needs it)."; \
		echo "Run 'make dev-build-frontend' first."; \
		exit 1; }
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

dev-test-ui: dev-build-frontend build dev-seed ## Run Playwright UI tests (headless)
	cd tests/browser && npx playwright test

dev-clean: ## Remove dev database, seed manifest, and build artifacts
	rm -f weos.db .dev-seed.json
	rm -rf bin/

# --- Embedded oxigraph knowledge-graph backend (story #422) ---
# The graph can run from an in-process oxigraph store (no sidecar) when built
# with the `oxigraph_embedded` tag. That needs CGO and a per-platform static
# lib (liboxigraph_ffi.a) vendored from the binding's GitHub release — it is
# not in the Go module (too large), so fetch it here. Without the tag weos
# builds pure-Go and the graph falls back to nop.
OXIGRAPH_LIB_VERSION ?= go/v0.1.0-alpha.3
OXIGRAPH_LIB_DIR      := infrastructure/graph/oxigraph/lib
OXIGRAPH_PLATFORM     := $(shell go env GOOS)_$(shell go env GOARCH)
CGO_LDFLAGS_EMBEDDED  := -L$(abspath $(OXIGRAPH_LIB_DIR))/$(OXIGRAPH_PLATFORM)
# sha256 checker: Linux ships sha256sum, macOS ships shasum. Pick whichever is
# present so the fetch target's verify step works on both (both read the
# checksum list from stdin with `-c -`).
SHA256SUM_CHECK       := $(shell command -v sha256sum >/dev/null 2>&1 && echo 'sha256sum -c -' || echo 'shasum -a 256 -c -')

fetch-oxigraph-lib: ## Download + sha-verify liboxigraph_ffi.a for this platform (for -tags oxigraph_embedded)
	@mkdir -p $(OXIGRAPH_LIB_DIR)/$(OXIGRAPH_PLATFORM) /tmp/oxigraph-lib
	@echo "fetching liboxigraph_ffi for $(OXIGRAPH_PLATFORM) ($(OXIGRAPH_LIB_VERSION))"
	@gh release download "$(OXIGRAPH_LIB_VERSION)" --repo akeemphilbert/oxigraph \
		--pattern "liboxigraph_ffi_$(OXIGRAPH_PLATFORM).a.gz" --pattern "SHA256SUMS" \
		--dir /tmp/oxigraph-lib --clobber
	@cd /tmp/oxigraph-lib && grep "liboxigraph_ffi_$(OXIGRAPH_PLATFORM).a.gz" SHA256SUMS | $(SHA256SUM_CHECK)
	@gunzip -f /tmp/oxigraph-lib/liboxigraph_ffi_$(OXIGRAPH_PLATFORM).a.gz
	@cp /tmp/oxigraph-lib/liboxigraph_ffi_$(OXIGRAPH_PLATFORM).a \
		$(OXIGRAPH_LIB_DIR)/$(OXIGRAPH_PLATFORM)/liboxigraph_ffi.a

build-embedded: fetch-oxigraph-lib ## Build weos with the embedded oxigraph backend
	CGO_LDFLAGS="$(CGO_LDFLAGS_EMBEDDED)" go build -tags oxigraph_embedded -o bin/weos ./cmd/weos

test-graph-embedded: fetch-oxigraph-lib ## Test the embedded oxigraph backend (CGO + vendored lib): unit + godog acceptance
	CGO_LDFLAGS="$(CGO_LDFLAGS_EMBEDDED)" go test -tags oxigraph_embedded ./infrastructure/graph/...
	CGO_LDFLAGS="$(CGO_LDFLAGS_EMBEDDED)" go test -tags oxigraph_embedded ./tests/e2e/ -run 'KnowledgeGraph'
