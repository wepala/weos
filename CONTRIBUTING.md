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

## Contributor License Agreement

> **Draft for legal review.**

weos is dual-licensed — AGPL v3 or (at your option) any later version, or a
commercial license from Wepala, LLC (see `LICENSING.md`). So that Wepala can
license every contribution both ways, each contributor must agree to the
[Individual Contributor License Agreement](CLA.md) once it comes into force.

- To agree, put the three lines from section 5 of `CLA.md` (statement, name,
  email) in the description of your first pull request. The pull request
  template has them. If you contributed before the CLA came into force, put
  them in your next pull request.
- From the date the CLA comes into force, a pull request from a contributor
  who has not agreed is not merged. Until that date, pull requests are not
  held for the CLA.
- If you contribute for your employer, make sure it has allowed you to sign.
