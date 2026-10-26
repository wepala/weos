---
title: "ADR: An MCP Filter Can Read the Stored Document"
parent: Architecture Decision Records
layout: default
nav_order: 13
---

# ADR: An MCP `resource_list` Filter Can Read a Property from the Stored Document

**Status:** Accepted — Implemented
**Date:** 2026-10-26
**Story:** wm-xtpky.1 — `resource_list` filters, and a bad filter fails loudly (epic wm-xtpky)

## Problem

The MCP `resource_list` tool gained `filters`: an array of `{field, operator, value}` conditions
that an LLM client sends to answer a question such as "which of my tasks are not done?". The client
is a model, so a filter that is not applied is worse than an error: the model reads the unfiltered
page as the answer and tells a person something confident and wrong (Article XI).

### How filtering worked before

A `repositories.FilterCondition` always meant **a projection column**.
`FindAllByTypeWithFilters` converted `Field` to snake_case and compared that column. When the
column did not exist (`HasColumn` false) or the operator was unknown, the repository **skipped the
condition without a word** and returned the rest of the listing. The REST listing's
`_filter[field][op]=value` parameters rely on that skip, and REST clients see it as "an unknown
filter is ignored".

Three facts of the projection made that column-only reading wrong for an LLM client:

1. **A standard column shadows a schema property of the same name.** `ExtractFlatColumns` skips any
   schema property whose snake_case name is in `standardColumnNames` (`status`, `account_id`,
   `created_by`, `updated_at`, `sequence_no`, …), so the projection never stores it. The tasks
   preset declares `status` as the task's own state, but the projection's `status` column is the
   resource lifecycle, which holds `active` for every live resource. A filter `status ne done` on
   tasks therefore compared the lifecycle and matched every task. Other preset types declare
   `accountId`, `createdBy`, `updatedAt` or `sequenceNo` the same way.
2. **A type with no JSON Schema has only the standard columns.** Every other field a client named
   was skipped, and the whole listing came back.
3. **SQL `!=` is never true for NULL**, so `ne` dropped every resource that left the field empty.

## Decision Drivers

- **No silent drop on the MCP path.** Each condition the tool accepts is applied; each condition it
  cannot apply is refused with an error that names the valid fields or operators. As a scenario:
  *an LLM client* sends *a filter on a field the type declares*, against *`resource_list`*, on *a
  live instance with existing resources*; the tool *returns only matching resources, or an error*,
  and *no unfiltered page is ever returned for a filtered call*.
- **REST results do not change.** The REST listing and its skip are a published behavior.
- **The answer is the property the client named.** On a task, "status" means the task's status.
- **No migration of projection tables** on live instances in this epic (see
  [Projection Schema Migration]({% link decisions/projection-schema-migration.md %}) for what that
  costs).
- **Both databases.** SQLite (desktop, tests) and PostgreSQL (cloud) give the same answer.

## Considered Options

1. **Read the lifecycle column for a shadowed name.** The existing behavior. No code change in the
   repository. Rejected: it answers a different question than the one asked. `status ne done` on
   tasks matches every task, which is the confident wrong answer this story exists to stop.
