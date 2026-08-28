# WeOS Core Constitution

**Version:** 1.1.0 &nbsp;|&nbsp; **Ratified:** 2026-08-27 &nbsp;|&nbsp; **Last amended:** 2026-08-27

This document states the non-negotiable rules for `services/core`. Every
contributor is bound by it — human or agent. Where an article conflicts with
`CLAUDE.md`, `CONTRIBUTING.md`, a skill, or a habit, the article wins. Change an
article only through the amendment procedure in [Governance](#governance).

Each article gives the rule, the reason, and how the rule is enforced. An article
marked **(ASPIRATIONAL)** is binding on reviewers but has no automated gate yet.

---

## Article I — Never persist entities directly

**Rule.** All state changes record an event on the entity and commit through a
`UnitOfWork`. Events are immutable once created. Event handlers are idempotent and
survive replay.

**Reason.** The event store is the source of truth. A direct write bypasses the
store, so the projection and the history disagree. Replay then produces a different
system than the one that ran.

**How this is enforced.** Review. Use `application.NewSimpleUnitOfWork(eventStore,
dispatcher)`, then `uow.Track(entity)` and `uow.Commit(ctx)`. A repository write
that does not pass through a unit of work is a violation.

---

## Article II — Dependencies point inward

**Rule.** API → Application → Domain ← Infrastructure. `domain/` imports no outer
layer. Infrastructure implements interfaces that the domain declares.

**Reason.** The domain must stay testable without a database, an HTTP server, or a
DI container. An inward-pointing import graph is what keeps that true.

**How this is enforced.** The `depguard` linter, via the `domain-points-inward`
rule in `.golangci.yml`. An import of `api/`, `application/`, or
`infrastructure/` inside `domain/` fails `make lint`.

Two waivers exist. `domain/repositories/role_settings_repository.go` and
`role_resource_access_repository.go` still name the GORM persistence model in
their interfaces. Clearing them needs a domain-owned type plus a mapping in the
gorm layer, which is an architectural change and wants its own ADR.

---

## Article III — One binary, config-driven

**Rule.** There is one binary, `cmd/weos`. Every entry point builds a
`config.Config` and passes it to `application.Module(cfg)`. No package reads the
environment on its own.

**Reason.** Configuration read in a leaf package cannot be overridden, tested, or
seen. Centralising it keeps the load order — defaults, `.env`, environment, CLI
flags — predictable.

**How this is enforced.** The `forbidigo` linter. `os.Getenv` and `os.LookupEnv`
outside `internal/config/` fail `make lint`. Add configuration to
`internal/config/config.go` and its `LoadFromEnvironment` method instead.

Seven waivers exist, all deliberate operator overrides at the CLI and provider
edge: `PORT` and `WORKER_RUN_IN_PROCESS` in `serve.go`, the account password
channel in `account.go`, the DSN presence check in `dsn.go`, and the agent
session DSN and script switch in `orchestrator_provider.go`. Each carries its
reason at the call site.

---

## Article IV — Content is ontology-typed

**Rule.** Resources are JSON-LD against a declared `@context`. Every property a
preset defines states a term its vocabulary defines. No undefined predicates.

**Reason.** An undefined predicate is invisible to a query and meaningless to an
LLM. Grounded types are the reason the graph gives useful answers and free SEO
structured data.

**How this is enforced.** The vocabulary contract suites in `tests/e2e/` —
`waived_predicates_retired.feature`, `house_vocabulary_domain.feature`, and
`context_term_adoption.feature`. A property with no defined term fails them.

---

## Article V — Tests are layered and race-checked

**Rule.** Unit tests go in `tests/unit/`. Integration tests go in
`tests/integration/`. Acceptance tests go in `tests/e2e/` as godog Gherkin. All run
under `-race`.

**Reason.** Each layer catches a different defect class. Race detection catches the
concurrency defects that only appear under load in production.

**How this is enforced.** `make test`, `make test-unit`, `make test-integration`,
`make test-graph-embedded`. CI runs `go test -v -race -timeout 20m ./...`.

---

## Article VI — The gates are green before merge

**Rule.** All five CI jobs pass before a pull request merges: `frontend`, `build`,
`graph-embedded`, `test`, and `lint`.

**Reason.** A red gate that merges anyway trains everyone to ignore the gate.

**How this is enforced.** `.github/workflows/ci.yml`. Do not merge on red. Do not
disable a job to go green — fix the cause, or amend this constitution.

---

## Article VII — Code is formatted and lints clean

**Rule.** Code passes the formatters `gofmt` and `goimports`, and the ten enabled
linters:

| Linter | Catches |
|---|---|
| `errcheck` | Unhandled errors |
| `govet` | Suspect constructs the compiler allows |
| `ineffassign` | Assignments nothing reads |
| `staticcheck` | Bugs, dead code, and misuse |
| `unused` | Unreachable identifiers |
| `misspell` | Misspellings (US locale) |
| `gosec` | Weak permissions, unsafe paths, hardcoded secrets |
| `gocritic` | Diagnostic and performance defects |
| `depguard` | Article II — imports that point outward from `domain/` |
| `forbidigo` | Article III — `os.Getenv` outside `internal/config` |

`errcheck`, `gosec`, and `forbidigo` are relaxed in `_test.go` files.
`forbidigo` is also relaxed inside `internal/config/`, which is the one place
allowed to read the environment.

Two `gocritic` checkers are disabled deliberately. `hugeParam` and
`rangeValCopy` tell you to pass Uber Fx `fx.In` structs and `config.Config` by
pointer. Fx requires them by value, and Article III mandates it. Between them
those two account for 205 of the 224 findings the `performance` tag would
otherwise raise.

**Reason.** A consistent format removes formatting from review. Every remaining
rule is worth stopping a build for. Three of these linters enforce an article of
this constitution that review alone used to carry.

**How this is enforced.** `.golangci.yml`, run by `make lint` and the CI `lint`
job. The CI linter version is pinned, so a new upstream check cannot turn the
build red without a deliberate bump.

Do not add a `//nolint` without a comment giving the reason. The tree carries
nine recorded waivers today: two `depguard` (Article II debt in
`domain/repositories/`), and seven `forbidigo` (deliberate operator overrides at
the CLI and provider edge). Fix them as you touch them, and do not add more.

---

## Article VIII — API responses use the envelope

**Rule.** Handlers return through `respond`, `respondPaginated`, `respondError`, or
`respondRaw`. Non-fatal notes reach the client through
`entities.AddMessage(ctx, ...)`. `/health` and static files are the only exemptions.

**Reason.** One response shape means one client parser. Hand-rolled JSON in a
handler breaks every consumer that trusts the envelope.

**How this is enforced.** Review of `api/handlers/`. A direct `c.JSON(...)` in a
handler under `/api` is a violation.

---

## Article IX — The acceptance contract comes before the implementation

**Rule.** Write the Gherkin scenarios for a story first. Get them confirmed. Then
write the code that satisfies them.

**Reason.** Scenarios written after the code describe what was built, not what was
asked for. Confirming the contract first is where a misread requirement is cheap to
fix.

**How this is enforced. (ASPIRATIONAL)** Review. The pull request shows the
`.feature` file in an earlier commit than the implementation, or explains why not.

---

## Article X — An architectural change needs an ADR

**Rule.** Adding a layer, changing the event or projection contract, or swapping a
foundational library requires a record in `docs/decisions/` and a row in
`docs/decisions/index.md`.

**Reason.** The reasoning behind a decision decays faster than the code. Six months
later the ADR is the only thing that says which options were rejected, and why.

**How this is enforced. (ASPIRATIONAL)** Review. Follow the shape of the existing
records in `docs/decisions/`: context, options evaluated, decision, status, date.

---

## Article XI — No silent failures

**Rule.** Handle every error, surface it, or log it with a reason. A deliberately
discarded error carries a comment saying why discarding it is safe.

```go
// Best-effort cache warm; a miss costs a query, not correctness.
_ = cache.Warm(ctx)
```

**Reason.** A swallowed error turns a loud bug into a quiet wrong answer. Quiet
wrong answers in a knowledge graph propagate into every response built on them.

**How this is enforced.** `errcheck` catches the unchecked call. Review catches the
uncommented `_ =`. There are discarded errors in the tree that predate this
article; fix them as you touch them, and do not add more.

---

## Article XII — Coverage does not regress

**Rule.** A pull request that lowers the Codecov number explains why in its body.

**Reason.** Coverage falls one uncovered branch at a time. Naming the drop makes it
a decision instead of an accident.

**How this is enforced. (ASPIRATIONAL)** CI uploads `coverage.out` to Codecov.
Codecov reports the delta; it does not block the merge. This article supersedes the
unenforced ">80% coverage" target previously stated in `CONTRIBUTING.md`.

---

## Article XIII — Core stays open

**Rule.** `go.mod` must never require a private module. Proprietary presets and
closed extensions ship as an overlay `cmd/weos` in a separate repository.

**Reason.** WeOS core is AGPL 3.0. A private requirement in `go.mod` makes the
public repository unbuildable for everyone outside the organisation, which quietly
ends the open-source project.

**How this is enforced.** The CI `build` job runs `go build ./cmd/weos` without
private credentials. A private requirement fails it.

---

## Article XIV — Work lands through a reviewed pull request

**Rule.** Branch from `v3`. Open a pull request against `v3`. Stacked pull requests
onto another feature branch are allowed. Never push directly to `v3`, `develop`, or
`main`.

**Reason.** Review is where an article of this constitution gets caught before it
is broken in the default branch.

**How this is enforced.** Review, and the CI triggers on `main`, `develop`, and
`v3` in `.github/workflows/ci.yml`.

---

## Governance

**Precedence.** On conflict, an article of this constitution outranks `CLAUDE.md`,
`CONTRIBUTING.md`, `.claude/local-context.md`, and any skill.

**Amendment.** Amend an article through a pull request that edits this file and
bumps the version in the same commit. State the reason for the amendment in the
pull request body.

**Versioning.** This document uses semantic versioning.

| Bump | When |
|------|------|
| MAJOR | An article is removed, or redefined in a way that makes previously compliant code non-compliant |
| MINOR | An article is added, or materially expanded |
| PATCH | Wording, formatting, or a corrected reference only |

**Compliance review.** A reviewer who finds a violation names the article. An
author who must violate an article says which one and why, in the pull request
body. A repeated, justified violation is a signal to amend the article rather than
to keep granting exceptions.

**Aspirational articles.** An article marked **(ASPIRATIONAL)** is binding on
review but has no automated gate. Remove the marker when a gate lands.

### Amendment history

| Version | Date | Change |
|---|---|---|
| 1.0.0 | 2026-08-27 | Ratified |
| 1.1.0 | 2026-08-27 | Article VII expanded from six linters to ten. `gosec`, `gocritic`, `depguard`, and `forbidigo` added; `depguard` and `forbidigo` now machine-enforce Articles II and III. CI linter version pinned. |
