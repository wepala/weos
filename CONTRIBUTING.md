# Contributing

## Development Workflow

1. Create a feature branch from `main`
2. Write tests first (TDD approach)
3. Implement the feature
4. Ensure all tests pass
5. Run linters and formatters
6. Submit a pull request

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
- Aim for >80% test coverage

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
