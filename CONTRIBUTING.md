# Contributing

Read [`constitution.md`](constitution.md) first. It states the non-negotiable rules
for this service. Where this file conflicts with an article of the constitution, the
article wins.

## Development Workflow

1. Create a feature branch from `v3` — the integration branch for this service
2. Write the acceptance scenarios first, and get them confirmed (Article IX)
3. Write tests, then implement the feature
4. Ensure all tests pass — `make test`
5. Run linters and formatters — `make lint`, `make fmt`
6. Submit a pull request against `v3`. Stacking onto another feature branch is fine.
   Never push directly to `v3`, `develop`, or `main` (Article XIV)

## Cutting a release

Releases on the `v3` line are to be tagged **`v3.0.1-beta.N`** — `beta`, a
literal period, then the number, with no leading zero on it. `v3.0.1-beta.1`
is the first tag in this scheme and has not been cut yet: until it is, the
newest tag on the line is still `v3.0.1-alpha21` and `go get -u` still resolves
to `v3.0.1-alpha9`. Cutting that first tag is tracked as beads `wm-o53y`.

To cut a release:

```bash
# 1. Fetch every published tag, so the next number is worked out from the real
#    tip rather than from a stale local view. Minting an N that already exists
#    on another commit gets the push rejected — or, if it is forced, publishes a
#    mismatch the module proxy will cache and never let you correct.
git fetch --tags

# 2. Find the newest tag in the scheme. `--sort=-v:refname` sorts by version;
#    the obvious `git tag -l 'v3.0.1-beta.*' | tail -1` sorts LEXICALLY and so
#    reports beta.9 as the newest once beta.10 exists — the same trap this
#    scheme exists to close, rebuilt in the procedure.
git tag --list 'v3.0.1-beta.*' --sort=-v:refname | head -1

# 3. Check the tag you are about to create, and re-prove the scheme sorts.
make check-release-tag TAG=v3.0.1-beta.2

# 4. Tag the commit and publish the tag.
git tag v3.0.1-beta.2
git push origin v3.0.1-beta.2
```

The check refuses anything outside the scheme, and then runs the ordering tests
so a tag is never cut against a scheme that has stopped sorting. `Makefile`'s
`RELEASE_TAG_PREFIX` is the single source of truth for what the scheme is;
`tests/unit/release_tag_test.go` reads it from there, fails if what it declares
would not sort above every tag already published, and shells back out to
`make check-release-tag` so the guard's own pattern is exercised too.

Moving the line — to `v3.1.0-beta.N`, or to the final `v3.0.1` that ends it —
means editing `RELEASE_TAG_PREFIX`. The check pins the version as well as the
shape, deliberately, so the sort order is re-proven whenever the line moves; a
correctly shaped tag on a different line is refused until you make that edit.

### Why the period matters

Semver compares pre-release identifiers one dot-separated piece at a time, and
a piece that is not purely numeric is compared as a **string**. `alpha21` is a
single string identifier, so it sorts *below* `alpha9` — `2` precedes `9`
lexically. Under the old scheme every tag from `alpha10` to `alpha21` therefore
sorted below `alpha9`, and `go get -u github.com/wepala/weos/v3` resolved to
`alpha9`, twelve tags stale, reporting no error. Pseudo-versions inherit the
same base, so a commit on today's tip read as a downgrade.

Splitting the number into its own identifier makes it compare numerically,
which is what `beta.N` does. It has to be `beta` rather than `alpha`: with
`alpha.22` the first identifier becomes plain `alpha`, which is a shorter
prefix of the existing `alpha9` and `alpha21`, and a shorter prefix sorts
first — so `v3.0.1-alpha.22` still loses to `v3.0.1-alpha9`. Zero-padding
(`alpha022`) loses for the same reason. `docs/decisions/release-tag-scheme.md`
records the options and the measurements.

### The old tags stay

**Never rename or delete a published `v3.0.1-alphaN` tag.** A module proxy has
already cached them, and a consumer pinned to one would break. They simply sit
below every `beta.N` tag from now on, which is harmless once nothing new is
published in the old shape.

## Code Standards

- Follow Go best practices and idioms
- Use `go fmt` and `goimports` for formatting
- Write table-driven tests for exported functions
- Use interfaces for dependency injection
- Document all public functions with GoDoc comments

## Testing

- Write unit tests in `tests/unit/`
- Write integration tests in `tests/integration/`
- Write E2E tests in `tests/e2e/` using Godog/Gherkin
- Use `go test -race` to detect race conditions
- Do not let coverage regress; explain any drop in the PR body (Article XII)

## Event Sourcing Patterns

- Domain entities embed `*ddd.BaseEntity`
- All state changes recorded as events via `RecordEvent()`
- Services use `SimpleUnitOfWork` for atomic persistence
- Event handlers must be idempotent
- Never persist entities directly - always use UnitOfWork

## Observability

- Use OpenTelemetry for tracing
- Include context in all function signatures
- Log with appropriate levels (info, warn, error)
- Include request IDs and trace context in logs
