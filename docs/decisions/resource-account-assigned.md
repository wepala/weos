---
title: "ADR: An Ownerless Resource Is Given Its Owner Through the Event Stream"
parent: Architecture Decision Records
layout: default
nav_order: 16
---

# ADR: An Ownerless Resource Is Given Its Owner Through the Event Stream

**Status:** Accepted — Implemented
**Date:** 2026-11-13
**Story:** wm-4kdhro — backfill the owner on rows written with an empty account during the pericarp
session bug

## Problem

A resource takes its owning account once, from `Resource.Created`, which copies the writer's active
account. Until weos v3.0.1-alpha12 fixed pericarp#68, every authenticated session ran with an empty
active account, so every resource written in that time stored `AccountID: ""`.

Account-scoped reads never match an empty owner. The owner of those resources cannot see, remove or
repair them through any account-scoped path.

Before this change, no event could set the owner after `Resource.Created`:

- `Resource.ApplyEvent` set `accountID` from `Resource.Created` only.
- `Resource.Updated` changes data only, and the update projection keeps the stored row's owner.
- The projection upsert kept `account_id` out of its `ON CONFLICT ... DO UPDATE` columns, so a set
  owner was immutable on the read model, and an empty one stayed empty.

So a hand `UPDATE` of `account_id` lasts only until the next rebuild. `weos worker reproject` replays
`Resource.Created` and writes the empty owner back.

## Decision Drivers

- **The repair must survive a rebuild.** The read model is a projection of the stream, so the owner
  has to be in the stream.
- **A repair must never move a resource between accounts.** Moving a resource raises questions this
  repair does not need to answer: who may move it, what happens to its grants, and which account's
  knowledge-graph store holds it.
- **The stream is append-only.** History is not rewritten.
- **Every consumer that routes by account must see the owner.** These are the projection tables, the
  canonical record, the per-account knowledge graph and the lexical index.

## Options Considered

### 1. Update the projection tables by hand — rejected

This is the smallest change, and it needs no code. It does not last: a rebuild replays
`Resource.Created` and empties the owner again. The owner would also never reach the knowledge graph
or the lexical index, because their subscribers read the stream, not the tables.

### 2. Rewrite the stored `Resource.Created` events — rejected

This makes the stream say what the owner "should have been". It breaks the append-only contract of
the event store, it makes the stream disagree with any export or backup already taken, and it hides
that a repair happened.

### 3. A general owner-change event (a transfer) — rejected

`Resource.AccountChanged{From, To}` would also cover future moves between accounts. It is a much
larger decision: authorization for the move, the grants that refer to the old account, and the
removal of the resource from the old account's graph store. None of that is needed to repair an
empty owner, and a transfer would make a wrong repair run harder to detect.

### 4. Infer the owner at replay time — rejected

A replay rule such as "an ownerless resource belongs to its creator's personal account" needs no new
event. It is wrong for an agent who is a member of more than one account, it would apply silently to
every rebuild, and nothing in the stream would record that it happened.

### 5. A fill-only `Resource.AccountAssigned` event — chosen

## Decision

### 1. The event

`Resource.AccountAssigned{AccountID, Timestamp}` gives an ownerless resource its owner. It is
**fill-only** in every place that reads it:

- `Resource.AssignAccount` refuses a resource that already has an owner, even the same owner
  (`ErrResourceAlreadyOwned`).
- `Resource.ApplyEvent` sets the owner only when it is empty, so the first owner in the history
  always stands.
- The synchronous projection handler writes the owner into the canonical record and every projection
  row, and it always writes the event's sequence number. It keeps a set owner and logs a warning if
  the event names a different one.
- The per-account knowledge-graph projector reads the owner from the resource's history, so on a
  graph rebuild the resource's earlier accountless events go to the assigned owner's store. The
  lexical index re-indexes the row under the owner.
- `weos worker reproject` replays the event.

### 2. The projection contract changes: `account_id` is fill-only on upsert

Before: on conflict, the upsert never updated `account_id`.
After: on conflict, the upsert writes the incoming `account_id` only over an empty or `NULL` one
(`fillEmptyOwner`, a `CASE` over the existing and `excluded` values). A set owner is still never
overwritten, so this change only adds a case where the owner can be filled. A rebuild over existing
rows keeps the owner.

### 3. The only writer is an operator command

`weos maintenance assign-owner` is the only code that records the event. No HTTP or MCP surface
reaches it. The command:

- refuses unless the account exists and the creator is a member of it;
- selects only ownerless resources of one creator, optionally inside a creation-time window;
- lists every selected resource on a dry run;
- names the target account before a real run writes anything;
- requires the account to be given a second time (`--confirm-account`) when the creator is a member
  of more than one account;
