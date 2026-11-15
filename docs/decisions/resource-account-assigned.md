---
title: "ADR: An Ownerless Resource Is Given Its Owner Through the Event Stream"
parent: Architecture Decision Records
layout: default
nav_order: 16
---

# ADR: An Ownerless Resource Is Given Its Owner Through the Event Stream

**Status:** Accepted — Implemented
**Date:** 2026-11-13 (decision 5, the move out of an account with no members: 2026-11-15)
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
  knowledge-graph store holds it. Decision 5 answers those questions for one case only — a move out of
  an account with no members — with its own event and its own command; this repair still never moves.
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
  that is still ownerless stops the run with an error that names the resource and the table. A row the
  write did not reach at all is not counted either, because neither a re-run's fill nor a second
  assignment can write it. One that does not exist after the upsert is listed under `NeedsReproject`
  with the table, because a reproject writes it. One another account owns is listed under
  `ForeignRows` instead, with the table and the account that holds the row: a reproject uses the same
  fill-only upsert and keeps that owner, so there is no automatic repair, and the row needs
  investigation of how it got that owner. Both lists can hold a resource whose assignment event is
  already stored — the read-back comes after the commit — so the command describes them as incomplete
  projections, not as resources it left untouched.

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

A resource whose record has an owner and a projection row owned by a different account is missing
from the record owner's listing as well (wm-oyoqao). The selection finds it (a join on each table where
the row's owner is set and differs from the record's) and lists it under `ForeignRows`, in the preview
and in the result, whichever account owns the record. Nothing is written to it, and the command names no
repair for it: an assignment, a fill and `weos worker reproject` all keep an owner a row already has.

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
procedure forbids. A row to fill that has no owner to fill any more when the run reaches it is
read back before the resource is skipped: it is skipped only when every row holds the run's account. A
row that disappeared is listed under `NeedsReproject`, and one another account took under
`ForeignRows`, so a run with `--write` does not exit 0 over them.

The canonical table is never one of a resource's projection tables. A type named `resource`, or one
that descends from it, maps to `resources`, the canonical table. The selection, the read-back and the
fill leave that table out of every list of projection tables, with a projection manager and without one:
a gap query would otherwise join the table to itself and fail on an ambiguous column, ending the whole
preview or run, and the record is not a projection row.

This is a read-model write, which option 1 rejects. Option 1 is rejected because the owner would not
be in the stream. Here the `Resource.AccountAssigned` event is already in the stream, so a rebuild
writes the same owner, and the write only makes the read model agree with the stream sooner.

### 5. The one case where an owner moves: out of an account nobody can use

**Date:** 2026-11-15 — wm-c7xehr

Option 3 rejected a general transfer, and that rejection stands. One case needs a move all the same.
An account with no `account_members` row cannot be signed in to by anyone. Every resource it owns is
out of reach of every account-scoped path, and `checkInstanceAccess` refuses a delete by everyone but
the resource's creator. A production instance was found with an account like that, created by a
pre-cutover twin whose agent has no credential: its resources could not be listed, and a statement
import could not be removed, because each transaction delete was refused. `assign-owner` cannot help:
it only fills an empty owner.

`Resource.AccountAdopted{FromAccountID, AccountID, Timestamp}` moves a resource out of such an account.
It is not the transfer option 3 rejected, because each of the questions that made the transfer a
larger decision has a narrow answer here:

- **Who may move it.** Only the operator command `weos maintenance adopt-account`; no HTTP or MCP
  surface records the event. It refuses unless the source account exists and has **no members** and
  **no pending invitation** that has not expired (accepting one makes a member, wm-m0ci05), the
  target exists and is another account, and the agent named for the run is an **owner or admin** of the
  target. The run reads the member and invitation checks again before it moves anything. A move never
  takes a resource out of an account somebody can still use. One use the checks cannot see is the
  creator rule: an agent keeps access to what it created with no membership. The preview therefore lists
  each creator of the source's resources, whether it still has a credential and when it last wrote one,
  and leaves that judgment to the operator.
