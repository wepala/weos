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
