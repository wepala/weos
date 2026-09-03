.PHONY: help test build run clean lint fmt vet coverage check-release-tag

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

test: ## Run all tests
	go test -v -race -coverprofile=coverage.out ./...

test-unit: ## Run unit tests only
	go test -v -race -short ./tests/unit/...

test-integration: ## Run integration tests only
	go test -v -race ./tests/integration/...

test-e2e: ## Run E2E tests
	go test -v ./tests/e2e/...

test-ui: dev-build-frontend build ## Run Playwright BDD tests against the embedded admin SPA (requires: npx playwright install chromium)
	cd web/admin && npm run test:e2e

coverage: test ## Generate coverage report
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# --- Build version stamping (wm-gt1l) ---
# The binary reports the tag it was built from, rather than a string somebody
# typed. `git describe --tags` names the nearest tag and how far past it this
# build is; `--dirty` says when the tree carried uncommitted changes. That is
# what makes "what version are you running?" answerable from the process.
#
# VERSION is EMPTY when git has no tag to describe — a shallow clone, an export,
# or a fresh repository — and an empty VERSION deliberately passes no -X at all.
# Stamping a blank version would print as `weos version` with nothing after it;
# passing nothing instead leaves the binary to fall back on the build info the
# toolchain records, which reports `dev+<commit>`. See internal/version.
#
# A RELEASE BUILD THEREFORE NEEDS THE TAGS. `git describe` reads tags from the
# local clone, and CI clones shallow and tagless by default, so a release job
# that does not ask for them stamps nothing and ships a binary reporting `dev+`
# while every step stays green. Use actions/checkout with `fetch-depth: 0`, or
# run `git fetch --tags --force` before make. Where no tag can reach the build
# at all — the Docker image, whose .dockerignore excludes .git — pass the
# version in as the VERSION build arg instead.
#
# `?=` is deliberately NOT used here. It would also take an exported VERSION
# out of the environment, and VERSION is a generic enough name that a shell
# profile, a wrapper script or an unrelated CI step may well have one set —
# which would stamp that unrelated string onto the binary and reintroduce the
# very defect this stamping exists to fix, silently. Testing $(origin) instead
# keeps `make build VERSION=v3.2.0` working, because only a value given on the
# command line beats `git describe`.
ifneq ($(origin VERSION),command line)
VERSION     := $(shell git describe --tags --dirty 2>/dev/null)
endif
VERSION_PKG := github.com/wepala/weos/v3/internal/version
GO_LDFLAGS  := $(if $(strip $(VERSION)),-X $(VERSION_PKG).version=$(strip $(VERSION)))

build: ## Build the weos binary
	@test -f web/dist/index.html || { \
		echo "web/dist/index.html is missing, so this binary would ship no SPA."; \
		echo "(//go:embed all:dist only needs web/dist to be non-empty, which the"; \
		echo " tracked web/dist/PLACEHOLDER already guarantees. index.html is a"; \
		echo " separate requirement: it is what makes the served UI work.)"; \
		echo "Run 'make dev-build-frontend' first."; \
		echo "If 'git status' also shows web/dist/PLACEHOLDER deleted, an older build removed it:"; \
		echo "  git checkout -- web/dist/PLACEHOLDER"; \
		exit 1; }
	go build -ldflags "$(GO_LDFLAGS)" -o bin/weos ./cmd/weos

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

# web/dist/PLACEHOLDER is tracked — .gitignore ignores web/dist/* and un-ignores
# that one entry — because `//go:embed all:dist` in web/embed.go does not compile
# against an empty directory. The placeholder is the only reason the module
# builds for a consumer who never runs this target. So clear the generated
# contents and leave the directory and the placeholder alone: `rm -rf web/dist`
# here left a tracked file deleted in every developer's `git status`, and
# committing that deletion broke the build for every downstream consumer while
# still passing locally, because the author's own web/dist was full at the time.
# The exclusion matches by -path, not -name: -name would also spare a nested
# PLACEHOLDER left by a previous build, and how that breaks depends on the find
# make happens to get. BSD find — /usr/bin/find, the one it gets on macOS —
# exits 0 and silently leaves web/dist/_nuxt/ and everything in it behind; GNU
# find and bfs abort the recipe with "Directory not empty". Stale output either
# way, so match the one tracked path.
#
# A tree where the old recipe already deleted the placeholder does not heal
# itself: sparing a file only helps a file that is still there. So restore it
# first. `touch` would leave a modified file rather than a clean one, hence the
# checkout; the trailing `|| true` keeps a build from a tarball with no git
# working.
dev-build-frontend: ## Build Nuxt frontend into web/dist/
	cd web/admin && npx nuxt generate
	test ! -L web/dist || { echo "web/dist is a symlink; find cannot sweep through one, so stale output would pile up unnoticed"; exit 1; }
	mkdir -p web/dist
	test -f web/dist/PLACEHOLDER || git checkout -- web/dist/PLACEHOLDER 2>/dev/null || true
	find web/dist -mindepth 1 ! -path web/dist/PLACEHOLDER -delete
	cp -r web/admin/.output/public/. web/dist/

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
	CGO_LDFLAGS="$(CGO_LDFLAGS_EMBEDDED)" go build -tags oxigraph_embedded -ldflags "$(GO_LDFLAGS)" -o bin/weos ./cmd/weos