- writes one event per resource through the normal unit of work;
- counts a resource as assigned only after its record **and every projection row it has** (its own
  type's table and each ancestor type's table) are read back with the owner. A record that is behind
  its event stream is skipped and listed, with `weos worker reproject` as the repair. A projection row
  that is still ownerless stops the run with an error that names the resource and the table.

### 4. A re-run fills a projection row from the record, with no second event

Projection writes are not transactional (`Save` and `Update` say so on purpose), so the record can
take the owner while a projection row does not. The resource then has its event, and
`AssignAccount` refuses a second one.

The command therefore also selects a resource whose record already has the run's `--account` but
whose projection rows do not. The preview marks it "(record has the owner; projection row to fill)".
A resource whose record has the run's `--account` but that has no projection row at all in one of its
type tables is missing from that listing too, and neither an assignment nor a fill writes a row that
does not exist (wm-zm4sbz). The selection finds it (`NOT EXISTS` against each table) and lists it under
`NeedsReproject`, in the preview and in the result, with `weos worker reproject` as the repair. It is
not filled.

A real run writes the record's data and owner over each ownerless projection row
(`FillOwnerlessProjectionRows`), and then reads the rows back. It appends no event and does not write
the canonical record. It skips a record that is behind the event store, that has a different owner, or
that has a different creator.

A resource whose record is owned by an account other than the run's `--account` is never filled by
that run, because the run's account check does not cover it. It is still counted, per owning account,
and the preview and the result print each account id (wm-v16sea), so a creator who is a member of more
than one account learns that a run with the other account is needed instead of reading a clean run.

A fill rewrites the whole projection row from the record and sets its `updated_at`, as
`weos worker reproject` does, so on a type whose row had drifted from the record it changes more than
the owner. The preview and the result therefore count fills apart from assignments ("rows to fill",
with no event, beside the resources to assign, with one event each), so a run's assigned count matches
the events it stored. When the preview lists many rows to fill, a reproject of the stopped instance is
the better repair, and the preview says so.

The fill does not go through a unit of work, so no version conflict protects it from a write that
lands between its read of the record and its write of a row (wm-5o1kcm). The write statement itself
carries the guard instead: it updates the row only while the row has no owner and its `sequence_no` is
not past the record's. A row that fails the guard is left as it is, and the resource is listed under
`NeedsReproject` with the reason. That only happens when the instance serves during the run, which the
procedure forbids.

This is a read-model write, which option 1 rejects. Option 1 is rejected because the owner would not
be in the stream. Here the `Resource.AccountAssigned` event is already in the stream, so a rebuild
writes the same owner, and the write only makes the read model agree with the stream sooner.

## Consequences

### The event must never be removed

**Once one `Resource.AccountAssigned` is in a store, every later release must keep the event type,
its payload shape and its `ApplyEvent` case for as long as that stream exists.**

`Resource.ApplyEvent` returns `unknown event type` for any payload it does not know. A release
without this case would fail to load the aggregate of every repaired resource from its history. A
release without the projection case would rebuild every repaired resource as ownerless again, and the
repair would be lost with no error. Deprecating the command is allowed. Removing the event is not.

### Older binaries

A binary built before this release does not know the event. Its background subscribers (`oxigraph`,
`lexical-index`) skip an unknown event type and advance their checkpoints past it. If such a binary
serves during or after a real run, the knowledge graph and the search index never learn the owner,
and nothing reports this. The repair is `weos worker checkpoint reset oxigraph` and
`weos worker checkpoint reset lexical-index` with this release. The command's help states the rule:
upgrade the instance to this release and stop it before a real run.

### Other consequences

- **There is no undo.** An assignment cannot be reversed by any event. A wrong run is reversed only
  by a database restore. This is why the command lists the rows, names the account and requires the
  account a second time for a creator who is in more than one account.
- **A real run refreshes `updated_at`** on every repaired row, as a no-op edit would. Lists sorted by
  last update show the repaired rows first after the run.
- **A resource with no creator** (a system write) cannot be selected, because the membership check
  has no agent to test.
- **The first preview after an upgrade can list more rows than the ownerless records.** It also
  lists every resource whose record has the run's account but whose projection row is ownerless,
  whatever the cause. Each such row carries the mark above, and is counted as a row to fill, not as
  a resource to assign.
- The `CASE` in the upsert is tested on SQLite and, when `TEST_POSTGRES_DSN` names a server, on
  PostgreSQL.

## Further Reading

- [Transaction ID and Projection Consolidation]({% link decisions/transaction-id-and-projection-consolidation.md %})
  — the synchronous projection path that this event uses.
- [Projection Schema Migration]({% link decisions/projection-schema-migration.md %}) — how the
  projection tables are built and rebuilt.
- [An Account Erasure Participant]({% link decisions/account-erasure-participant.md %}) — erasure
  selects a repaired resource by its canonical `account_id`.
