---
title: "ADR: A Projection Clears an Omitted Property"
parent: Architecture Decision Records
layout: default
nav_order: 8
---

# ADR: An Update Is a Full Replacement, so the Projection Clears What It Omits

**Status:** Accepted — Implemented
**Date:** 2026-08-30
**Issue:** [#550 — Clearing a reference does not clear it in the projection](https://github.com/wepala/weos/issues/550)

## Problem

A client cleared a reference on a resource. The event store recorded the clear correctly. The
projection row kept the old value forever, and every read built on that row — the API, a filter, a
sort, the admin UI, the graph — kept reporting a reference the record no longer had.

One shape caused it in all three projection write paths. The row is built from the keys
`ExtractFlatColumns` produced, and a property the document no longer carries produces no key.
`updateProjectionBySlug` then derives its `ON CONFLICT ... DO UPDATE` column list from those same
keys, so the column was never in the `UPDATE` statement at all. The write reported success. Nothing
in the system said the value had gone stale.

The defect was not confined to references. A literal property removed from a document went stale in
exactly the same way, in the concrete table and in every ancestor table a dual projection writes.

## The contract this records

The decision here is **not** "a bug was fixed". It is a contract change that reaches every resource
type on the platform, so it is stated plainly:

> **An update is a full replacement.** The document a client sends on the update path is the whole
> intended state of the resource. A projected property the document omits is **cleared**, not
> preserved. The read model now agrees with the canonical store instead of preserving values the
> canonical store had already dropped.

Three qualifications belong with it.

**A partial patch is unaffected.** `UpdateData` supplies only the fields it means to change.
Absence there means "unchanged" and always did. The clear belongs to the wholesale path alone.

**A derived column is never cleared by absence.** A `<fk>_display` column is written by the
projection, not stated by any document, so it is absent from every write by construction. Only
DECLARED columns — one per schema property, plus the FK of every activated link — take part.

**A column is cleared only when the document was read in full.** See the safety rule below.

## Decision

### 1. The clear lives on the wholesale write path only

`nullClearedColumns` adds an explicit `nil` for every declared column the extracted row does not
carry, and it is called from `updateProjectionBySlug` and nowhere else. It runs BEFORE
`populateDisplayColumns`, because that helper already nulls a display column whose FK key is present
and nil — which is exactly the input a cleared reference produces. The ordering is pinned by
`TestUpdateProjection_ClearedReference_NullsDisplayColumn`.

`ProjectionManager` gained `DeclaredColumns(slug)` to serve it. The declared set is computed in
`EnsureTable` from the same `columnDef` list that builds the table, with a new `Derived` flag
marking the columns no document states. `RegisterLink` extends the set with the FK it activates and
not with its display sibling.

### 2. Absence clears only after a COMPLETE read — the safety rule

Absence of a column from the extracted row has two possible causes, and only one of them is a clear:

1. The client dropped the property. This is a clear.
2. The extraction could not read it. This is **not** a clear.

Acting on the second erases live data. Two cases were real:

- `ExtractFlatColumns` returned silently when the document did not parse. Every declared column was
  then absent, so a parse failure escalated from "the row goes stale" to "the row is erased".
- `EdgeProperty` returns `ok=false` for any predicate IRI that no term, alias or `@vocab` in the
  stored `@context` names. Records written before issue #515 key their edges by predicate IRI, and a
  stored `@context` can drift — `weos resource-type adopt` exists because of that drift, and it tells
  the operator to run `weos worker reproject`, which drives this exact path over the whole history. A
  documented operator action would have mass-nulled live references.

So the extraction now reports what it could not read. `ExtractFlatColumns` returns an
`ExtractReport` carrying a `ReadError` for a document it could not parse and an `UnreadableEdges`
list naming every edges-node key that produced no column value. The clear runs only when
`report.Complete()` is true. An incomplete read leaves the row exactly as stale as it was before this
issue — the old defect, never a new one — and logs the keys it could not read, so context drift is
findable instead of silent (Article XI).

The rule is stated as a property of the extraction rather than as two guards at the two known call
sites, because a third silent skip added later would otherwise re-open the same hole.

### 3. Rejected alternatives

**Widen every property to accept an explicit `null`.** The client would then say "clear this" rather
than omit it. Rejected: it is a schema change on every resource type on the platform, carrying the
stored-type migration risk that `docs/decisions/projection-schema-migration.md` documents at length,
on live instances. It also does not fix the reported defect for any client that already omits the
property — which is what the API's own update semantics tell them to do.

**Put the clear in the shared `ExtractFlatColumns` + `dropMissingColumns` sequence.** All three write
paths run it, so this is the smallest diff and looks like the obvious home. Rejected: it turns every
partial `UpdateData` patch into a wipe of everything the patch omitted. Pinned against by
`TestUpdateData_PartialPatch_KeepsOmittedLiteralColumn`.

**Preserve a value the document dropped, as a safety net.** Rejected, because it is the defect. The
projection is a read model of the canonical store, and a read model that holds values the store does
not is not a cache — it is a second, disagreeing source of truth.

## Consequences

### This change does not create data loss. It unmasks loss that already happened.

`Resource.Restore` has always replaced the entity's data wholesale
(`domain/entities/resource.go:75`), and so has the `ResourceUpdated` branch of `ApplyEvent`
(`domain/entities/resource.go:129`). `application/event_handlers.go` restores the stored resource
with `state.Data` and keeps only metadata from the stored row. The canonical record therefore lost an
omitted property at the moment of the update, every time, long before this change. The projection was
the only place the old value survived — which made the read model disagree with the record it
projects, and made the disagreement invisible.

Anyone reading this after a report of "the new build deleted my data" should start here: the value
was gone from the canonical store already. What changed is that the projection stopped hiding it.

### Good

- A cleared reference, and a cleared literal, reach the projection — in the concrete table and in
  every ancestor table.
- The read model and the canonical store agree, so a reproject is idempotent instead of corrective.
- A drifted `@context` is now named in the log on every write that meets it, on all three write
  paths, instead of being skipped in silence.
- A document the projection cannot parse no longer erases the row.

### Risks and things an operator must know

- **A writer that sends a trimmed payload now visibly wipes fields.** Any first-party or third-party
  caller that builds its update from a subset of the resource will clear everything it left out. It
  always cleared them in the canonical store; now the projection agrees, so the effect is visible to
  users. **An audit of first-party writers is wanted and is deliberately not part of this change.**
  This is the release-note item.
- **`weos worker reproject` erases values that survived only in the projection.** This is correct
  behavior — the projection is rebuilt from canonical documents, and those documents never had the
  values. Users will nonetheless experience it as mass data loss. **A pre-reproject audit is wanted:**
  compare each projection row against its canonical document and report the columns that would go
  NULL, before anyone runs the command on a populated instance.
- **The fix is prospective.** A row that already went stale under the old behavior stays stale until
  the resource is next updated, or until an operator reprojects. Nothing in this change repairs
  history.
- **An unreadable edge leaves a stale column rather than clearing it.** That is the safe direction of
  the trade, but it means a genuine clear is not applied while the drift lasts. The log names the key;
  fixing the `@context` (`weos resource-type adopt`) and reprojecting is the repair.
- **An empty edge value is treated as unreadable.** `BuildResourceGraph` writes an edges key only
  with at least one reference, so an edge carrying none is a shape this reader does not know. A future
  writer that emits an empty list to mean "cleared" would find it ignored, and would need this rule
  revisited.

## Further Reading

- [Projection Schema Migration]({% link decisions/projection-schema-migration.md %}) — how projection
  columns are derived, and the reproject path this change interacts with.
- [Cross-Preset Link Definitions]({% link decisions/cross-preset-link-definitions.md %}) — the links
  whose FK columns join the declared set.