test-graph-embedded: fetch-oxigraph-lib ## Test the embedded oxigraph backend (CGO + vendored lib): unit + godog acceptance
	CGO_LDFLAGS="$(CGO_LDFLAGS_EMBEDDED)" go test -race -tags oxigraph_embedded ./infrastructure/graph/...
	CGO_LDFLAGS="$(CGO_LDFLAGS_EMBEDDED)" go test -race -tags oxigraph_embedded ./tests/e2e/ -run 'KnowledgeGraph'

# --- Release tagging (wm-1jkb) ---
# A semver pre-release identifier that is not purely numeric is compared as a
# STRING, so `v3.0.1-alpha21` sorts BELOW `v3.0.1-alpha9` and `go get -u` never
# reaches it. The number therefore has to be its own dot-separated identifier.
# `alpha.N` cannot rescue the v3.0.1 line — it makes the first identifier
# `alpha`, a shorter prefix of `alpha9`, which still sorts first — so the line
# moves to `beta.N`. RELEASE_TAG_PREFIX is the single source of truth for the
# scheme: tests/unit/release_tag_test.go reads it from here and proves what it
# declares sorts above every published v3 tag. See CONTRIBUTING.md, "Cutting a
# release", and docs/decisions/release-tag-scheme.md.
#
# The number is matched as `[1-9][0-9]*`, not `[0-9]+`: semver forbids a leading
# zero in a numeric identifier, so `v3.0.1-beta.01` is not a valid version at
# all and the Go toolchain drops it — published, cached by the proxy, and
# reachable by nobody. The guard has to refuse what Go cannot see.
#
# $(strip) because make keeps trailing whitespace in a `:=` value, and an inline
# comment after the value leaves a space behind as well. Unstripped, that space
# lands in the middle of the pattern and the guard refuses the correct tag with
# a message whose only clue is a character you cannot see.
RELEASE_TAG_PREFIX  := v3.0.1-beta.
RELEASE_TAG_PATTERN := ^$(subst .,\.,$(strip $(RELEASE_TAG_PREFIX)))[1-9][0-9]*$$

# TAG reaches the recipe through the ENVIRONMENT, never through Make's textual
# substitution. `$(TAG)` inside a quoted shell word is pasted in verbatim, so a
# value carrying a quote and a semicolon closes the string and runs whatever
# follows. Whoever supplies TAG is already running make, so the severity is low
# — but this is a public repository and recipes get copied out of it.
#
# A target-specific `check-release-tag: export TAG` would scope this more
# tightly. It needs make 4.x: GNU Make 3.81, which is what macOS still ships,
# reads that line as a rule with a prerequisite named `export` and stops. This
# file-scope directive works on both.
export TAG

check-release-tag: ## Check a release tag against the scheme and re-prove the scheme sorts (make check-release-tag TAG=v3.0.1-beta.1)
	@test -n "$$TAG" || { \
		echo "check-release-tag: name the tag, e.g. make check-release-tag TAG=$(strip $(RELEASE_TAG_PREFIX))1"; \
		exit 1; }
	@printf '%s\n' "$$TAG" | grep -Eq '$(RELEASE_TAG_PATTERN)' || { \
		echo "check-release-tag: refusing \"$$TAG\"."; \
		echo ""; \
		echo "  A release tag on this line is $(strip $(RELEASE_TAG_PREFIX))N, with a literal"; \
		echo "  period before the number and no leading zero on it:"; \
		echo "      good  $(strip $(RELEASE_TAG_PREFIX))1   $(strip $(RELEASE_TAG_PREFIX))10   $(strip $(RELEASE_TAG_PREFIX))22"; \
		echo "      bad   v3.0.1-alpha22   v3.0.1-beta22   v3.0.1-alpha022   v3.0.1-alpha.22   v3.0.1-beta.01"; \
		echo ""; \
		echo "  Without the period the identifier is compared as a string, so alpha21"; \
		echo "  sorts below alpha9 and 'go get -u' never sees the tag. With it, but"; \
		echo "  still under alpha, the first identifier becomes a prefix of alpha9 and"; \
		echo "  sorts below it for the same reason. A leading zero is not valid semver"; \
		echo "  at all, so the Go toolchain ignores such a tag entirely."; \
		echo ""; \
		echo "  If the tag you named is the right shape on a DIFFERENT version line"; \
		echo "  (v3.1.0-beta.1, say, or the final v3.0.1), this check is not the thing"; \
		echo "  to work around: the version is pinned on purpose. Move the line by"; \
		echo "  editing RELEASE_TAG_PREFIX in the Makefile, which re-proves the sort"; \
		echo "  order against every published tag."; \
		echo ""; \
		echo "  See CONTRIBUTING.md, \"Cutting a release\"."; \
		exit 1; }
# The shape check above proves the tag matches the prefix. It cannot prove the
# PREFIX is right, because it derives its pattern from that same prefix — a
# prefix typo produces a matching pattern and passes silently. So re-prove the
# ordering here, at the one moment ordering matters. WEOS_IN_CHECK_RELEASE_TAG
# tells the guard test in that package to skip, so it cannot re-enter this
# target and recurse.
	@WEOS_IN_CHECK_RELEASE_TAG=1 go test -count=1 ./tests/unit/ -run 'ReleaseTag|Scheme' || { \
		echo ""; \
		echo "check-release-tag: \"$$TAG\" has the right SHAPE, but the scheme the"; \
		echo "  Makefile declares no longer sorts above every published tag. The shape"; \
		echo "  check alone cannot see this — it derives its pattern from the same"; \
		echo "  RELEASE_TAG_PREFIX it is checking. Fix the prefix before tagging."; \
		exit 1; }
	@echo "check-release-tag: $$TAG is a valid release tag."
