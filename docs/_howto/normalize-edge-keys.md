---
title: Normalize Stored Edge Keys
parent: How-to Guides
layout: default
nav_order: 12
---

# Normalize Stored Edge Keys

Resources written before compact edge storage (issue #515, `v3.0.1-alpha13`) store each
reference edge under the **predicate IRI** it resolved to at write time. Resources written
after it store the edge under the **property name**, and the document's own `@context`
carries the mapping. Both forms read, indefinitely — but an IRI-keyed edge is coupled to
the IRI that was current when it was written, so every later rename of that term needs a
permanent alias (`weos:termAliases`) on every instance.

One migration removes that coupling: it rewrites the stored events so every edge is keyed
by its property name. After it, a namespace change is a preset edit plus a reprojection.

## Before you start

- Stop the server. Both commands below run against the database directly.
- Back up the database. The rollback is restoring the backup — the migration appends,
  deletes and renumbers nothing, but it does rewrite event payloads in place.

## 1. Count what is still IRI-keyed

```bash
weos worker count-iri-edge-keys
```

The report counts, per resource type, the resources that still key an edge by IRI on two
surfaces:

- **events** — the `Resource.Created` / `Resource.Updated` payloads the migration rewrites;
- **records** — the canonical `resources` table every reader serves, which changes only
  when a reprojection replays the events.

Each IRI-keyed edge is classified the way the migration will treat it: **resolvable** (one
property claims it — it will be rewritten), **ambiguous** (several names claim it — it is
reported, never rewritten) or **unmapped** (nothing names it — likewise). The resources
holding an ambiguous or unmapped key are the residue the migration leaves behind; fix the
type's `@context` first if you want the check to reach zero.

The check exits `0` when both surfaces count zero, `2` when they do not, and `1` when it
could not run. It refuses to run against a store with no resource type and no event, so a
mistyped `DATABASE_DSN` cannot open an empty database and pass.

## 2. Dry-run the migration

```bash
weos worker normalize-edge-keys
```

Nothing is written. The report names, per type, the events that would be rewritten, and
lists every edge it would decline — `ambiguous edge key …` with the candidate properties,
`unresolved edge key …`, `colliding edge key …` — with the resource id, event id and
position, so you can decide before anything changes.

## 3. Apply it

```bash
weos worker normalize-edge-keys --write
```

Each batch commits on its own; re-running is idempotent. The command exits non-zero when
it declined any edge. An edge left keyed by its IRI keeps reading through the existing
paths.

## 4. Rebuild the read models

```bash
weos worker reproject
weos worker checkpoint reset oxigraph --truncate
```

`reproject` rebuilds the canonical records, projection columns and triples from the
rewritten events; the checkpoint reset rebuilds the knowledge graph, which `reproject`
does not reach. The triples table is upsert-only: a row under a predicate that moved
lingers beside the new one until that read model is rebuilt from scratch.

## When a term or class has moved: re-stamp

A reprojection replays each event's payload verbatim, and the class and predicates the
knowledge graph derives come from the payload's own embedded `@context`, stamped when the
resource was written. So when a preset moves a term, a prefix or a class — the house
vocabulary moving to `weos.io`, a type gaining an `@type` — resources written before the
move keep the old IRI however often you reproject.

```bash
weos worker normalize-edge-keys --restamp --type food-item --type recipe   # dry run
weos worker normalize-edge-keys --restamp --type food-item --type recipe --write
weos worker reproject
weos worker checkpoint reset oxigraph --truncate
```

`--restamp` brings every document's embedded `@context` and entity `@type` up to what a
fresh write embeds today, and moves the aggregate's `Triple.Created`/`Triple.Deleted`
predicates with it. It works from the type's **stored** context: on an install whose boot
is holding a preset's new IRI at the stored definition (the "context terms diverge" line at
startup), there is nothing to move until the held term is adopted. `--type` scopes the run;
without it every document whose embedded context differs from today's is rewritten.

## 5. Prove it, and keep proving it

```bash
weos worker count-iri-edge-keys
```

Both surfaces now count zero and the check passes. Between step 3 and step 4 the events
are clean and the records are not — the report shows that window rather than hiding it, so
do not declare the migration done until this check passes.

For a scheduled gate afterwards, `weos worker count-iri-edge-keys --records-only` reads
only the canonical records, which is the cheap steady-state check. It fails again the
moment the old shape reappears — a restored backup, an import from an instance that never
migrated — which nothing else on the instance would report.