2. **Refuse a shadowed name.** Honest, and small. Rejected: the most common filter on the most
   common type (a task's `status`) becomes impossible over MCP, and the model's only fallback is to
   list every resource and filter in its own context, which is slow, costly and truncates.
3. **Promote a shadowed property to its own projection column** (for example `task_status`).
   Indexable, and keeps a single column-only read path. Rejected for this epic: it changes the DDL
   of every table whose type shadows a standard name, needs an added column, a backfill and a
   reproject on live instances, and needs a naming rule for the new column that every reader
   (REST, the flat listing, sort, the admin UI) must then learn. It stays the route to an index
   (follow-up wm-pcebea).
4. **Validate only at the MCP boundary and keep the repository untouched.** Rejected: the
   repository has no way to read a shadowed property or a field of a schema-less type, so boundary
   validation could only refuse those (option 2) or pass them on to be misread or skipped
   (option 1). It also cannot fix `ne` on NULL or compare a number as a number.
5. **Scoping flags on `FilterCondition` that only the MCP tool sets** — chosen.

## Decision Outcome

Chosen option 5. `FilterCondition` gains three fields. Their zero values reproduce the old
behavior exactly, and only `internal/mcp/buildListFilters` sets them, so the REST listing and every
in-process caller are unchanged.

| Field | Meaning |
|---|---|
| `DocumentProperty bool` | Compare the property in the resource's stored JSON-LD document, not the projection column named `Field`. |
| `ValueType FilterValueType` | The JSON type (`string`, `number`, `boolean`) a `DocumentProperty` condition compares as. A column condition ignores it. |
| `NeIncludesMissing bool` | `ne` also matches a resource with no value for the field (column: `IS NULL OR`; document: absent or JSON null). |

### In the repository

`projectionFilterClause` builds every condition for both the entity listing and the flat listing.
A column condition goes through the old path, including the skip when the column is missing. A
`DocumentProperty` condition goes to `documentFilterClause`, which:

- reads the property from the entity node (`@graph[0].<field>`) or, for a legacy flat document,
  from the top level, with `COALESCE` taking whichever the document has;
- binds every path element and the value as parameters, so no field name reaches the SQL text;
- compares the value only with a stored value of the same JSON type — SQLite through `json_type`
  and `json_extract`, PostgreSQL by comparing `jsonb` with `jsonb` — so `"3"` is never compared
  with `3` and no stored value is ever cast;
- in the flat listing, reads `resources.data` through a scalar subquery on `id`.

### In the MCP tool

`buildListFilters` reads the type's JSON Schema and decides each condition:

| The field is… | Result |
|---|---|
| declared by the schema **and** a standard column's name | `DocumentProperty` — **the schema property wins over the column** |
| declared by the schema, not a standard name | the projection column |
| declared, an `array` or `object`, and not a reference | refused: a condition compares one value |
| declared, a reference (`x-resource-type`) | the column; `eq` on one identifier matches a list-valued reference, as before |
| not declared, a standard column | the column |
| not declared, on a type that has a schema | refused, with the list of filterable fields |
| any non-standard field, on a type with **no** schema | `DocumentProperty` |

Every condition also sets `NeIncludesMissing`, and the value is converted to the property's declared
JSON type (a value that type cannot hold is refused). An unknown operator is refused, because the
repository would skip it. The tool description states these rules, and says that a cursor does not
carry the filters, so the next page needs the same filters sent again.

## Consequences

### Good

- Over MCP, a condition is either applied or refused. The silent skip remains only for the case
  below that this record does not cover.
- A task's `status`, and every other shadowed property, filters on what the client named.
- REST, the admin UI and in-process callers see no change; the flags default to the old behavior.
- No table, column or migration changes; nothing to reproject.
- A user-supplied `accountId` condition cannot widen access: the visibility scope is applied
  separately and ANDed with every condition.

### Costs and risks

- **A second read path in the repository.** Filtering now reads either a projection column or the
  stored document. Every future change to filtering (a new operator, a sort, a new dialect) has two
  paths to keep in step.
- **The same name means two things on two surfaces.** `_filter[status]` on tasks over REST still
  reads the lifecycle column; `status` over MCP reads the task's status. Likewise `accountId`,
  `createdBy`, `updatedAt` and `sequenceNo` on preset types that declare them. A reader comparing a
  REST result with an MCP result for those names will see different rows. This is deliberate,
  because REST results must not change, but it is a lasting divergence.
- **Document reads have no index.** Each candidate row's JSON is parsed several times per condition
  (the type check and the value, each with two `COALESCE` arms), and PostgreSQL casts `resources.data` to `jsonb` per arm. The most common
  filter, a task's `status`, takes this path. Follow-up **wm-pcebea**; option 3 is the likely fix.
- **A drifted projection column is still not covered.** A declared, non-shadowed property reads its
  column. When that column is NULL on rows written before the column existed, or missing on a
  drifted database, the condition matches wrongly or is skipped, with no error. Out of this epic;
  follow-up **wm-j9ninh**.
- **PostgreSQL is tested in CI only by its SQL text and arguments.**
  `TestDocumentFilterClause_Postgres` pins the generated SQL and bound arguments;
  `TestFindAllWithFilters_DocumentPropertyComparesInItsJSONType_Postgres` runs against a real
  database only where `TEST_POSTGRES_DSN` is set, and CI does not set it.
- **Hand-copied constant lists.** The MCP tool keeps `filterOperators`, `projectionSkippedKeys` and
  `standardColumns` equal to the gorm package's `operatorMap`, `jsonLDKeys` and
  `standardColumnNames` by a comment only. If they drift, a condition the tool accepts can be
  silently skipped again. Follow-up **wm-c1nbkm**: export the sets from `domain/repositories`.
- **The cursor does not carry the filters.** A model that sends only the cursor for page two gets
  the unfiltered listing. The tool description tells it to resend the filters; no fingerprint check
  was added.

## Further Reading

- [Projection Schema Migration]({% link decisions/projection-schema-migration.md %}) — why the
  projection's columns drift, and what a column migration on live instances costs.
- [A Projection Clears an Omitted Property]({% link decisions/projection-clears-omitted-properties.md %})
  — the other recent change to what the projection promises its readers.
