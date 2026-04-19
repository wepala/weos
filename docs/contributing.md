---
title: Contributing
layout: default
nav_order: 7
---

# Contributing to WeOS

Thank you for your interest in contributing to WeOS.

## Development Workflow

1. Fork the repository and clone your fork
2. Create a feature branch from `main`
3. Make your changes following the code standards below
4. Write or update tests for your changes
5. Run `make test` to ensure all tests pass
6. Run `make lint` to check for linting issues
7. Submit a pull request against `main`

## Code Standards

- Follow Go conventions and idioms
- Aim for functions under 100 lines / 50 statements
- Aim for maximum line length of 120 characters
- Aim for cyclomatic complexity under 15
- Use `goimports` for import formatting (`make fmt`)
- Enabled linters: `errcheck`, `govet`, `ineffassign`, `staticcheck`, `unused`, `misspell`

## Testing

- Unit tests go in `tests/unit/`
- Integration tests go in `tests/integration/`
- End-to-end tests go in `tests/e2e/`
- Run the full suite: `make test` (includes race detection and coverage)
- Run a single test: `go test -v -run TestName ./path/to/package`

## Event Sourcing Patterns

When working with domain entities:

- Never persist entities directly — always use `SimpleUnitOfWork`
- Events are immutable — never modify events after creation
- Event handlers must be idempotent — they may be replayed
- Record events in entity methods, not in services

## Architecture

Dependencies point inward: `API -> Application -> Domain <- Infrastructure`

See the [Architecture]({% link architecture/README.md %}) page and [Explanation]({% link _explanation/index.md %}) section for details.