- **Its files.** The command moves resources, not files. An upload is stored under
  `accounts/<account>/` and its URL names that folder; the file route serves it only to that account,
  and erasure deletes the folder. A moved resource that names one would point at a file its new account
  can never read, and erasing the emptied source would delete the file. So the command refuses, in the
  preview and again when the real run starts, a source account with any file in its folder (counted in
  every configured storage backend through `services.AccountFileCounter`) or any live resource whose
  stored data names a URL in that folder, and the refusal gives both counts. Moving the files with
  their resources, and rewriting the URLs through events, is a separate decision (wm-lhc55a); until it
  is made, moving uploads is not supported yet.
- **Its grants.** `resource_permissions` rows name an agent and a resource, not an account, so a move
  leaves them as they are. The resource's creator is unchanged too. Lists and `memory_search` show a
  caller what it created or holds a read grant on, so the run gives the agent named for it a read grant
  on each resource it moves, through the resource permission service and before the move (wm-ebgqob).
  A grant the agent holds is kept, so a re-run grants nothing twice. Widening the list and search
  scope to the account is a separate decision (wm-o71nrb); until it is made, other members of the
  target do not see the moved resources in lists or search.
- **Its knowledge-graph store.** The per-account projector removes the resource from the source
  account's graph and loads its state into the target's, with the relations its history still holds
  (each `Triple.Created` not undone by a `Triple.Deleted`): `RemoveSubject` takes them out of the
  source with the rest of the resource. The lexical index re-indexes it under the target.
- **Detecting a wrong run.** The preview names both accounts, the source's member and invitation
  counts, the creators of its resources and the agent's role, and counts the resources per type, before anything is written. Every move is an event,
  so the stream records which resources moved, from where and to where.

**Compare-and-set, everywhere.** The event applies only while the resource is in `FromAccountID`. That
holds in `Resource.AdoptAccount`, which refuses a resource of any other account; in `ApplyEvent`; in the
synchronous projection, which moves the canonical record and then each projection row with a write
whose own `WHERE` checks the row is still in the source; in the graph projector and the lexical index,
which compare with the owner the resource's history shows before the event. So a replay changes nothing,
a second run moves nothing, and a resource of a third account is never touched.

**The projection contract does not change.** The upsert still keeps an owner a row already has; it is
still fill-only. The move does not go through the upsert at all: it writes the record and each row with
narrow statements (`WriteAccountAdoption`) that set the owner, the sequence number and the update time
and nothing else, the row's owner only while it is in the source or empty, so no other write gains the
power to move an owner. A full rewrite of the row would recompute its reference labels in the target,
and the run moves one resource at a time, so a reference to a resource not moved yet would lose its
label (wm-xuskm9). A row that does not exist is written whole, its labels read in the target and, where
they resolve only there, in the source. A rebuild over
existing rows replays `Resource.Created` first, which writes the source account back over the record
while the rows keep the target; the replayed move then puts the record in the target again. A rebuild
from empty tables writes the source and then moves it. Both end where the live write ended.

**Read-back, as in assign-owner.** A resource is counted as moved only when its record and every
projection row read back in the target. A record behind its event stream is listed for a reproject. A
row still in the source under a record that moved stops the run, and a re-run moves that row with no
second event, once it finds the move in the resource's history. A resource of the source account with a
row a third account holds is listed and never moved, and the run exits non-zero.

**The source account is not deleted** by the command. Removing the emptied account is erasure's job,
with its own checks — and erasure must not take what moved. Every moved resource's
`Resource.Created` still names the source account in its payload, and erasure used to delete the whole
stream of every aggregate whose events named the account. From this release it leaves every resource
whose history moved it out of the account (its latest `Resource.AccountAdopted` names another
account): its stream, its rows, its triples, its grants and its index rows are the new account's
(wm-myzy4t). **Never erase the source account with a binary older than this release**: that erasure
deletes the history of every resource that moved, and the next reproject removes the resources from
the target account.

The event follows the same rule as `Resource.AccountAssigned` below: once one is in a store, every later
release keeps the event type, its payload shape and its `ApplyEvent` case. A binary older than the
release that added it **fails** to load the aggregate of every moved resource (`unknown event type`), so
a write to one, an import removal over one and a reproject all fail; its background workers skip the
event. Never serve an older binary after a real run. If one served, the repair for the background
workers is the same two checkpoint resets, with this release.

## Consequences

### The event must never be removed

This applies to `Resource.AccountAdopted` (decision 5) in the same words.

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
