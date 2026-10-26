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
**Extended:** 2026-10-26, story wm-xtpky.2 — `resource_list` answers with flat projection rows.
See the section "Flat rows and the table-less listing" below.

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
| declared, in a schema the projection reads no columns from | `DocumentProperty` |
| declared, an `array` or `object`, and not a reference | refused: a condition compares one value |
| declared, a reference (`x-resource-type`) with a column of its own | the column; `eq` on one identifier matches a list-valued reference, as before |
| declared, a reference with no column — a standard column's name, or a schema the projection reads no columns from | refused, and left out of the filterable fields |
| not declared, a standard column | the column |
| not declared, on a type that has a schema | refused, with the list of filterable fields |
| any non-standard field, on a type with **no** schema | `DocumentProperty` |

**A schema the projection reads no columns from.** `schemaToColumns` reads each property's `type`
as a single name. A schema that gives any property a list of types, such as `["number","null"]`,
fails that read, and the projection makes no property columns for the whole type — only the
standard columns, as for a type with no schema. The tool decodes the same shape to detect this, and
reads every declared property from the document. Making the projection read a list of types would
add columns to live tables, with NULL on every existing row until a reproject, and would change
what REST `_filter` returns for those fields; that is out of this record.

**A reference is never read from the document.** The stored document keeps a reference as an edge
in `@graph[1]`, an `@id` object or a list of them, keyed by property name or, on records older than
issue #515, by predicate IRI. The document read looks only at the entity node, so `eq` would match
nothing and `ne` would match every resource. A reference with no column of its own is therefore
refused (option 2, for references only). No preset declares one today.

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
  `standardColumnNames` by a comment only, and `projectionReadsColumns` decodes the same schema
  shape as `schemaToColumns` the same way. If they drift, a condition the tool accepts can be
  silently skipped again. Follow-up **wm-c1nbkm**: export the sets from `domain/repositories`.
- **The cursor does not carry the filters.** A model that sends only the cursor for page two gets
  the unfiltered listing. The tool description tells it to resend the filters; no fingerprint check
  was added.

## Flat rows and the table-less listing (story wm-xtpky.2)

### Problem

`resource_list` gained `flat`: a call that sets it is answered with each resource's projection row
under `rows` instead of its JSON-LD document under `data`. A row costs a fraction of a document's
tokens, so a model counting open tasks or adding up amounts can read a large listing whole. The
filters above apply to a flat call unchanged.

Two things the decision above did not cover then became visible.

1. **What a flat row holds for a property the projection keeps no column for.** That is every
   property the table above marks `DocumentProperty`: a schema property a standard column shadows
   (a task's `status`), and every property of a schema the projection reads no columns from.
2. **A type with no projection table.** A flat call on such a type has no rows, so it is answered
   from the entity path — the same fallback the REST listing makes — and keeps its conditions.
   That path is `findAllFromGenericWithFilters`, which reads the canonical `resources` table.

### How it worked before story 2

- **The flat listing was REST-only.** `ResourceService.ListFlat` / `ListFlatWithFilters` returned
  the projection row as it is stored. For a task, `status` is the lifecycle column, `active` for
  every live task, and the task's own status is in no column at all. The REST flat listing still
  returns that row; nothing in this story changes it.
- **The table-less listing ignored the MCP flags.** `findAllFromGenericWithFilters` compared every
  condition as `json_extract(data, '$.<field>') <op> ?`: the top level of the document, which a
  JSON-LD record does not use (its properties are under `@graph[0]`), with no JSON type check and
  no `NeIncludesMissing`. So the decision table above was **not true** for a type with no
  projection table: a condition the tool accepted matched nothing, or every resource, with no
  error.

### Decision Drivers

- **A row and a condition read the same value.** In one call, `status eq open` must return rows
  whose `status` reads `open`. A row that says `active` beside a condition that matched `open` is a
  confident wrong answer for a model to repeat (Article XI).
- **REST results do not change** — still a published behavior, as in story 1.
- **No null where a value exists.** A row whose value cannot be read fails the call; it never
  carries a null that the model reads as "no value".
- **No silent drop on any path the MCP tool reaches**, including the table-less fallback.
- **No break for code built against this module.** Thin-wrap binaries and their test fakes
  implement `ResourceRepository` and `ResourceService`; a new method on either breaks their build.

### Considered Options

1. **Rows straight from the projection, with no overlay.** The wording of the feature file's
   preamble ("the columns the projection keeps … and nothing else"). Rejected: the scenarios
   *A flat listing answers with projection rows instead of JSON-LD documents* and *The cursor
   continues a filtered flat listing with the condition still applied* need a task row's `status`
   to be the task's status, and a condition on `status` reads the document (story 1), so the row
   and the condition would disagree in the same call.
2. **Overlay in the repository, so REST and MCP flat rows agree.** `ListFlat` would return the
   document's value for a shadowed name on every surface. Rejected for this epic: the REST flat
   row's `status` would change from the lifecycle to the task's status, and REST must not change.
3. **Promote a shadowed property to its own column** (story 1's option 3). Still rejected for the
   same reasons — DDL change, backfill and reproject on live instances — and still the route to an
   index (wm-pcebea).
4. **Overlay from a second run of the entity listing for the same page.** The first implementation
   (08197bb5). Rejected after review (wm-yh1xt5): the two listings run at different moments, so a
   write between them can move a resource onto or off the page and leave a row with a null; and
   the second listing evaluates every condition on the documents again, doubling the cost.
5. **Overlay in the MCP tool from the page's documents, read by id** — chosen.

For the table-less listing, the options were to keep ignoring the flags (rejected: the silent drop
this record exists to stop), to refuse every MCP condition on a table-less type (rejected: the
scenario *The fallback for a type with no projection table keeps the condition it was given* needs
it applied), or to **honour the flags** — chosen.

### Decision Outcome

**Flat rows** (`internal/mcp/resource_list_flat.go`, `listFlat`):

1. **A property the projection keeps no column for is read from the stored document.**
   `documentProperties` lists, from the type's JSON Schema, every property that is not a column
   (except `id`, the row's identity). After the projection page is read,
   `overlayDocumentProperties` sets each of those on every row from the resource's stored document
   — from `@graph[0]`, or the top level of a legacy flat record, the same places a
   `DocumentProperty` condition reads. A document with no value gives null. So **an MCP flat row's
   `status` on a task is the task's status, while a REST flat row's `status` for the same task is
   the lifecycle**; the lifecycle value is not in an MCP flat row for such a type.
2. **The page's documents are read by id, under the same visibility scope.** One
   `WHERE type_slug = ? AND id IN (…)` query over the page's identifiers, through `ListByIDs` →
   `FindAllByTypeAndIDs`, with the scope `buildVisibilityScope` gives the listing. A row whose
   document the read does not return (deleted in between), or a document that is not a JSON
   object, **fails the call** with the resource's id. It never becomes a null.
3. **A row that cannot carry the type's values answers under `data`.** A type with no JSON Schema
   (nothing names its properties), or with a reference that has no column (the document keeps a
   reference as an edge the overlay does not read), returns `errFlatAnswersWithDocuments`. The tool
   then answers from the entity path, as for `ErrNoProjectionTable`, **with its filters kept**. The
   tool description tells the model to read `data` whenever `rows` is absent.
4. **A flat sort on a document-read property is refused, by name.** The sort reads the projection
   column — the lifecycle for a task's `status`, and no column at all (falling back to `id`) for a
   property with none — so the rows would arrive in an order their values do not show.
   `checkFlatSort` returns an error that names the property. A call without `flat` is not checked:
   its sort is what it always was.
5. **Shadowed `createdAt` and `updatedAt` keep the column when the document lacks them.** These two
   standard columns mean what the property means (`sameMeaningColumns`; the mealplanning shopping
   list declares `createdAt`), so a row keeps the column's value instead of a null. Any other
   shadowed name reads null: a task with no status of its own is not `active`.

**The table-less listing** (`genericFilterClause` in
`infrastructure/database/gorm/resource_repository.go`; d5fd7af1, wm-ugv8y7):

6. **The table-less listing honours the MCP flags.** A condition carrying any of them is applied
   the way the projection applies it: a standard column of the `resources` table
   (`genericFilterColumns`), unless the tool marked it `DocumentProperty`, is compared as a column,
   with `NeIncludesMissing`; any other field is read by `documentFilterClause` from `data`, in its
   JSON type, on SQLite and PostgreSQL alike. A condition on a reference (`x-resource-type` in the
   type's schema) is **refused**: there is no projection column to match it in, and the document
   keeps it as an edge. A condition without the flags — every REST condition — keeps the old
   top-level `json_extract` reading, pinned by
   `TestFindAllWithFilters_GenericPathKeepsTheRESTReading`. `TestGenericFilterClause_Postgres`
   pins the PostgreSQL SQL text of the flagged path (e7a86b10).

**Two optional interfaces:**

7. `repositories.ResourceIDFinder` (`FindAllByTypeAndIDs`) and `application.ResourceIDLister`
   (`ListByIDs`) are interfaces of their own, **not methods on `ResourceRepository` or
   `ResourceService`**, so no repository or service implemented outside this module — a downstream
   binary's stub or test fake — stops building. The caller type-asserts for each. A service or
   repository without one is an error on a flat call that needs the overlay, never an empty answer.

### Consequences

#### Good

- In one MCP call, a flat row and a condition on the same name read the same value.
- A row never carries a quiet null in place of a value: a missing or unreadable document fails the
  call.
- The decision table above is now true for every type, with or without a projection table.
- REST flat rows, REST `_filter` on table-less types and the unflat MCP sort are unchanged.
- The by-id read cannot move a row off the page; a concurrent write can only make a value newer
  than the row's columns.
- No new method on a published interface; no table, column or migration change.

#### Costs and risks

- **The app and the assistant can disagree about a task's status.** A REST flat row (what the app
  reads) holds the lifecycle under `status`; an MCP flat row holds the task's status. A user who
  compares the two sees two answers for one task. This extends "The same name means two things"
  above from conditions to the row shape. Moving the overlay into the repository (option 2) is the
  fix, once REST is allowed to change.
- **One more query per flat page** for a type with a shadowed or column-less property — every task
  listing, because `status` is shadowed. It reads at most `limit` rows by primary key, so its cost
  is bounded by the page, not the table.
- **A `createdAt` row and a `createdAt` condition can disagree.** When the document lacks
  `createdAt`, the row shows the column's value, but a condition on `createdAt` still reads the
  document and finds no value. Accepted as a residual: the alternative is a null in the row for a
  value that exists.
- **A flat call cannot sort by a document-read property.** The model must sort the rows itself or
  drop `flat`. A column (option 3) would lift this.
- **The REST table-less listing on PostgreSQL still uses SQLite's `json_extract`.** Pre-existing,
  and not changed here because REST must not change; follow-up **wm-stihh3**.
- **Document reads still have no index** (wm-pcebea), and **drifted columns are still not covered**
  (wm-j9ninh); both apply to flat rows as they do to conditions.

## Further Reading

- [Projection Schema Migration]({% link decisions/projection-schema-migration.md %}) — why the
  projection's columns drift, and what a column migration on live instances costs.
- [A Projection Clears an Omitted Property]({% link decisions/projection-clears-omitted-properties.md %})
  — the other recent change to what the projection promises its readers.
